package tmux

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PromptMatcher detects shell prompt lines using configurable patterns.
type PromptMatcher struct {
	patterns    []*regexp.Regexp
	startPats   []*regexp.Regexp
}

func NewPromptMatcher(patterns []string) *PromptMatcher {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	startPats := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if r, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, r)
		}
		// Also compile a line-start variant: strips trailing $ and wraps with ^
		// to catch prompts like "> command" where > is at start of line.
		if r, err := regexp.Compile("^" + strings.TrimRight(p, "$") + `\s`); err == nil {
			startPats = append(startPats, r)
		}
	}
	return &PromptMatcher{patterns: compiled, startPats: startPats}
}

// Match returns true if line matches any prompt pattern (end-of-line or start-of-line).
func (m *PromptMatcher) Match(line string) bool {
	for _, r := range m.patterns {
		if r.MatchString(line) {
			return true
		}
	}
	for _, r := range m.startPats {
		if r.MatchString(line) {
			return true
		}
	}
	return false
}

// ANSIActivityDetector tracks ANSI escape sequence frequency to detect animations.
type ANSIActivityDetector struct {
	mu           sync.Mutex
	lastActivity time.Time
	threshold    time.Duration
}

func NewANSIActivityDetector() *ANSIActivityDetector {
	return &ANSIActivityDetector{threshold: 150 * time.Millisecond}
}

// Feed records that ANSI escape sequences were seen in data.
func (d *ANSIActivityDetector) Feed(data string) {
	if ansiEscape.MatchString(data) {
		d.mu.Lock()
		d.lastActivity = time.Now()
		d.mu.Unlock()
	}
}

// IsAnimating returns true if ANSI sequences were seen recently.
func (d *ANSIActivityDetector) IsAnimating() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.lastActivity.IsZero() && time.Since(d.lastActivity) < d.threshold
}

// IdleDetector monitors a tmux session and calls onIdle when output settles.
// Triple-trigger: ANSI animation stop + prompt detection + timeout fallback.
type IdleDetector struct {
	bridge        *Bridge
	session       string
	timeout       time.Duration
	maxLines      int
	promptMatcher *PromptMatcher
	lastContent   string
	onIdle        func(content string)
}

func NewIdleDetector(bridge *Bridge, session string, timeout time.Duration, maxLines int, promptMatcher *PromptMatcher, onIdle func(string)) *IdleDetector {
	return &IdleDetector{
		bridge:        bridge,
		session:       session,
		timeout:       timeout,
		maxLines:      maxLines,
		promptMatcher: promptMatcher,
		onIdle:        onIdle,
	}
}

// watchInterval is how often output is pushed when there is new content but no
// command has settled (e.g. a long-running background process).
const watchInterval = 10 * time.Second

// Run polls the tmux pane and calls onIdle when output settles. Blocks until ctx done.
//
// Two push paths:
//  1. Immediate: when a shell prompt is detected (command finished), push right away.
//  2. Periodic: every watchInterval, push if content changed since the last push.
func (d *IdleDetector) Run(ctx context.Context) {
	pollTicker := time.NewTicker(100 * time.Millisecond)
	defer pollTicker.Stop()
	periodicTicker := time.NewTicker(watchInterval)
	defer periodicTicker.Stop()

	var lastChange time.Time
	var lastPushed string
	var lastFired time.Time // cooldown: prevents rapid re-triggers on minor content changes

	push := func(content string) {
		lastFired = time.Now()
		lastPushed = content
		d.onIdle(content)
	}

	for {
		select {
		case <-ctx.Done():
			return

		case <-periodicTicker.C:
			// Push current snapshot if it changed since the last push.
			content, err := d.bridge.Capture(d.session, d.maxLines)
			if err != nil {
				continue
			}
			if content != lastPushed {
				push(content)
			}

		case <-pollTicker.C:
			content, err := d.bridge.Capture(d.session, d.maxLines)
			if err != nil {
				continue
			}

			if content != d.lastContent {
				d.lastContent = content
				lastChange = time.Now()
				continue
			}

			// Content is stable. Skip if nothing changed since last push,
			// or if we are still within the cooldown window after the last push.
			if content == lastPushed || lastChange.IsZero() {
				continue
			}
			if !lastFired.IsZero() && time.Since(lastFired) < d.timeout {
				continue
			}

			idle := time.Since(lastChange) > d.timeout
			promptFound := d.promptMatcher.Match(content)

			if idle || promptFound {
				push(content)
			}
		}
	}
}
