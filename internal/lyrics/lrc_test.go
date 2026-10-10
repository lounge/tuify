package lyrics

import (
	"slices"
	"testing"
)

func TestParseLRC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []Line
	}{
		{
			name:  "two-digit fraction",
			input: "[00:12.34] Hello",
			want:  []Line{{StartMs: 12340, Text: "Hello"}},
		},
		{
			name:  "three-digit fraction and no space",
			input: "[00:12.345]Hello",
			want:  []Line{{StartMs: 12345, Text: "Hello"}},
		},
		{
			name:  "one-digit fraction",
			input: "[00:12.3] Hello",
			want:  []Line{{StartMs: 12300, Text: "Hello"}},
		},
		{
			name:  "no fraction",
			input: "[01:02] Hello",
			want:  []Line{{StartMs: 62000, Text: "Hello"}},
		},
		{
			name:  "colon as fraction separator",
			input: "[00:01:50] Hello",
			want:  []Line{{StartMs: 1500, Text: "Hello"}},
		},
		{
			name:  "three-digit minutes",
			input: "[100:00.00] Late",
			want:  []Line{{StartMs: 6000000, Text: "Late"}},
		},
		{
			name:  "several stamps expand to one line each, sorted",
			input: "[01:00.00][00:10.00] Chorus",
			want:  []Line{{StartMs: 10000, Text: "Chorus"}, {StartMs: 60000, Text: "Chorus"}},
		},
		{
			name:  "unsorted input is sorted",
			input: "[00:20.00] b\n[00:10.00] a",
			want:  []Line{{StartMs: 10000, Text: "a"}, {StartMs: 20000, Text: "b"}},
		},
		{
			name:  "equal stamps keep file order",
			input: "[00:10.00] a\n[00:10.00] b",
			want:  []Line{{StartMs: 10000, Text: "a"}, {StartMs: 10000, Text: "b"}},
		},
		{
			name:  "stamped empty line is kept as a break",
			input: "[00:10.00] a\n[00:15.00]\n[00:20.00] b",
			want:  []Line{{StartMs: 10000, Text: "a"}, {StartMs: 15000, Text: ""}, {StartMs: 20000, Text: "b"}},
		},
		{
			name:  "metadata tags are dropped",
			input: "[ar:Queen]\n[ti:Song]\n[offset:+500]\n[00:01.00] a",
			want:  []Line{{StartMs: 1000, Text: "a"}},
		},
		{
			name:  "untimed lines are dropped",
			input: "intro text\n[00:01.00] a\n\ngarbage",
			want:  []Line{{StartMs: 1000, Text: "a"}},
		},
		{
			name:  "BOM and CRLF",
			input: "\xef\xbb\xbf[00:01.00] a\r\n[00:02.00] b\r\n",
			want:  []Line{{StartMs: 1000, Text: "a"}, {StartMs: 2000, Text: "b"}},
		},
		{
			name:  "enhanced LRC word stamps are stripped",
			input: "[00:01.00] <00:01.00>Hello <00:01.50>world",
			want:  []Line{{StartMs: 1000, Text: "Hello world"}},
		},
		{
			name:  "control characters are removed",
			input: "[00:01.00] a\x1b[2Jb\x07",
			want:  []Line{{StartMs: 1000, Text: "a[2Jb"}},
		},
		{
			name:  "control character inside a word stamp does not hide it",
			input: "[0:00]<\x0e0:00>",
			want:  []Line{{StartMs: 0, Text: ""}},
		},
		{
			name:  "seconds past 59 are not a stamp",
			input: "[00:60.00] a",
			want:  nil,
		},
		{
			name:  "stamp in the middle of a line is text",
			input: "la [00:01.00] la",
			want:  nil,
		},
		{
			name:  "space between stamps ends the chain",
			input: "[00:01.00] [00:02.00] x",
			want:  []Line{{StartMs: 1000, Text: "[00:02.00] x"}},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseLRC(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("parseLRC(%q)\n got %v\nwant %v", tt.input, got, tt.want)
			}
		})
	}
}
