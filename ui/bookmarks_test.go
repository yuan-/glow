package ui

import (
	"path/filepath"
	"testing"
)

// TestBookmarkStoreRoundTrip 驗證 bookmark 的新增、去重、排序與持久化。
func TestBookmarkStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bookmarks.json")
	s := newBookmarkStoreAt(path)

	added, err := s.add("/a.md", 10, "Section 1")
	if err != nil || !added {
		t.Fatalf("add: added=%v err=%v", added, err)
	}
	// 同一行去重
	added, err = s.add("/a.md", 10, "dup")
	if err != nil || added {
		t.Fatalf("duplicate add: added=%v err=%v", added, err)
	}
	s.add("/a.md", 5, "Top")
	s.add("/b.md", 1, "B")

	got := s.forFile("/a.md")
	if len(got) != 2 || got[0].Y != 5 || got[1].Y != 10 {
		t.Fatalf("forFile = %+v", got)
	}
	if got[0].Label != "Top" || got[1].Label != "Section 1" {
		t.Fatalf("labels = %q %q", got[0].Label, got[1].Label)
	}
	if got[0].Saved.IsZero() {
		t.Error("saved timestamp should be set")
	}

	// 持久化：重新載入
	s2 := newBookmarkStoreAt(path)
	got2 := s2.forFile("/a.md")
	if len(got2) != 2 || got2[0].Label != "Top" || got2[1].Y != 10 {
		t.Fatalf("reload forFile = %+v", got2)
	}
	if len(s2.forFile("/b.md")) != 1 {
		t.Error("b.md bookmarks should persist")
	}
	if len(s2.forFile("/missing.md")) != 0 {
		t.Error("missing file should have no bookmarks")
	}
}

// TestBookmarkStoreNoPath 驗證無路徑時不執行任何操作。
func TestBookmarkStoreNoPath(t *testing.T) {
	s := &bookmarkStore{filePath: ""}
	added, err := s.add("x", 1, "y")
	if err != nil || added {
		t.Fatalf("empty path: added=%v err=%v", added, err)
	}
	if len(s.forFile("x")) != 0 {
		t.Error("no bookmarks expected")
	}
}

// TestBookmarkListPanel 驗證 bookmark 列表面板的繪製與選取。
func TestBookmarkListPanel(t *testing.T) {
	l := bookmarkListModel{}
	l.open([]bookmarkInfo{
		{Y: 5, Label: "Top"},
		{Y: 10, Label: "Section 1"},
	}, "test.md", 10)

	if !l.visible {
		t.Fatal("list should be visible after open")
	}
	if bm := l.selected(); bm == nil || bm.Y != 5 {
		t.Fatalf("selected = %+v", bm)
	}

	l.moveSel(1)
	if bm := l.selected(); bm == nil || bm.Y != 10 {
		t.Fatalf("after moveSel(1): selected = %+v", bm)
	}
	// 底部鉗制
	l.moveSel(1)
	if bm := l.selected(); bm == nil || bm.Y != 10 {
		t.Fatalf("clamped at bottom: selected = %+v", bm)
	}

	view := l.panel(80, 24, newStyles(true))
	if view == "" {
		t.Fatal("panel should render")
	}
	start, end := l.windowRange()
	if start != 0 || end != 2 {
		t.Fatalf("windowRange = (%d, %d), want (0, 2)", start, end)
	}
	l.closeList()
	if l.visible {
		t.Error("list should close")
	}
	if l.panel(80, 24, newStyles(true)) != "" {
		t.Error("closed list should not render")
	}
}

// TestBookmarkListOpenEmpty 驗證空列表不會開啟。
func TestBookmarkListOpenEmpty(t *testing.T) {
	l := bookmarkListModel{}
	l.open(nil, "test.md", 10)
	if l.visible {
		t.Error("empty list should stay hidden")
	}
}
