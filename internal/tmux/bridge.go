package tmux

import (
	"os/exec"
	"regexp"
	"strings"
)

// ansiEscape matches all ANSI escape sequences including CSI (with private params),
// OSC, and single-char escapes.
var ansiEscape = regexp.MustCompile(`\x1b(?:[@-Z\\-_]|\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))`)

// StripANSI removes all ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// TruncateLines returns the last n lines of s.
func TruncateLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// Bridge wraps tmux CLI commands.
type Bridge struct{}

// New returns a new Bridge.
func New() *Bridge { return &Bridge{} }

// ListSessions returns all active tmux session names.
func (b *Bridge) ListSessions() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil, err
	}
	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// Capture returns the current pane content of session, with ANSI stripped and truncated.
func (b *Bridge) Capture(session string, maxLines int) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", session).Output()
	if err != nil {
		return "", err
	}
	clean := StripANSI(string(out))
	return TruncateLines(clean, maxLines), nil
}

// SendKeys sends text input followed by Enter to the given tmux session.
// The text is sent with -l (literal) so that any \n or \r in the message is
// not misinterpreted by tmux as a key sequence (e.g. \n → M-Enter / Option+Enter
// on macOS). Trailing CR/LF is stripped because Enter is sent explicitly.
func (b *Bridge) SendKeys(session, text string) error {
	text = strings.TrimRight(text, "\r\n")
	if err := exec.Command("tmux", "send-keys", "-t", session, "-l", text).Run(); err != nil {
		return err
	}
	return exec.Command("tmux", "send-keys", "-t", session, "Enter").Run()
}

// SendRawKey sends a tmux key (e.g. "C-c", "C-z") to the session without Enter.
func (b *Bridge) SendRawKey(session, key string) error {
	return exec.Command("tmux", "send-keys", "-t", session, key).Run()
}
