package ui

import "strings"



// highlightMatchesANSI 在含 ANSI 色碼的字串中為匹配文字加上高亮標記。
// matches: [[start, end], ...] 是基於純文字（無 ANSI）的 byte 偏移，
// 且必須不重疊、按 start 升序（由 SearchModel.findMatches 保證）。
// currentIdx: 當前選中的匹配項索引，用於不同樣式的高亮。
func highlightMatchesANSI(content string, matches [][]int, currentIdx int) string {
	if len(matches) == 0 {
		return content
	}

	// 首先解析 ANSI 序列，建立「純文字 byte 索引 → 原始字串 byte offset」映射。
	// 注意：這裡的「字元」單位是 byte（與 matches 的偏移單位一致），
	// 對多-byte 字元（如中文）依然成立，因為兩邊都用 byte 計算。
	type charPos struct {
		byteOffset int // 在原始字串中的 byte offset
		charIndex  int // 純文字中的 byte 索引
	}

	var chars []charPos
	inEscape := false
	charIdx := 0

	for i := 0; i < len(content); i++ {
		b := content[i]
		if b == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
				inEscape = false
			}
			continue
		}
		chars = append(chars, charPos{byteOffset: i, charIndex: charIdx})
		charIdx++
	}

	if len(chars) == 0 {
		return content
	}

	// 建立從純文字位置到 byte offset 的映射
	charToByte := make([]int, charIdx+1)
	for _, c := range chars {
		charToByte[c.charIndex] = c.byteOffset
	}
	charToByte[charIdx] = len(content) // EOF position

	// 從後往前插入高亮序列（避免偏移問題；matches 不重疊且由 findMatches 保證升序）。
	// 高亮用「開啟/關閉」配對的 SGR 參數，不用全域 reset（\x1b[0m），
	// 以免破壞匹配區域周圍的底色/字色。
	const (
		hlOtherOpen  = "\x1b[4;38;5;213m" // 下劃線 + 紫紅
		hlOtherClose = "\x1b[24;39m"     // 取消下劃線、字色還原
		hlCurOpen    = "\x1b[7;1m"       // 反白 + 粗體
		hlCurClose   = "\x1b[27;22m"     // 取消反白/粗體
	)

	result := content
	for i := len(matches) - 1; i >= 0; i-- {
		startChar := matches[i][0]
		endChar := matches[i][1]

		if startChar >= len(chars) || endChar > len(chars) || startChar >= endChar {
			continue
		}

		startByte := charToByte[startChar]
		endByte := charToByte[endChar]

		var openSeq, closeSeq string
		if i == currentIdx {
			openSeq, closeSeq = hlCurOpen, hlCurClose
		} else {
			openSeq, closeSeq = hlOtherOpen, hlOtherClose
		}

		result = result[:startByte] + openSeq + result[startByte:endByte] + closeSeq + result[endByte:]
	}

	return result
}

// findNextMatchIndex 在純文字中尋找下一個匹配項的索引。
func findNextMatchIndex(content, query string, afterCharIdx int, caseSensitive bool) int {
	searchContent := content
	searchQuery := query
	if !caseSensitive {
		searchContent = strings.ToLower(content)
		searchQuery = strings.ToLower(query)
	}

	start := afterCharIdx
	if start >= len(searchContent) {
		return -1
	}

	idx := strings.Index(searchContent[start:], searchQuery)
	if idx == -1 {
		return -1
	}
	return start + idx
}
