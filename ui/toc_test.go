package ui

import (
	"testing"
)

func TestTocModel_UpdateAndDisplay(t *testing.T) {
	m := newTocModel()
	
	rawMarkdown := `# Heading 1
Some text

## Heading 2
More text

### Heading 3
Even more text`
	
	renderedContent := "Heading 1\n\nHeading 2\n\nHeading 3"
	
	// Test updateToc with both parameters
	m.updateToc(rawMarkdown, renderedContent)
	
	if m.state != tocVisible {
		t.Errorf("after updateToc, state = %v, want %v", m.state, tocVisible)
	}
	
	if len(m.headings) != 3 {
		t.Errorf("expected 3 headings, got %d", len(m.headings))
	}
}

func TestTocModel_Close(t *testing.T) {
	m := newTocModel()
	
	rawMarkdown := "# Heading"
	renderedContent := "Heading"
	
	m.updateToc(rawMarkdown, renderedContent)
	if m.state != tocVisible {
		t.Errorf("after updateToc, state should be visible")
	}
	
	m.closeToc()
	if m.state != tocHidden {
		t.Errorf("after closeToc, state = %v, want %v", m.state, tocHidden)
	}
}

func TestTocModel_HeadingLines(t *testing.T) {
	m := newTocModel()

	rawMarkdown := "# H1\n## H2\n### H3"
	// simulated rendered output: h2+ keeps the # prefix, lines carry leading spaces
	renderedContent := "  H1\n\n  ## H2\n\n  ### H3"

	m.updateToc(rawMarkdown, renderedContent)

	if len(m.headings) != 3 {
		t.Fatalf("expected 3 headings, got %d", len(m.headings))
	}

	want := []int{0, 2, 4}
	for i, w := range want {
		if m.lineOf[i] != w {
			t.Errorf("lineOf[%d] = %d, want %d", i, m.lineOf[i], w)
		}
	}

	// after moving the selection, selectedLine returns the matching line
	m.moveSel(1, 3)
	if got := m.selectedLine(); got != 2 {
		t.Errorf("selectedLine() = %d, want 2", got)
	}
}

func TestTocModel_EmptyMarkdown(t *testing.T) {
	m := newTocModel()
	
	rawMarkdown := "Just some text without headings"
	renderedContent := "Just some text without headings"
	
	m.updateToc(rawMarkdown, renderedContent)
	
	if m.state != tocHidden {
		t.Errorf("after updateToc with no headings, state = %v, want %v", m.state, tocHidden)
	}
}

func TestSearchNextPrevMatch_EdgeCases(t *testing.T) {
	sm := NewSearchModel()
	sm.query = "test"
	sm.matches = [][]int{{0, 4}, {5, 9}} // 2 matches
	
	// Test NextMatch from negative index
	sm.currentIdx = -1
	sm.NextMatch()
	if sm.currentIdx != 0 {
		t.Errorf("after NextMatch from -1, currentIdx = %d, want 0", sm.currentIdx)
	}
	
	// Test PrevMatch from negative index  
	sm.currentIdx = -1
	sm.PrevMatch()
	if sm.currentIdx != 1 { // Should wrap to last match
		t.Errorf("after PrevMatch from -1, currentIdx = %d, want 1", sm.currentIdx)
	}
	
	// Test NextMatch wrapping
	sm.currentIdx = 1
	sm.NextMatch()
	if sm.currentIdx != 0 {
		t.Errorf("after NextMatch wrapping, currentIdx = %d, want 0", sm.currentIdx)
	}
	
	// Test PrevMatch wrapping
	sm.currentIdx = 0
	sm.PrevMatch()
	if sm.currentIdx != 1 {
		t.Errorf("after PrevMatch wrapping, currentIdx = %d, want 1", sm.currentIdx)
	}
}

func TestSearchNextPrevMatch_EmptyMatches(t *testing.T) {
	sm := NewSearchModel()
	sm.query = "test"
	sm.matches = [][]int{}
	
	sm.currentIdx = -1
	sm.NextMatch() // Should not panic
	
	if sm.currentIdx != -1 {
		t.Errorf("after NextMatch with no matches, currentIdx should stay -1")
	}
}

func TestTocModel_FromCurrentDocument(t *testing.T) {
	// 模擬按下 t 鍵時，直接從 currentDocument.Body 讀取內容的行為
	m := newTocModel()
	
	// 這模擬 m.currentDocument.Body 的內容
	rawMarkdown := "# H1\n## H2"
	renderedDoc := "H1\n\nH2"  // 可能為空（如果尚未渲染）
	
	// 按下 t 鍵的邏輯：使用 raw 内容，rendered 可能為空
	m.updateToc(rawMarkdown, renderedDoc)
	
	if m.state != tocVisible {
		t.Errorf("after updateToc with valid raw content, state should be visible")
	}
	
	if len(m.headings) != 2 {
		t.Errorf("expected 2 headings, got %d", len(m.headings))
	}
}
