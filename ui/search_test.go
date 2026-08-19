package ui

import (
	"testing"
)

func TestSearchModel_StartAndStop(t *testing.T) {
	sm := NewSearchModel()
	if sm.state != searchHidden {
		t.Errorf("initial state = %d, want %d", sm.state, searchHidden)
	}

	content := "Hello World\nhello world\nHELLO WORLD"
	sm.StartSearch(content)
	if sm.state != searchActive {
		t.Errorf("after StartSearch, state = %d, want %d", sm.state, searchActive)
	}
	if sm.query != "" {
		t.Errorf("query = %q, want empty", sm.query)
	}

	sm.StopSearch()
	if sm.state != searchHidden {
		t.Errorf("after StopSearch, state = %d, want %d", sm.state, searchHidden)
	}
}

func TestSearchModel_FindMatches(t *testing.T) {
	content := "Hello World\nhello world\nHELLO WORLD"

	tests := []struct {
		name          string
		query         string
		caseSensitive bool
		wantCount     int
	}{
		{
			name:          "case sensitive exact match",
			query:         "Hello",
			caseSensitive: true,
			wantCount:     1,
		},
		{
			name:          "case insensitive finds all",
			query:         "hello",
			caseSensitive: false,
			wantCount:     3,
		},
		{
			name:          "empty query no matches",
			query:         "",
			caseSensitive: true,
			wantCount:     0,
		},
		{
			name:          "non-existent string",
			query:         "xyz",
			caseSensitive: true,
			wantCount:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSearchModel()
			sm.StartSearch(content)
			sm.findMatches(sm.rawContent, tt.query, tt.caseSensitive)
			if len(sm.matches) != tt.wantCount {
				t.Errorf("findMatches() got %d matches, want %d", len(sm.matches), tt.wantCount)
			}
		})
	}
}

func TestSearchModel_NextPrevMatch(t *testing.T) {
	content := "apple banana apple cherry apple"
	sm := NewSearchModel()
	sm.StartSearch(content)
	sm.findMatches(sm.rawContent, "apple", true)

	if len(sm.matches) != 3 {
		t.Fatalf("expected 3 matches for 'apple', got %d", len(sm.matches))
	}

	// NextMatch should cycle through matches
	sm.NextMatch()
	if sm.currentIdx != 0 {
		t.Errorf("after first NextMatch, currentIdx = %d, want 0", sm.currentIdx)
	}

	sm.NextMatch()
	if sm.currentIdx != 1 {
		t.Errorf("after second NextMatch, currentIdx = %d, want 1", sm.currentIdx)
	}

	sm.PrevMatch()
	if sm.currentIdx != 0 {
		t.Errorf("after PrevMatch, currentIdx = %d, want 0", sm.currentIdx)
	}

	// Test wrapping: prev from 0 should go to last
	sm.PrevMatch()
	if sm.currentIdx != 2 {
		t.Errorf("after PrevMatch wrapping, currentIdx = %d, want 2", sm.currentIdx)
	}
}

func TestHighlightMatchesANSI(t *testing.T) {
	content := "\x1b[38;5;252mHello\x1b[0m World"
	matches := [][]int{{0, 5}} // "Hello" in plain text

	result := highlightMatchesANSI(content, matches, 0)
	if result == content {
		t.Error("highlightMatchesANSI() should add highlight sequences")
	}
}

func TestSearchModel_GetMatchInfo(t *testing.T) {
	sm := NewSearchModel()
	sm.query = "test"

	// No matches yet
	info := sm.GetMatchInfo()
	if info != "無匹配結果" {
		t.Errorf("GetMatchInfo() with no matches = %q, want %q", info, "無匹配結果")
	}

	sm.matches = [][]int{{0, 4}, {10, 14}}
	sm.currentIdx = 0
	info = sm.GetMatchInfo()
	if info != "1/2" {
		t.Errorf("GetMatchInfo() = %q, want %q", info, "1/2")
	}

	// Empty query
	sm.query = ""
	sm.matches = nil
	info = sm.GetMatchInfo()
	if info != "" {
		t.Errorf("GetMatchInfo() with empty query = %q, want empty", info)
	}
}

func TestFindNextMatchIndex(t *testing.T) {
	content := "the cat and the dog"

	idx := findNextMatchIndex(content, "the", 0, true)
	if idx != 0 {
		t.Errorf("findNextMatchIndex() first 'the' = %d, want 0", idx)
	}

	idx = findNextMatchIndex(content, "the", 1, true)
	if idx != 12 {
		t.Errorf("findNextMatchIndex() second 'the' = %d, want 12", idx)
	}

	idx = findNextMatchIndex(content, "the", 13, true)
	if idx != -1 {
		t.Errorf("findNextMatchIndex() after last 'the' = %d, want -1", idx)
	}
}
