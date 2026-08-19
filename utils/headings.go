package utils

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// HeadingInfo 代表一個標題的資訊。
type HeadingInfo struct {
	Level int    // 標題層級 (1-6)
	Text  string // 標題文字（不含 # 符號）
}

// ExtractHeadings 從原始 Markdown 文字中提取所有標題。
func ExtractHeadings(md string) []HeadingInfo {
	return extractHeadingsFromSource([]byte(md))
}

func extractHeadingsFromSource(source []byte) []HeadingInfo {
	var result []HeadingInfo
	gm := goldmark.New()
	p := gm.Parser()
	reader := text.NewReader(source)
	doc := p.Parse(reader)

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		var textBuilder strings.Builder
		child := h.FirstChild()
		for child != nil {
			switch t := child.(type) {
			case *ast.Text:
				textBuilder.Write(t.Segment.Value(source))
			case *ast.String:
				textBuilder.Write(t.Value)
			case *ast.Emphasis:
				// 處理粗體/斜體中的文字
				gc := t.FirstChild()
				for gc != nil {
					if gt, ok := gc.(*ast.Text); ok {
						textBuilder.Write(gt.Segment.Value(source))
					} else if gs, ok := gc.(*ast.String); ok {
						textBuilder.Write(gs.Value)
					}
					gc = gc.NextSibling()
				}
			}
			child = child.NextSibling()
		}

		result = append(result, HeadingInfo{
			Level: h.Level,
			Text:  strings.TrimSpace(textBuilder.String()),
		})
		return ast.WalkContinue, nil
	})

	return result
}

// FindHeadingLineInRenderedContent 在渲染後的內容（含 ANSI 色碼）中尋找標題對應的行號。
// 回傳 0-based 行號，若找不到則回傳 -1。
func FindHeadingLineInRenderedContent(rendered string, heading HeadingInfo) int {
	lines := strings.Split(rendered, "\n")

	for i, line := range lines {
		if isHeadingLine(line, heading) {
			return i
		}
	}

	return -1
}

// isHeadingLine 檢查一行是否為指定層級的標題行。
// glamour 的 dark style 對 h1 不顯示 # 符號，所以我們需要同時檢查
// 有無 # 前綴的情況（h2+）以及純文字匹配（h1）。
func isHeadingLine(line string, heading HeadingInfo) bool {
	// 注意：glamour 輸出每行都有前導空白，必須先 TrimSpace
	clean := strings.TrimSpace(StripANSI(line))
	if clean == "" || heading.Text == "" {
		return false
	}

	// h2+: 檢查是否有 "## Text"、"### Text" 等格式
	if heading.Level >= 2 {
		prefix := strings.Repeat("#", heading.Level) + " "
		return strings.HasPrefix(clean, prefix) && strings.Contains(clean, heading.Text)
	}

	// h1: glamour dark style 不顯示 #，只檢查文字內容
	if heading.Level == 1 {
		return strings.HasPrefix(clean, heading.Text)
	}

	return false
}

// FindHeadingLines 以文件順序逐一定位各標題在渲染後內容中的行號。
// 依序掃描（每個標題只在前一個標題之後搜尋）可以避免同名標題錯配，
// 回傳值與 headings 一一对應，找不到時為 -1。
func FindHeadingLines(rendered string, headings []HeadingInfo) []int {
	lines := strings.Split(rendered, "\n")
	result := make([]int, len(headings))

	searchStart := 0
	for i, h := range headings {
		line := -1
		// 第一層：前綴 + 完整文字匹配
		for j := searchStart; j < len(lines); j++ {
			if isHeadingLine(lines[j], h) {
				line = j
				break
			}
		}
		// 第二層：標題文字過長被換行時，單一列不會包含完整文字，
		// 退而求其次只要求前綴（h2+）或開頭文字相符。
		if line == -1 {
			for j := searchStart; j < len(lines); j++ {
				clean := strings.TrimSpace(StripANSI(lines[j]))
				if clean == "" {
					continue
				}
				if h.Level >= 2 && strings.HasPrefix(clean, strings.Repeat("#", h.Level)+" ") {
					line = j
					break
				}
				if h.Level == 1 {
					runes := []rune(h.Text)
					if len(runes) > 8 {
						runes = runes[:8]
					}
					if strings.HasPrefix(clean, string(runes)) && !strings.ContainsAny(clean, "\t") {
						line = j
						break
					}
				}
			}
		}
		result[i] = line
		if line >= 0 {
			searchStart = line + 1
		} else if i < len(headings)-1 {
			// 找不到時不推進搜尋起點，讓後續標題仍可從原位置開始找，
			// 但仍需避免把同一行重複分配：保持 searchStart 不變即可。
		}
	}

	return result
}

// StripANSI 從字串中移除 ANSI escape sequences。
func StripANSI(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
