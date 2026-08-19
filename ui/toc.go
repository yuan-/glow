package ui

import (
	"fmt"
	"strings"

	"charm.land/glow/v3/utils"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/truncate"
)

// tocState 代表 TOC 彈出視窗的狀態。
type tocState int

const (
	tocHidden tocState = iota
	tocVisible
)

// tocModel 管理 TOC（目錄）彈出視窗的狀態與邏輯。
//
// 跳轉位置在開啟時就根據渲染後內容預計算（lineOf），
// 因為渲染結果才是 viewport 實際顯示的內容。
type tocModel struct {
	state       tocState
	headings    []utils.HeadingInfo
	lineOf      []int // 各標題在渲染後內容中的行號，找不到為 -1
	renderedDoc string // 渲染後的文件內容，用於定位標題行號
	selectedIdx int
	scrollTop   int // 滾動視窗的第一個標題索引
	maxVisible  int // 面板最多顯示的標題列數（依終端大小計算）
}

// newTocModel 建立一個新的 TOC model。
func newTocModel() tocModel {
	return tocModel{state: tocHidden}
}

// updateToc 根據原始 Markdown 內容與渲染後內容重建 TOC。
// 回傳值表示是否有標題（false 時 TOC 保持隱藏）。
func (m *tocModel) updateToc(rawMarkdown string, renderedDoc string) bool {
	hs := utils.ExtractHeadings(rawMarkdown)
	if len(hs) == 0 {
		m.state = tocHidden
		return false
	}

	m.headings = hs
	m.renderedDoc = renderedDoc
	m.lineOf = make([]int, len(hs))
	for i := range m.lineOf {
		m.lineOf[i] = -1
	}
	if renderedDoc != "" {
		m.lineOf = utils.FindHeadingLines(renderedDoc, hs)
	}
	m.selectedIdx = 0
	m.scrollTop = 0
	m.state = tocVisible
	return true
}

// refresh 在文件重新渲染後更新標題行號（保留目前選取與捲動位置）。
func (m *tocModel) refresh(renderedDoc string) {
	if m.state != tocVisible || len(m.headings) == 0 {
		return
	}
	m.renderedDoc = renderedDoc
	if renderedDoc != "" {
		m.lineOf = utils.FindHeadingLines(renderedDoc, m.headings)
	}
}

// closeToc 關閉 TOC 視窗。
func (m *tocModel) closeToc() {
	m.state = tocHidden
}

// visible 回傳 TOC 是否可見（有標題且已開啟）。
func (m tocModel) visible() bool {
	return m.state == tocVisible && len(m.headings) > 0
}

// selectedLine 回傳目前選中標題對應的渲染行號，找不到回傳 -1。
func (m tocModel) selectedLine() int {
	if !m.visible() || m.selectedIdx < 0 || m.selectedIdx >= len(m.lineOf) {
		return -1
	}
	return m.lineOf[m.selectedIdx]
}

// moveSel 上/下移動選取，並維持選取項在可見視窗內。
func (m *tocModel) moveSel(delta, visible int) {
	if len(m.headings) == 0 {
		return
	}
	if visible <= 0 || visible > len(m.headings) {
		visible = len(m.headings)
	}
	m.selectedIdx += delta
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
	}
	if m.selectedIdx >= len(m.headings) {
		m.selectedIdx = len(m.headings) - 1
	}
	if m.selectedIdx < m.scrollTop {
		m.scrollTop = m.selectedIdx
	}
	if m.selectedIdx >= m.scrollTop+visible {
		m.scrollTop = m.selectedIdx - visible + 1
	}
}

// windowRange 回傳目前滾動視窗的 [start, end)。
func (m tocModel) windowRange() (start, end int) {
	if m.maxVisible <= 0 || m.maxVisible > len(m.headings)-m.scrollTop {
		end = len(m.headings)
	} else {
		end = m.scrollTop + m.maxVisible
	}
	start = m.scrollTop
	if start < 0 {
		start = 0
	}
	if start > end {
		start = end
	}
	return start, end
}

// panel 繪製 TOC 浮動面板（含邊框）。
// width/areaHeight 為終端寬度與文件區域高度，用於決定面板大小與可顯示列數。
// 回傳 "" 表示無法顯示（隱藏或視窗太小）。
func (m tocModel) panel(width, areaHeight int, s Styles) string {
	if !m.visible() || width < 30 || areaHeight < 9 {
		return ""
	}

	panelW := min(width-8, 64)
	contentW := panelW - 6 // 扣除 border(2) + padding(4)
	if contentW < 10 {
		return ""
	}

	// 面板高度 = 標題列 + 標頭(1) + 提示列(1) + padding(2) + border(2)
	// maxVisible 由 pager 在開啟時依終端大小設定；未設定時用保守預設值。
	maxRows := m.maxVisible
	if maxRows <= 0 {
		maxRows = areaHeight - 7
	}
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > len(m.headings) {
		maxRows = len(m.headings)
	}

	start, end := m.windowRange()

	var b strings.Builder
	header := "📑 Table of Contents"
	if len(m.headings) > maxRows {
		header += fmt.Sprintf("  (%d/%d)", start+1, len(m.headings))
	}
	b.WriteString(s.tocTitleStyle.Render(header))
	b.WriteRune('\n')

	for i := start; i < end; i++ {
		h := m.headings[i]
		indent := strings.Repeat("  ", h.Level-1)
		label := strings.Repeat("#", h.Level) + " " + h.Text
		maxLabel := contentW - runewidth.StringWidth(indent) - 1
		if maxLabel > 0 && runewidth.StringWidth(label) > maxLabel {
			label = truncate.String(label, uint(maxLabel))
		}
		row := indent + label
		if i == m.selectedIdx {
			row = s.tocSelectedStyle.Render(row)
		}
		b.WriteString(row)
		if i < end-1 {
			b.WriteRune('\n')
		}
	}
	b.WriteRune('\n')
	b.WriteString(s.tocHintStyle.Render("↑/↓ move   enter jump   esc close"))

	panelStyle := s.tocPanelStyle.Width(contentW)

	return panelStyle.Render(b.String())
}
