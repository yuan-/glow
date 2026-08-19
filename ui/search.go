package ui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/glow/v3/utils"
)

// searchState 代表搜尋模式狀態。
type searchState int

const (
	searchHidden searchState = iota
	searchActive    // 正在輸入搜尋字串，即時高亮
	searchConfirmed // 已確認搜尋，可用 n/N 切換
)

// SearchModel 管理搜尋功能的狀態與邏輯。
type SearchModel struct {
	state      searchState
	input      textinput.Model
	query      string    // 目前搜尋字串
	matches    [][]int   // 匹配位置列表 [[start, end], ...]（rawContent 的 byte 偏移，不重疊）
	currentIdx int       // 當前匹配的索引（-1 表示無匹配）
	rendered   string    // 渲染後的文件內容（含 ANSI 色碼）
	rawContent string    // 原始渲染內容（不含 ANSI），用於計算匹配位置
	lineStarts []int     // rawContent 每一行開頭的 byte 偏移（用於匹配項→行號）
}

// NewSearchModel 建立一個新的搜尋 model。
func NewSearchModel() SearchModel {
	ti := textinput.New()
	ti.Placeholder = "搜尋"
	ti.Prompt = "/ "
	ti.CharLimit = 256
	ti.SetWidth(40)
	ti.Focus()

	return SearchModel{
		state: searchHidden,
		input: ti,
	}
}

// StartSearch 啟動搜尋模式。
func (m *SearchModel) StartSearch(renderedContent string) {
	m.state = searchActive
	m.query = ""
	m.matches = nil
	m.currentIdx = -1
	m.rendered = renderedContent
	m.rawContent = utils.StripANSI(renderedContent)
	m.lineStarts = []int{0}
	for i := 0; i < len(m.rawContent); i++ {
		if m.rawContent[i] == '\n' {
			m.lineStarts = append(m.lineStarts, i+1)
		}
	}
	m.input.SetValue("")
	m.input.Placeholder = "搜尋"
	m.input.Focus()
}

// StopSearch 停止搜尋模式，清除所有高亮。
func (m *SearchModel) StopSearch() {
	m.state = searchHidden
	m.query = ""
	m.matches = nil
	m.currentIdx = -1
	m.input.Blur()
}

// ConfirmSearch 確認搜尋，進入切換匹配項模式。
func (m *SearchModel) ConfirmSearch() tea.Cmd {
	if m.query == "" {
		m.StopSearch()
		return nil
	}

	m.state = searchConfirmed
	m.findMatches(m.rawContent, m.query, false) // 不區分大小寫
	m.currentIdx = -1

	// 跳到第一個匹配項
	m.NextMatch()

	m.input.Blur()
	return textinput.Blink
}

// NextMatch 跳到下一個匹配項。
func (m *SearchModel) NextMatch() {
	if len(m.matches) == 0 {
		return
	}
	if m.currentIdx < 0 {
		m.currentIdx = 0
	} else {
		m.currentIdx = (m.currentIdx + 1) % len(m.matches)
	}
}

// PrevMatch 跳到上一個匹配項。
func (m *SearchModel) PrevMatch() {
	if len(m.matches) == 0 {
		return
	}
	if m.currentIdx < 0 {
		m.currentIdx = len(m.matches) - 1
	} else {
		m.currentIdx = (m.currentIdx - 1 + len(m.matches)) % len(m.matches)
	}
}

// Update 處理搜尋輸入模式的鍵盤輸入（輸入字串、enter 確認、esc 取消）。
// 確認模式（confirmed）下的 n/N 導航由 pager 直接呼叫 NextMatch/PrevMatch 處理。
func (m *SearchModel) Update(msg tea.Msg) (SearchModel, tea.Cmd) {
	if m.state == searchHidden || m.state != searchActive {
		return *m, nil
	}

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.StopSearch()
			return *m, nil

		case "enter":
			return *m, m.ConfirmSearch()
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.query = m.input.Value()

	// 即時重新計算匹配（不區分大小寫）
	if len(m.rawContent) > 0 {
		m.findMatches(m.rawContent, m.query, false)
	}

	return *m, cmd
}

// findMatches 在內容中尋找所有匹配位置（不重疊，按位置升序）。
// 回傳的偏移是 content 的 byte 偏移。
// caseSensitive: 是否區分大小寫
func (m *SearchModel) findMatches(content, query string, caseSensitive bool) {
	m.matches = nil
	if query == "" {
		return
	}

	searchContent := content
	searchQuery := query
	if !caseSensitive {
		searchContent = strings.ToLower(content)
		searchQuery = strings.ToLower(query)
	}

	start := 0
	for {
		idx := strings.Index(searchContent[start:], searchQuery)
		if idx == -1 {
			break
		}
		actualStart := start + idx
		m.matches = append(m.matches, []int{actualStart, actualStart + len(query)})
		start = actualStart + len(query)
	}
}

// matchLine 回傳第 idx 個匹配項所在的行號（rawContent 的 0-based 行號，
// 與渲染後內容行號一致，因為兩邊只是 ANSI 序列的差異），找不到回傳 -1。
func (m *SearchModel) matchLine(idx int) int {
	if idx < 0 || idx >= len(m.matches) || len(m.lineStarts) == 0 {
		return -1
	}
	pos := m.matches[idx][0]
	// 第一個 lineStart > pos 的索引減一，即為匹配項所在行
	i := sort.Search(len(m.lineStarts), func(j int) bool {
		return m.lineStarts[j] > pos
	})
	line := i - 1
	if line < 0 {
		line = 0
	}
	return line
}

// GetHighlightedContent 回傳帶有高亮標記的渲染內容。
func (m *SearchModel) GetHighlightedContent() string {
	if m.state == searchHidden || len(m.matches) == 0 {
		return m.rendered
	}

	// 在 ANSI-aware 的方式下插入高亮序列
	result := highlightMatchesANSI(m.rendered, m.matches, m.currentIdx)
	return result
}

// GetMatchInfo 回傳目前匹配項的資訊。
func (m SearchModel) GetMatchInfo() string {
	if len(m.matches) == 0 {
		if m.query != "" {
			return "無匹配結果"
		}
		return ""
	}
	// 輸入中（尚未確認）時只顯示匹配數量
	if m.state == searchActive {
		return fmt.Sprintf("%d matches", len(m.matches))
	}
	return fmt.Sprintf("%d/%d", m.currentIdx+1, len(m.matches))
}

// IsSearching 回傳是否處於搜尋模式。
func (m SearchModel) IsSearching() bool {
	return m.state != searchHidden
}
