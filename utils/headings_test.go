package utils

import (
	"testing"
)

func TestExtractHeadings(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		want     []HeadingInfo
	}{
		{
			name: "basic headings",
			markdown: `# Title 1
## Title 2
### Title 3`,
			want: []HeadingInfo{
				{Level: 1, Text: "Title 1"},
				{Level: 2, Text: "Title 2"},
				{Level: 3, Text: "Title 3"},
			},
		},
		{
			name: "headings with text between",
			markdown: `# Title 1
Some paragraph here.
## Title 2
More text.
### Title 3`,
			want: []HeadingInfo{
				{Level: 1, Text: "Title 1"},
				{Level: 2, Text: "Title 2"},
				{Level: 3, Text: "Title 3"},
			},
		},
		{
			name:     "no headings",
			markdown: "Just plain text.\nNo headings here.",
			want:     nil,
		},
		{
			name: "empty string",
			markdown: "",
			want:     nil,
		},
		{
			name: "all heading levels",
			markdown: `# H1
## H2
### H3
#### H4
##### H5
###### H6`,
			want: []HeadingInfo{
				{Level: 1, Text: "H1"},
				{Level: 2, Text: "H2"},
				{Level: 3, Text: "H3"},
				{Level: 4, Text: "H4"},
				{Level: 5, Text: "H5"},
				{Level: 6, Text: "H6"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHeadings(tt.markdown)
			if len(got) != len(tt.want) {
				t.Errorf("ExtractHeadings() got %d headings, want %d. Got: %+v", len(got), len(tt.want), got)
				return
			}
			for i := range got {
				if got[i].Level != tt.want[i].Level || got[i].Text != tt.want[i].Text {
					t.Errorf("Heading[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no ANSI",
			input: "Hello World",
			want:  "Hello World",
		},
		{
			name:  "with ANSI codes",
			input: "\x1b[38;5;39;1m## \x1b[0m\x1b[38;5;39;1mTitle\x1b[0m",
			want:  "## Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.want {
				t.Errorf("stripANSI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindHeadingLineInRenderedContent(t *testing.T) {
	rendered := "\x1b[38;5;228;48;5;63;1m\x1b[0m  \x1b[38;5;228;48;5;63;1mTitle\x1b[0m\x1b[38;5;228;48;5;63;1m 1\x1b[0m\n\x1b[0m  \x1b[38;5;252mSome text\x1b[0m\n\x1b[38;5;39;1m## \x1b[0m\x1b[38;5;39;1mTitle\x1b[0m\x1b[38;5;39;1m 2\x1b[0m"

	tests := []struct {
		name     string
		rendered string
		heading  HeadingInfo
		want     int
	}{
		{
			name:     "find h1 heading",
			rendered: rendered,
			heading:  HeadingInfo{Level: 1, Text: "Title 1"},
			want:     0,
		},
		{
			name:     "find h2 heading",
			rendered: rendered,
			heading:  HeadingInfo{Level: 2, Text: "Title 2"},
			want:     2,
		},
		{
			name:     "non-existent heading",
			rendered: rendered,
			heading:  HeadingInfo{Level: 1, Text: "Not Found"},
			want:     -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindHeadingLineInRenderedContent(tt.rendered, tt.heading)
			if got != tt.want {
				t.Errorf("FindHeadingLineInRenderedContent() = %d, want %d", got, tt.want)
			}
		})
	}
}
