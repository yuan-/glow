package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stepModel 用一個訊息推進 model（忽略回傳的 command）。
func stepModel(m model, msg tea.Msg) model {
	nm, _ := m.Update(msg)
	model, ok := nm.(model)
	if !ok {
		panic("unexpected model type")
	}
	return model
}

// buildLongDoc 建立一份夠長的測試文件。
func buildLongDoc(lines int) string {
	var b strings.Builder
	b.WriteString("# Doc\n\n")
	for i := 0; i < lines; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

// TestPositionMemory 驗證「離開文件再回來時記住捲動位置」：
// esc 離開 → positionMem 記錄 → 重新進入同一檔案 → 渲染後還原位置。
func TestPositionMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	doc := buildLongDoc(60)
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Path: path, GlamourEnabled: false}
	m := newModel(cfg, "").(model)

	// 設定終端尺寸
	m = stepModel(m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// 模擬渲染完成並捲動到行 40
	m.pager.setContent(strings.Repeat("x\n", 100))
	m.pager.viewport.SetYOffset(40)

	// esc 離開（回文件列表）
	m = stepModel(m, keySpecial(tea.KeyEscape))
	if m.state != stateShowStash {
		t.Fatalf("state = %v, want stash", m.state)
	}
	if m.positionMem[path] != 40 {
		t.Fatalf("positionMem = %d, want 40", m.positionMem[path])
	}
	// 離開後 currentDocument 應清空（區分「重新進入」與「重新載入」）
	if m.pager.currentDocument.localPath != "" {
		t.Fatal("currentDocument should be cleared after unload")
	}

	// 重新進入同一檔案
	m = stepModel(m, fetchedMarkdownMsg(&markdown{localPath: path, Body: doc}))
	if m.pager.pendingRestoreY != 40 {
		t.Fatalf("pendingRestoreY = %d, want 40", m.pager.pendingRestoreY)
	}

	// 模擬渲染完成
	var rendered strings.Builder
	for i := 0; i < 100; i++ {
		rendered.WriteString("r" + strconv.Itoa(i) + "\n")
	}
	m = stepModel(m, contentRenderedMsg(rendered.String()))
	if m.state != stateShowDocument {
		t.Fatalf("state = %v, want document", m.state)
	}
	if m.pager.viewport.YOffset() != 40 {
		t.Fatalf("YOffset = %d, want 40", m.pager.viewport.YOffset())
	}

	// 再次離開時應記錄「還原後」的新位置
	m.pager.viewport.SetYOffset(55)
	m = stepModel(m, keySpecial(tea.KeyEscape))
	if m.positionMem[path] != 55 {
		t.Fatalf("positionMem = %d, want 55", m.positionMem[path])
	}
}

// TestPositionMemoryReloadKeepsPosition 驗證「重新載入同一檔案」（r 鍵流程）
// 會保持在目前捲動位置，而不是跳回記錄位置。
func TestPositionMemoryReloadKeepsPosition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	doc := buildLongDoc(60)
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Path: path, GlamourEnabled: false}
	m := newModel(cfg, "").(model)
	m = stepModel(m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// 第一次進入時先記錄一個不同的位置
	m.positionMem[path] = 10

	// 進入文件並捲到 30
	m = stepModel(m, fetchedMarkdownMsg(&markdown{localPath: path, Body: doc}))
	m = stepModel(m, contentRenderedMsg(strings.Repeat("x\n", 100)))
	m.pager.viewport.SetYOffset(30)

	// 重新載入同一檔案（r 鍵 → loadLocalMarkdown → fetchedMarkdownMsg）
	m = stepModel(m, fetchedMarkdownMsg(&markdown{localPath: path, Body: doc}))
	if m.pager.pendingRestoreY != 30 {
		t.Fatalf("pendingRestoreY = %d, want 30 (current, not remembered 10)", m.pager.pendingRestoreY)
	}
	m = stepModel(m, contentRenderedMsg(strings.Repeat("x\n", 100)))
	if m.pager.viewport.YOffset() != 30 {
		t.Fatalf("YOffset = %d, want 30", m.pager.viewport.YOffset())
	}
}
