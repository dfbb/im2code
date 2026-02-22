package tmux_test

import (
	"testing"
	"time"

	"github.com/dfbb/im2code/internal/tmux"
)

func TestPromptMatcher(t *testing.T) {
	patterns := []string{`[$#>]\s*$`, `>>>\s*$`}
	matcher := tmux.NewPromptMatcher(patterns)

	cases := []struct {
		line string
		want bool
	}{
		{"user@host:~$ ", true},
		{"root@server:/# ", true},
		{">>> ", true},
		{"some output text", false},
		{"error: something failed", false},
		{"> nested prompt", true},
	}
	for _, c := range cases {
		got := matcher.Match(c.line)
		if got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestANSIActivityDetector(t *testing.T) {
	det := tmux.NewANSIActivityDetector()

	// Feed rapid ANSI sequences (simulating animation)
	for i := 0; i < 20; i++ {
		det.Feed("\x1b[1A\x1b[2K spinner")
	}
	if !det.IsAnimating() {
		t.Error("expected IsAnimating() = true after rapid ANSI input")
	}

	// Wait for animation to be considered stopped
	time.Sleep(200 * time.Millisecond)
	if det.IsAnimating() {
		t.Error("expected IsAnimating() = false after idle period")
	}
}
