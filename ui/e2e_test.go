package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// writeKeys 將一組按鍵（raw 終端位元組）依序寫入 pipe，中間間隔 delay。
func writeKeys(t *testing.T, w io.Writer, keys []string, delay time.Duration) {
	t.Helper()
	for _, k := range keys {
		if _, err := io.WriteString(w, k); err != nil {
			t.Fatalf("write key %q: %v", k, err)
		}
		time.Sleep(delay)
	}
}

// sizedModel 包裝 model，在 Init 時補發一個 WindowSizeMsg。
// 非 TTY 的測試環境下 bubbletea 無法偵測終端尺寸，需要手動提供。
type sizedModel struct {
	inner tea.Model
}

func (s *sizedModel) Init() tea.Cmd {
	orig := s.inner.Init()
	return tea.Batch(
		func() tea.Msg { return tea.WindowSizeMsg{Width: 80, Height: 24} },
		orig,
	)
}

func (s *sizedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd := s.inner.Update(msg)
	s.inner = nm
	return s, cmd
}

func (s *sizedModel) View() tea.View {
	return s.inner.View()
}

// e2eProgram 是一個執行中的完整 tea.Program（pipe 輸入 / 捕捉輸出）。
type e2eProgram struct {
	keys *io.PipeWriter
	out  *bytes.Buffer
	done chan struct{}
	prog *tea.Program
}

// startE2EProgram 寫出測試 markdown 並啟動完整的 tea.Program。
func startE2EProgram(t *testing.T, doc string) *e2eProgram {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	out := &bytes.Buffer{}
	go func() { _, _ = io.Copy(out, outR) }()

	cfg := Config{Path: path, GlamourEnabled: true, GlamourStyle: "auto"}
	m := &sizedModel{inner: newModel(cfg, "")}
	p := tea.NewProgram(m, tea.WithInput(inR), tea.WithOutput(outW))
	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()

	return &e2eProgram{keys: inW, out: out, done: done, prog: p}
}

// stop 結束 program 並等待輸出 flushed。
func (e *e2eProgram) stop() {
	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		e.prog.Quit()
		<-e.done
	}
	e.keys.Close()
	time.Sleep(100 * time.Millisecond)
}

// plainScreen 回傳移除 ANSI 逸序序列的完整輸出。
func (e *e2eProgram) plainScreen() string {
	return stripTestANSI(e.out.String())
}

// tailStr 回傳字串最後 n 個字元（不足 n 時回傳全部）。
func tailStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// waitUntil 輪詢等待輸出包含指定字串（最多 timeout）。
func (e *e2eProgram) waitUntil(needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(e.plainScreen(), needle) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return strings.Contains(e.plainScreen(), needle)
}

// buildE2EDoc 建立一份含 8 個 section 的測試文件。
func buildE2EDoc(sections, linesPerSection int) string {
	var md strings.Builder
	md.WriteString("# E2E Title\n\n")
	for i := 1; i <= sections; i++ {
		fmt.Fprintf(&md, "## Section %d\n\n", i)
		for j := 0; j < linesPerSection; j++ {
			fmt.Fprintf(&md, "filler line %d-%d with some searchable text inside\n\n", i, j)
		}
	}
	return md.String()
}

// TestE2ETocAndSearch 透過完整的 tea.Program 驗證 TOC 與搜尋流程。
func TestE2ETocAndSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in short mode")
	}

	e := startE2EProgram(t, buildE2EDoc(8, 6))
	defer e.stop()

	// 等待初始渲染，然後操作：
	//   t → 開啟 TOC；down×3 → 選中 Section 3；enter → 跳轉；
	//   / → 搜尋；輸入 "section 5"；enter → 確認；n → 下一個；
	//   esc → 結束搜尋；q → 離開程式
	// 注意：q 是最后一个寫入的按鍵（pipe 無緩衝，程式結束後再寫會阻塞）。
	time.Sleep(800 * time.Millisecond)
	writeKeys(t, e.keys, []string{
		"t",         // 開啟 TOC
		"\x1b[B",    // down
		"\x1b[B",    // down
		"\x1b[B",    // down
		"\r",        // enter → 跳轉
		"/",         // 啟動搜尋
		"s", "e", "c", "t", "i", "o", "n", " ", "5", // 輸入 "section 5"
		"\r",        // 確認
		"n",         // 下一個匹配
		"\x1b",      // esc 結束搜尋
	}, 150*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	writeKeys(t, e.keys, []string{"q"}, 100*time.Millisecond) // 離開程式（最後一個按鍵）
	e.stop()

	plain := e.plainScreen()

	// TOC 面板應該曾經顯示
	if !strings.Contains(plain, "Table of Contents") {
		t.Error("TOC panel was never rendered")
	}
	// 搜尋輸入內容應該出現在輸入列
	if !strings.Contains(plain, "section 5") {
		t.Error("search query text not found in output")
	}
	// 搜尋確認後應顯示匹配計數
	if !strings.Contains(plain, "1/1") {
		t.Error("match info (1/1) not found in output")
	}
	// 跳轉後的內容：Section 5 應該曾經出現在畫面
	if !strings.Contains(plain, "Section 5") {
		t.Error("Section 5 content was never visible after jump")
	}
}

