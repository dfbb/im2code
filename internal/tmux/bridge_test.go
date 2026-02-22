package tmux_test

import (
	"strings"
	"testing"

	"github.com/dfbb/im2code/internal/tmux"
)

func TestStripANSI(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"\x1b[32mhello\x1b[0m", "hello"},
		{"\x1b[?25l spinner \x1b[?25h", " spinner "},
		{"\x1b[1A\x1b[2K", ""},
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		got := tmux.StripANSI(c.input)
		if got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestTruncateLines(t *testing.T) {
	input := strings.Repeat("line\n", 100)
	got := tmux.TruncateLines(input, 50)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) > 50 {
		t.Errorf("TruncateLines returned %d lines, want <= 50", len(lines))
	}
}
