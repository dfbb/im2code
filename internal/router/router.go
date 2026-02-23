package router

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/dfbb/im2code/internal/channel"
	"github.com/dfbb/im2code/internal/state"
	"github.com/dfbb/im2code/internal/tmux"
)

const helpText = `Available commands:
  {P}list              — list tmux sessions
  {P}attach <session>  — bind this chat to a session
  {P}detach            — remove binding
  {P}status            — show current binding
  {P}snap              — capture and send current pane
  {P}watch on|off      — toggle real-time push
  {P}key <key>         — send control key (e.g. ctrl-c)
  {P}help              — show this message`

// Router dispatches inbound IM messages: prefix-commands → bridge handlers, others → tmux.
type Router struct {
	prefix     string
	subs       *state.Subscriptions
	bridge     *tmux.Bridge
	outbound   chan<- channel.OutboundMessage
	onActivate func(ch, senderID string) // called when a channel is first activated
	watching   map[string]bool
	mu         sync.RWMutex
	activated  map[string]string // channel name → locked senderID
	activeMu   sync.Mutex
}

func New(prefix string, subs *state.Subscriptions, bridge *tmux.Bridge, outbound chan<- channel.OutboundMessage, onActivate func(ch, senderID string)) *Router {
	return &Router{
		prefix:     prefix,
		subs:       subs,
		bridge:     bridge,
		outbound:   outbound,
		onActivate: onActivate,
		watching:   make(map[string]bool),
		activated:  make(map[string]string),
	}
}

func (r *Router) reply(msg channel.InboundMessage, text string) {
	out := channel.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Text:    text,
	}
	select {
	case r.outbound <- out:
	default:
		slog.Warn("router: outbound full, dropping reply", "channel", msg.Channel, "chatID", msg.ChatID)
	}
}

func chatKey(msg channel.InboundMessage) string {
	return msg.Channel + ":" + msg.ChatID
}

// Handle dispatches a message: bridge command or tmux forward.
func (r *Router) Handle(msg channel.InboundMessage) {
	// Gate: channels without a pre-configured allowFrom require the sender to
	// activate with "{prefix}im2code" before any other interaction is accepted.
	if !msg.PreAuthorized {
		activationCmd := r.prefix + "im2code"

		r.activeMu.Lock()
		lockedSender := r.activated[msg.Channel]
		r.activeMu.Unlock()

		if lockedSender == "" {
			// Channel not yet activated.
			if msg.Text == activationCmd {
				r.activeMu.Lock()
				r.activated[msg.Channel] = msg.SenderID
				r.activeMu.Unlock()
				slog.Info("channel activated", "channel", msg.Channel, "senderID", msg.SenderID)
				if r.onActivate != nil {
					go r.onActivate(msg.Channel, msg.SenderID)
				}
				r.reply(msg, fmt.Sprintf("Activated. Send %shelp to see available commands.", r.prefix))
			}
			// Any other message from anyone: ignore silently.
			return
		}

		if lockedSender != msg.SenderID {
			// Wrong sender — ignore silently.
			return
		}
	}

	if strings.HasPrefix(msg.Text, r.prefix) {
		r.handleCommand(msg)
		return
	}

	key := chatKey(msg)
	session, ok := r.subs.Get(key)
	if !ok {
		r.reply(msg, fmt.Sprintf("No session bound. Use %sattach <session> to bind one.\nRun %slist to see available sessions.", r.prefix, r.prefix))
		return
	}

	if r.bridge == nil {
		r.reply(msg, "[tmux bridge not available]")
		return
	}
	if err := r.bridge.SendKeys(session, msg.Text); err != nil {
		r.reply(msg, fmt.Sprintf("Error sending to tmux: %v", err))
	}
}