// TestE2EBookmarks 透過完整的 tea.Program 驗證 bookmark 流程：
// m 記錄 → 捲動 → m 記錄 → f3 跳轉 → enter 開啟列表 → esc → 離開。
func TestE2EBookmarks(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in short mode")
	}

	// 隔離 bookmark 儲存（避免測試污染真實使用者的 bookmarks.json）
	t.Setenv("APPDATA", t.TempDir())       // Windows
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // Linux

	e := startE2EProgram(t, buildE2EDoc(8, 6))
	defer e.stop()
	time.Sleep(800 * time.Millisecond)

	writeKeys(t, e.keys, []string{
		"m",          // 在頂端記錄第一個 bookmark
		"\x1b[B",     // down
		"\x1b[B",     // down
		"\x1b[B",     // down
		"\x1b[B",     // down
		"\x1b[B",     // down
		"\x1b[B",     // down
		"m",          // 記錄第二個 bookmark
		"\x1bOR",     // f3 → 跳到下一個 bookmark
		"\r",         // enter → 開啟 bookmark 列表
	}, 150*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	writeKeys(t, e.keys, []string{
		"\x1b", // esc 關閉列表
		"q",    // 離開程式（最後一個按鍵）
	}, 150*time.Millisecond)
	e.stop()

	plain := e.plainScreen()
	for _, want := range []string{
		"Bookmark saved", // 狀態訊息
		"Bookmarks",      // 列表面板
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("%q not found in output", want)
		}
	}
}

// TestE2EPositionMemory 驗證「離開文件再回來時記住捲動位置」：
// G 捲到底 → q 回文件列表 → enter 重新開啟同一檔案 → 應還原在底端。
func TestE2EPositionMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in short mode")
	}

	e := startE2EProgram(t, buildE2EDoc(8, 6))
	defer e.stop()
	time.Sleep(800 * time.Millisecond)

	// 捲到底端，esc 回文件列表
	writeKeys(t, e.keys, []string{
		"G",    // 捲到底端
		"\x1b", // esc → 回文件列表
	}, 150*time.Millisecond)

	// 等待文件列表載入完成（gitcha 非同步搜尋；列表列會顯示「just now」）
	if !e.waitUntil("just now", 10*time.Second) {
		t.Fatal("file listing was never loaded")
	}

	// 記錄重新開啟前的輸出長度；渲染器是增量 diff，只看「新輸出」才能
	// 判斷重新開啟後的位置
	before := e.out.Len()

	writeKeys(t, e.keys, []string{
		"\r", // enter → 重新開啟 e2e.md
	}, 150*time.Millisecond)
	time.Sleep(700 * time.Millisecond)
	writeKeys(t, e.keys, []string{"q"}, 100*time.Millisecond) // 離開程式（最後一個按鍵）
	e.stop()

	// 重新開啟後應還原在底端（100%），而不是開頭（E2E Title）
	delta := stripTestANSI(e.out.String()[before:])
	if !strings.Contains(delta, "100%") {
		t.Error("after re-entering, document should be restored at the bottom (100% expected)")
	}
	if strings.Contains(delta, "E2E Title") {
		t.Error("after re-entering, document should not be at the top (E2E Title found)")
	}
}

func stripTestANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
