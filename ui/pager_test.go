package ui

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: string(r)}
}

func keySpecial(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

// buildTestDoc 建立一份夠長的測試文件（原始 markdown 與模擬渲染結果）。
// 渲染結果模仿 glamour 輸出：開頭空行、每行 2 空格縮排、h2+ 保留 # 前綴。
// 結構：
//
//	line 0: ""
//	line 1: "  Title"
//	line 2: ""
//	line 3: "  filler a"
//	line 4: "  filler b"
//	line 5: ""
//	line 6:  "  ## Section 1"   (之後每 4 行一個 section)
//	line 10: "  ## Section 2"
//	...
func buildTestDoc(sections int) (body, rendered string) {
	var b, r strings.Builder
	b.WriteString("# Title\n\nfiller a\nfiller b\n\n")
	r.WriteString("\n  Title\n\n  filler a\n  filler b\n\n")
	for i := 1; i <= sections; i++ {
		s := strconv.Itoa(i)
		b.WriteString("## Section " + s + "\nline " + s + ".1\nline " + s + ".2\n\n")
		r.WriteString("  ## Section " + s + "\n  line " + s + ".1\n  line " + s + ".2\n\n")
	}
	return b.String(), r.String()
}

// newTestPager 建立一個已載入文件的 pagerModel。
func newTestPager(t *testing.T, body, rendered string, w, h int) pagerModel {
	t.Helper()
	common := &commonModel{width: w, height: h, styles: newStyles(true)}
	m := newPagerModel(common)
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(h - 1)
	m.currentDocument = markdown{Body: body, Note: "test.md", localPath: "/test.md"}
	m.tocRaw = body
	m.tocRendered = rendered
	m.setContent(rendered)
	m.bookmarks = newBookmarkStoreAt(filepath.Join(t.TempDir(), "bookmarks.json"))
	return m
}

// TestPagerBookmarkFlow 驗證 bookmark 的記錄（m）、f2/f3 跳躍、
// 以及 enter 開啟列表 → 移動 → 跳轉 → 關閉的流程。
func TestPagerBookmarkFlow(t *testing.T) {
	body, rendered := buildTestDoc(12)
	m := newTestPager(t, body, rendered, 80, 20)

	// 沒有 bookmark 時 f2/f3 應顯示狀態訊息且不移動
	m, _ = m.update(keySpecial(tea.KeyF3))
	if m.viewport.YOffset() != 0 || m.statusMessage != "No bookmarks" {
		t.Fatalf("YOffset=%d status=%q", m.viewport.YOffset(), m.statusMessage)
	}
	m, _ = m.update(keySpecial(tea.KeyF2))
	if m.statusMessage != "No bookmarks" {
		t.Fatalf("status=%q", m.statusMessage)
	}

	// 捲到行 10（Section 2 標題）並按 m 記錄 bookmark
	m.setViewportTo(10)
	m, _ = m.update(keyRune('m'))
	if m.statusMessage != "Bookmark saved: Section 2" {
		t.Fatalf("status = %q, want \"Bookmark saved: Section 2\"", m.statusMessage)
	}

	// 同行重複記錄 → 提示已存在
	m, _ = m.update(keyRune('m'))
	if m.statusMessage != "Bookmark already saved here" {
		t.Fatalf("status = %q", m.statusMessage)
	}

	// 再記錄一個在行 11 的 bookmark（標籤同樣是上方的 Section 2）
	m.setViewportTo(11)
	m, _ = m.update(keyRune('m'))
	if m.statusMessage != "Bookmark saved: Section 2" {
		t.Fatalf("status = %q", m.statusMessage)
	}

	// f2 → 上一個 bookmark（行 10）
	m, _ = m.update(keySpecial(tea.KeyF2))
	if m.viewport.YOffset() != 10 || m.statusMessage != "Bookmark: Section 2" {
		t.Fatalf("YOffset=%d status=%q", m.viewport.YOffset(), m.statusMessage)
	}

	// f3 → 下一個 bookmark（行 11）
	m, _ = m.update(keySpecial(tea.KeyF3))
	if m.viewport.YOffset() != 11 {
		t.Fatalf("YOffset=%d, want 11", m.viewport.YOffset())
	}

	// f3 再按一次 → 沒有下一個
	m, _ = m.update(keySpecial(tea.KeyF3))
	if m.viewport.YOffset() != 11 || m.statusMessage != "No next bookmark" {
		t.Fatalf("YOffset=%d status=%q", m.viewport.YOffset(), m.statusMessage)
	}

	// enter 開啟 bookmark 列表
	m, _ = m.update(keySpecial(tea.KeyEnter))
	if !m.bookmarkList.visible || len(m.bookmarkList.items) != 2 {
		t.Fatalf("list visible=%v items=%d", m.bookmarkList.visible, len(m.bookmarkList.items))
	}
	// 列表開啟時 View 應繪製面板且總高度不變
	view := m.View()
	if lines := strings.Count(view, "\n"); lines+1 != 20 {
		t.Errorf("view height = %d lines, want 20", lines+1)
	}
	if !strings.Contains(view, "Bookmarks") {
		t.Error("bookmark panel should be visible in View()")
	}

	// 列表開啟時其他按鍵不應洩漏（例如 g 不跳到頂端）
	m, _ = m.update(keyRune('g'))
	if m.viewport.YOffset() != 11 || !m.bookmarkList.visible {
		t.Fatalf("g while list open: YOffset=%d visible=%v", m.viewport.YOffset(), m.bookmarkList.visible)
	}

	// j 選中第二個 → enter 跳轉並關閉
	m, _ = m.update(keyRune('j'))
	if m.bookmarkList.sel != 1 {
		t.Fatalf("sel=%d, want 1", m.bookmarkList.sel)
	}
	m, _ = m.update(keySpecial(tea.KeyEnter))
	if m.bookmarkList.visible {
		t.Error("list should close after enter")
	}
	if m.viewport.YOffset() != 11 {
		t.Errorf("YOffset=%d, want 11", m.viewport.YOffset())
	}

	// 重新開啟後 esc 應只關閉列表
	m, _ = m.update(keySpecial(tea.KeyEnter))
	m, _ = m.update(keySpecial(tea.KeyEscape))
	if m.bookmarkList.visible {
		t.Error("list should close after esc")
	}
	if m.viewport.YOffset() != 11 {
		t.Errorf("YOffset=%d, want 11 (unchanged)", m.viewport.YOffset())
	}
}

// TestPagerTocFlow 驗證 TOC 的開啟、上下移動、Enter 跳轉流程。
func TestPagerTocFlow(t *testing.T) {
	body, rendered := buildTestDoc(12)
	m := newTestPager(t, body, rendered, 80, 20)

	// 按下 t 開啟 TOC
	m, _ = m.update(keyRune('t'))
	if !m.toc.visible() {
		t.Fatal("TOC should be visible after pressing t")
	}
	if len(m.toc.headings) != 13 { // Title + 12 sections
		t.Fatalf("expected 13 headings, got %d", len(m.toc.headings))
	}
	if m.toc.lineOf[0] != 1 || m.toc.lineOf[2] != 10 {
		t.Fatalf("unexpected heading lines: %v", m.toc.lineOf)
	}

	// 向下移動兩次 → 選中 Section 2（行 10）
	m, _ = m.update(keySpecial(tea.KeyDown))
	m, _ = m.update(keySpecial(tea.KeyDown))
	if m.toc.selectedIdx != 2 {
		t.Fatalf("selectedIdx = %d, want 2", m.toc.selectedIdx)
	}

	// View 應該把面板貼在文件區域內，總高度恰好等於終端高度
	view := m.View()
	if lines := strings.Count(view, "\n"); lines+1 != 20 {
		t.Errorf("view height = %d lines, want 20", lines+1)
	}
	if !strings.Contains(view, "Table of Contents") {
		t.Error("TOC panel should be visible in View()")
	}

	// 按 h 不應該關閉文件或捲動（TOC 消費所有按鍵）
	yBefore := m.viewport.YOffset()
	m, _ = m.update(keyRune('h'))
	if m.viewport.YOffset() != yBefore || !m.toc.visible() {
		t.Error("pressing h while TOC is open should be a no-op")
	}

	// Enter 跳轉：Section 2 在行 10 → YOffset = 10-2 = 8，並關閉 TOC
	m, _ = m.update(keySpecial(tea.KeyEnter))
	if m.toc.visible() {
		t.Error("TOC should close after Enter")
	}
	if m.viewport.YOffset() != 8 {
		t.Errorf("YOffset = %d, want 8", m.viewport.YOffset())
	}

	// 按 esc 應該只關閉 TOC，不離開文件（頂層 model 有 guard 放行）
	m, _ = m.update(keyRune('t'))
	if !m.toc.visible() {
		t.Fatal("TOC should reopen")
	}
	m, _ = m.update(keySpecial(tea.KeyEscape))
	if m.toc.visible() {
		t.Error("TOC should close after esc")
	}
}

// TestPagerSearchFlow 驗證搜尋的啟動、輸入、確認、n/N 切換、結束流程。
func TestPagerSearchFlow(t *testing.T) {
	body, rendered := buildTestDoc(12)
	m := newTestPager(t, body, rendered, 80, 20)
	areaH := 19 // h - 1

	// 按下 / 啟動搜尋：viewport 應該縮一行給搜尋列
	m, _ = m.update(keyRune('/'))
	if !m.search.IsSearching() {
		t.Fatal("search should be active after pressing /")
	}
	if m.viewport.Height() != areaH-1 {
		t.Errorf("viewport.Height = %d, want %d (one row for search input)", m.viewport.Height(), areaH-1)
	}

	// 輸入 "line"：應該即時找到 24 個匹配（12 sections × 2 lines）
	for _, c := range []rune{'l', 'i', 'n', 'e'} {
		m, _ = m.update(keyRune(c))
	}
	if m.search.query != "line" {
		t.Fatalf("query = %q, want \"line\"", m.search.query)
	}
	if len(m.search.matches) != 24 {
		t.Fatalf("matches = %d, want 24", len(m.search.matches))
	}

	// View 應該顯示搜尋列且總高度仍等於終端高度
	view := m.View()
	if lines := strings.Count(view, "\n"); lines+1 != 20 {
		t.Errorf("view height = %d lines, want 20", lines+1)
	}
	if !strings.Contains(view, "enter search") {
		t.Error("search line with hint should be visible")
	}
	if !strings.Contains(view, "24 matches") {
		t.Error("match count should be shown while typing")
	}

	// Enter 確認：跳到第一個匹配（行 7）→ YOffset = 7-5 = 2
	m, _ = m.update(keySpecial(tea.KeyEnter))
	if m.search.state != searchConfirmed {
		t.Fatal("search should be confirmed after Enter")
	}
	if m.search.currentIdx != 0 {
		t.Fatalf("currentIdx = %d, want 0", m.search.currentIdx)
	}
	if m.viewport.YOffset() != 2 {
		t.Errorf("YOffset = %d, want 2", m.viewport.YOffset())
	}

	// n 到下一個匹配（行 8）→ YOffset = 8-5 = 3
	m, _ = m.update(keyRune('n'))
	if m.search.currentIdx != 1 || m.viewport.YOffset() != 3 {
		t.Errorf("after n: currentIdx = %d, YOffset = %d (want 1, 3)", m.search.currentIdx, m.viewport.YOffset())
	}

	// N 回退
	m, _ = m.update(keyRune('N'))
	if m.search.currentIdx != 0 || m.viewport.YOffset() != 2 {
		t.Errorf("after N: currentIdx = %d, YOffset = %d (want 0, 2)", m.search.currentIdx, m.viewport.YOffset())
	}

	// esc 結束搜尋：內容與版面還原
	m, _ = m.update(keySpecial(tea.KeyEscape))
	if m.search.IsSearching() {
		t.Fatal("search should stop after esc")
	}
	if m.viewport.Height() != areaH {
		t.Errorf("viewport.Height = %d, want %d after leaving search", m.viewport.Height(), areaH)
	}
	// 內容還原：viewport 顯示的應該是最上面 19 行渲染內容（忽略行尾補白）
	rlines := strings.Split(rendered, "\n")
	got := strings.Split(m.viewport.View(), "\n")
	if len(got) < 19 {
		t.Fatalf("viewport.View() returned %d lines, want >= 19", len(got))
	}
	for i := 0; i < 19; i++ {
		// viewport 行會被補白到固定寬度，比較時忽略行尾空白
		if strings.TrimRight(got[i], " ") != rlines[i] {
			t.Errorf("restored line %d = %q, want %q", i, got[i], rlines[i])
		}
	}
}

// TestPagerBookmarkLabel 驗證 bookmark 標籤的決定邏輯：
// 上方最近標題 → 目前列文字片段 → "line N"。
func TestPagerBookmarkLabel(t *testing.T) {
	body, rendered := buildTestDoc(12)
	m := newTestPager(t, body, rendered, 80, 20)

	// 行 11 上方最近的標題是 Section 2（行 10）
	m.setViewportTo(11)
	if got := m.bookmarkLabel(); got != "Section 2" {
		t.Fatalf("label = %q, want \"Section 2\"", got)
	}

	// 行 4 上方最近的標題是 Title（行 1）
	m.setViewportTo(4)
	if got := m.bookmarkLabel(); got != "Title" {
		t.Fatalf("label = %q, want \"Title\"", got)
	}

	// 沒有標題的文件 → 用目前列文字片段（文件要夠長才能捲動）
	plainBody := strings.Repeat("plain text line\n", 30)
	plain := newTestPager(t, plainBody, plainBody, 80, 20)
	plain.setViewportTo(1)
	if got := plain.bookmarkLabel(); got != "plain text line" {
		t.Fatalf("label = %q, want \"plain text line\"", got)
	}

	// 空列 → "line N"
	empty := newTestPager(t, "", "", 80, 20)
	if got := empty.bookmarkLabel(); got != "line 1" {
		t.Fatalf("label = %q, want \"line 1\"", got)
	}
}

// TestPagerSearchNoKeyLeak 驗證搜尋輸入時按鍵不會洩漏到文件導航。
func TestPagerSearchNoKeyLeak(t *testing.T) {
	body, rendered := buildTestDoc(12)
	m := newTestPager(t, body, rendered, 80, 20)

	m, _ = m.update(keyRune('/'))
	m, _ = m.update(keyRune('h')) // 輸入 h（常規模式下是「回文件列表」）
	m, _ = m.update(keyRune('j')) // 輸入 j（常規模式下是向下捲動）

	if m.search.query != "hj" {
		t.Fatalf("query = %q, want \"hj\"", m.search.query)
	}
	if m.viewport.YOffset() != 0 {
		t.Errorf("viewport should not scroll while typing, YOffset = %d", m.viewport.YOffset())
	}

	// 無匹配的查詢
	m, _ = m.update(keyRune('x'))
	if m.search.query != "hjx" {
		t.Fatalf("query = %q, want \"hjx\"", m.search.query)
	}
	if len(m.search.matches) != 0 {
		t.Errorf("matches = %d, want 0", len(m.search.matches))
	}
	view := m.View()
	if !strings.Contains(view, "無匹配結果") {
		t.Error("no-match info should be shown")
	}
}
