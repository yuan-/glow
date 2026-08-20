package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	runewidth "github.com/mattn/go-runewidth"
	"github.com/charmbracelet/log"
	"github.com/muesli/reflow/truncate"
)

// bookmarkInfo 代表一個 bookmark 位置（渲染內容中的行號）。
type bookmarkInfo struct {
	Y     int       `json:"y"`
	Label string    `json:"label"`
	Saved time.Time `json:"saved"`
}

// bookmarkFileData 是 bookmarks JSON 檔案的資料結構。
type bookmarkFileData struct {
	FileBookmarks map[string][]bookmarkInfo `json:"file_bookmarks"`
}

// bookmarkStore 管理 bookmark 的持久化：單一 JSON 檔案，依檔案路徑分組。
type bookmarkStore struct {
	filePath string
	data     bookmarkFileData
}

// defaultBookmarkPath 回傳預設 bookmarks 儲存位置
// （<UserConfigDir>/glow/bookmarks.json）。
func defaultBookmarkPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "glow", "bookmarks.json")
}

// newBookmarkStore 從預設位置載入 bookmarks（不存在或損毀時重新開始）。
func newBookmarkStore() *bookmarkStore {
	return newBookmarkStoreAt(defaultBookmarkPath())
}

// newBookmarkStoreAt 在指定檔案位置建立 bookmark store（供測試使用）。
func newBookmarkStoreAt(path string) *bookmarkStore {
	s := &bookmarkStore{
		filePath: path,
		data:     bookmarkFileData{FileBookmarks: make(map[string][]bookmarkInfo)},
	}
	if path != "" {
		if err := s.load(); err != nil {
			log.Debug("failed to load bookmarks", "path", path, "error", err)
		}
	}
	return s
}

func (s *bookmarkStore) load() error {
	b, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var d bookmarkFileData
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	if d.FileBookmarks == nil {
		d.FileBookmarks = make(map[string][]bookmarkInfo)
	}
	s.data = d
	return nil
}

// save 將 bookmarks 寫回磁碟。
func (s *bookmarkStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(&s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, b, 0o644)
}

// forFile 回傳某檔案的全部 bookmarks（依行號排序的副本）。
func (s *bookmarkStore) forFile(path string) []bookmarkInfo {
	if path == "" {
		return nil
	}
	items := s.data.FileBookmarks[path]
	out := make([]bookmarkInfo, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool { return out[i].Y < out[j].Y })
	return out
}

// add 新增一個 bookmark（同行去重）。已存在時回傳 false。
func (s *bookmarkStore) add(path string, y int, label string) (bool, error) {
	if path == "" || s.filePath == "" {
		return false, nil
	}
	if s.data.FileBookmarks == nil {
		s.data.FileBookmarks = make(map[string][]bookmarkInfo)
	}
	for _, bm := range s.data.FileBookmarks[path] {
		if bm.Y == y {
			return false, nil
		}
	}
	s.data.FileBookmarks[path] = append(s.data.FileBookmarks[path], bookmarkInfo{
		Y:     y,
		Label: label,
		Saved: time.Now(),
	})
	return true, s.save()
}

// bookmarkListModel 管理 bookmark 列表彈出視窗的狀態與邏輯。
type bookmarkListModel struct {
	visible    bool
	items      []bookmarkInfo
	fileNote   string
	sel        int
	scrollTop  int // 滾動視窗的第一個 bookmark 索引
	maxVisible int // 面板最多顯示的列數（依終端大小計算）
}

// open 開啟列表（items 為空時保持隱藏）。
func (m *bookmarkListModel) open(items []bookmarkInfo, fileNote string, maxVisible int) {
	if len(items) == 0 {
		return
	}
	m.items = items
	m.fileNote = fileNote
	m.maxVisible = maxVisible
	m.sel = 0
	m.scrollTop = 0
	m.visible = true
}

// closeList 關閉列表。
func (m *bookmarkListModel) closeList() {
	m.visible = false
}

// selected 回傳目前選中的 bookmark。
func (m bookmarkListModel) selected() *bookmarkInfo {
	if !m.visible || m.sel < 0 || m.sel >= len(m.items) {
		return nil
	}
	return &m.items[m.sel]
}

// moveSel 上/下移動選取，並維持選取項在可見視窗內。
func (m *bookmarkListModel) moveSel(delta int) {
	if len(m.items) == 0 {
		return
	}
	visible := m.maxVisible
	if visible <= 0 || visible > len(m.items) {
		visible = len(m.items)
	}
	m.sel += delta
	if m.sel < 0 {
		m.sel = 0
	}
	if m.sel >= len(m.items) {
		m.sel = len(m.items) - 1
	}
	if m.sel < m.scrollTop {
		m.scrollTop = m.sel
	}
	if m.sel >= m.scrollTop+visible {
		m.scrollTop = m.sel - visible + 1
	}
}

// windowRange 回傳目前滾動視窗的 [start, end)。
func (m bookmarkListModel) windowRange() (start, end int) {
	if m.maxVisible <= 0 || m.maxVisible > len(m.items)-m.scrollTop {
		end = len(m.items)
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

// panel 繪製 bookmark 列表浮動面板（含邊框）。
// width/areaHeight 為終端寬度與文件區域高度。回傳 "" 表示無法顯示。
func (m bookmarkListModel) panel(width, areaHeight int, s Styles) string {
	if !m.visible || width < 30 || areaHeight < 9 {
		return ""
	}

	panelW := min(width-8, 72)
	contentW := panelW - 6 // 扣除 border(2) + padding(4)
	if contentW < 10 {
		return ""
	}

	maxRows := m.maxVisible
	if maxRows <= 0 {
		maxRows = areaHeight - 7
	}
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > len(m.items) {
		maxRows = len(m.items)
	}

	start, end := m.windowRange()

	var b strings.Builder
	header := "🔖 Bookmarks"
	if m.fileNote != "" {
		header += "  " + m.fileNote
	}
	if len(m.items) > maxRows {
		header += fmt.Sprintf("  (%d/%d)", start+1, len(m.items))
	}
	b.WriteString(s.tocTitleStyle.Render(header))
	b.WriteRune('\n')

	for i := start; i < end; i++ {
		bm := m.items[i]
		when := bm.Saved.Format("2006-01-02 15:04")
		// 列的固定開銷（編號 + 行號 + 時間 + 間距）
		overhead := len(fmt.Sprintf("%2d  L%-5d %s  (%s)", 0, 0, "", when))
		label := bm.Label
		if maxLabel := contentW - overhead; maxLabel > 0 && runewidth.StringWidth(label) > maxLabel {
			label = truncate.String(label, uint(maxLabel))
		}
		row := fmt.Sprintf("%2d  L%-5d %s  (%s)", i+1, bm.Y+1, label, when)
		if i == m.sel {
			row = s.tocSelectedStyle.Render(row)
		}
		b.WriteString(row)
		if i < end-1 {
			b.WriteRune('\n')
		}
	}
	b.WriteRune('\n')
	b.WriteString(s.tocHintStyle.Render("↑/↓ move   enter jump   esc close"))

	return s.tocPanelStyle.Width(contentW).Render(b.String())
}

// stripANSI 移除字串中的 ANSI 逸序序列（用於提取純文字片段）。
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}