func (r *Router) handleCommand(msg channel.InboundMessage) {
	text := strings.TrimPrefix(msg.Text, r.prefix)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		r.reply(msg, r.helpText())
		return
	}
	cmd := strings.ToLower(parts[0])
	args := parts[1:]
	key := chatKey(msg)

	switch cmd {
	case "help":
		r.reply(msg, r.helpText())

	case "list":
		if r.bridge == nil {
			r.reply(msg, "[tmux bridge not available]")
			return
		}
		sessions, err := r.bridge.ListSessions()
		if err != nil {
			r.reply(msg, "No tmux sessions found (is tmux running?)")
			return
		}
		r.reply(msg, "Sessions:\n  "+strings.Join(sessions, "\n  "))

	case "attach":
		if len(args) == 0 {
			r.reply(msg, fmt.Sprintf("Usage: %sattach <session>", r.prefix))
			return
		}
		r.subs.Set(key, args[0])
		r.reply(msg, fmt.Sprintf("Attached to session: %s", args[0]))

	case "detach":
		r.subs.Delete(key)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.watching[key] = false
		r.reply(msg, "Detached.")

	case "status":
		session, ok := r.subs.Get(key)
		if !ok {
			r.reply(msg, "Not attached to any session.")
			return
		}
		r.mu.RLock()
		defer r.mu.RUnlock()
		watch := r.watching[key]
		r.reply(msg, fmt.Sprintf("Session: %s\nWatch: %v", session, watch))

	case "snap":
		session, ok := r.subs.Get(key)
		if !ok {
			r.reply(msg, "Not attached to any session.")
			return
		}
		if r.bridge == nil {
			r.reply(msg, "[tmux bridge not available]")
			return
		}
		content, err := r.bridge.Capture(session, 50)
		if err != nil {
			r.reply(msg, fmt.Sprintf("Capture failed: %v", err))
			return
		}
		r.reply(msg, "```\n"+content+"\n```")

	case "watch":
		if len(args) == 0 {
			r.reply(msg, fmt.Sprintf("Usage: %swatch on|off", r.prefix))
			return
		}
		switch strings.ToLower(args[0]) {
		case "on":
			r.mu.Lock()
			defer r.mu.Unlock()
			r.watching[key] = true
			r.reply(msg, "Watch mode enabled.")
		case "off":
			r.mu.Lock()
			defer r.mu.Unlock()
			r.watching[key] = false
			r.reply(msg, "Watch mode disabled.")
		default:
			r.reply(msg, fmt.Sprintf("Usage: %swatch on|off", r.prefix))
		}

	case "key":
		if len(args) == 0 {
			r.reply(msg, fmt.Sprintf("Usage: %skey <key> (e.g. ctrl-c)", r.prefix))
			return
		}
		session, ok := r.subs.Get(key)
		if !ok {
			r.reply(msg, "Not attached to any session.")
			return
		}
		if r.bridge == nil {
			r.reply(msg, "[tmux bridge not available]")
			return
		}
		if err := r.bridge.SendRawKey(session, toTmuxKey(args[0])); err != nil {
			r.reply(msg, fmt.Sprintf("Error: %v", err))
		}

	default:
		r.reply(msg, fmt.Sprintf("Unknown command: %s%s\nRun %shelp for available commands.", r.prefix, cmd, r.prefix))
	}
}

func (r *Router) helpText() string {
	return strings.ReplaceAll(helpText, "{P}", r.prefix)
}

// WatchedChats returns a snapshot of {chatKey: session} for all currently watched chats.
// Called by watchSubscriptions to manage idle detectors.
func (r *Router) WatchedChats() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for key, watching := range r.watching {
		if watching {
			if session, ok := r.subs.Get(key); ok {
				result[key] = session
			}
		}
	}
	return result
}

// keyAliases maps short/common names to the tmux key names expected by send-keys.
var keyAliases = map[string]string{
	"esc":       "Escape",
	"escape":    "Escape",
	"bs":        "BSpace",
	"backspace": "BSpace",
	"del":       "Delete",
	"delete":    "Delete",
	"ins":       "Insert",
	"insert":    "Insert",
	"pgup":      "PPage",
	"pageup":    "PPage",
	"pgdn":      "NPage",
	"pagedown":  "NPage",
	"home":      "Home",
	"end":       "End",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"space":     "Space",
}

func toTmuxKey(key string) string {
	// Normalize separator: ctrl+x and ctrl-x are equivalent.
	normalized := strings.ReplaceAll(strings.ToLower(key), "+", "-")
	if strings.HasPrefix(normalized, "ctrl-") {
		return "C-" + normalized[5:]
	}
	if strings.HasPrefix(normalized, "alt-") {
		return "M-" + normalized[4:]
	}
	if alias, ok := keyAliases[normalized]; ok {
		return alias
	}
	return key // pass through as-is: Enter, Tab, Escape, F1, etc.
}
