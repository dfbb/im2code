package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dfbb/im2code/internal/channel"
	"github.com/dfbb/im2code/internal/channel/dingtalk"
	"github.com/dfbb/im2code/internal/channel/discord"
	"github.com/dfbb/im2code/internal/channel/feishu"
	"github.com/dfbb/im2code/internal/channel/qq"
	"github.com/dfbb/im2code/internal/channel/slack"
	"github.com/dfbb/im2code/internal/channel/telegram"
	"github.com/dfbb/im2code/internal/channel/whatsapp"
	"github.com/dfbb/im2code/internal/config"
	"github.com/dfbb/im2code/internal/history"
	"github.com/dfbb/im2code/internal/router"
	"github.com/dfbb/im2code/internal/state"
	"github.com/dfbb/im2code/internal/tmux"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the im2code daemon",
	RunE:  runStart,
}

var (
	flagConfig   string
	flagPrefix   string
	flagChannels []string
)

func init() {
	startCmd.Flags().StringVar(&flagConfig, "config", "", "config file (default: ~/.im2code/config.yaml)")
	startCmd.Flags().StringVar(&flagPrefix, "prefix", "", "bridge command prefix (overrides config)")
	startCmd.Flags().StringSliceVar(&flagChannels, "channels", nil, "channels to enable (e.g. telegram,slack)")
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath())
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg = config.Defaults()
	}

	if err := setupLogging(cfg.LogLevel, cfg.LogFile); err != nil {
		return fmt.Errorf("setting up logging: %w", err)
	}
	slog.Info("im2code starting", "loglevel", cfg.LogLevel, "logfile", cfg.LogFile)

	prefix := cfg.Prefix
	if flagPrefix != "" {
		prefix = flagPrefix
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determining home directory: %w", err)
	}
	dataDir := home + "/.im2code"
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	// Write back the merged config so any fields that were absent (or the file
	// itself if it did not exist) are initialised with their default values.
	if err := config.Save(configPath(), cfg); err != nil {
		slog.Warn("could not persist config defaults", "err", err)
	}

	subs, err := state.NewSubscriptions(dataDir + "/subscriptions.json")
	if err != nil {
		return fmt.Errorf("loading subscriptions: %w", err)
	}

	idleTimeout, err := time.ParseDuration(cfg.Tmux.IdleTimeout)
	if err != nil {
		idleTimeout = 2 * time.Second
	}

	watchTimeMin := parseClamped(cfg.Tmux.WatchTimeMin, 5*time.Second, time.Second, 30*time.Second)
	watchTimeMax := parseClamped(cfg.Tmux.WatchTimeMax, 20*time.Second, 5*time.Second, 3600*time.Second)

	bridge := tmux.New()
	promptMatcher := tmux.NewPromptMatcher(cfg.Tmux.PromptPatterns)
	inbound := make(chan channel.InboundMessage, 64)
	outbound := make(chan channel.OutboundMessage, 64)

	mgr := channel.NewManager(inbound, outbound)

	enabled := func(name string) bool {
		if len(flagChannels) == 0 {
			return true
		}
		for _, c := range flagChannels {
			if c == name {
				return true
			}
		}
		return false
	}

	if enabled("telegram") && cfg.Channels.Telegram.Token != "" {
		mgr.Register(telegram.New(cfg.Channels.Telegram.Token, cfg.Channels.Telegram.AllowFrom, inbound))
	}
	if enabled("discord") && cfg.Channels.Discord.Token != "" {
		mgr.Register(discord.New(cfg.Channels.Discord.Token, cfg.Channels.Discord.AllowFrom, inbound))
	}
	if enabled("slack") && cfg.Channels.Slack.BotToken != "" {
		mgr.Register(slack.New(cfg.Channels.Slack.BotToken, cfg.Channels.Slack.AppToken, cfg.Channels.Slack.AllowFrom, inbound))
	}
	// WhatsApp has no token-based credential: its first-run flow presents a QR
	// code on stderr for pairing. Always include it when the channel is enabled.
	if enabled("whatsapp") {
		mgr.Register(whatsapp.New(cfg.Channels.WhatsApp.SessionDir, cfg.Channels.WhatsApp.AllowFrom, cfg.LogLevel, inbound))
	}
	if enabled("feishu") && cfg.Channels.Feishu.AppID != "" {
		mgr.Register(feishu.New(cfg.Channels.Feishu.AppID, cfg.Channels.Feishu.AppSecret, nil, inbound))
	}
	if enabled("dingtalk") && cfg.Channels.DingTalk.ClientID != "" {
		mgr.Register(dingtalk.New(cfg.Channels.DingTalk.ClientID, cfg.Channels.DingTalk.ClientSecret, nil, inbound))
	}
	if enabled("qq") && cfg.Channels.QQ.AppID != "" && cfg.Channels.QQ.Secret != "" {
		mgr.Register(qq.New(cfg.Channels.QQ.AppID, cfg.Channels.QQ.Secret, cfg.Channels.QQ.AllowFrom, inbound))
	}

	cfgFile := configPath()
	onActivate := func(ch, senderID string) {
		err := updateConfig(cfgFile, func(raw map[string]any) {
			chanMap := getOrCreateMap(getOrCreateMap(raw, "channels"), ch)
			existing, _ := chanMap["allow_from"].([]any)
			chanMap["allow_from"] = append(existing, senderID)
		})
		if err != nil {
			slog.Error("failed to persist activated user to config", "channel", ch, "err", err)
		} else {
			slog.Info("activated user saved to config", "channel", ch, "senderID", senderID)
		}
	}

	histDBPath := cfg.CmdHistoryDB
	if histDBPath == "" {
		histDBPath = dataDir + "/cmd_history.db"
	}
	hist, err := history.New(histDBPath)
	if err != nil {
		return fmt.Errorf("opening command history db: %w", err)
	}
	defer hist.Close()

	rtr := router.New(prefix, subs, bridge, outbound, onActivate, hist)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				// Drain remaining buffered messages before exiting.
				for {
					select {
					case msg := <-inbound:
						rtr.Handle(msg)
					default:
						return
					}
				}
			case msg := <-inbound:
				rtr.Handle(msg)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		watchSubscriptions(ctx, rtr, bridge, idleTimeout, watchTimeMin, watchTimeMax, cfg.Tmux.MaxOutputLines, promptMatcher, outbound)
	}()

	slog.Info("im2code started", "prefix", prefix)
	mgr.Run(ctx)
	wg.Wait()
	slog.Info("im2code stopped")
	return nil
}

// watchSubscriptions periodically checks which subscriptions have watch mode enabled
// and runs idle detectors for them.
func watchSubscriptions(
	ctx context.Context,
	rtr *router.Router,
	bridge *tmux.Bridge,
	timeout time.Duration,
	watchMin time.Duration,
	watchMax time.Duration,
	maxLines int,
	pm *tmux.PromptMatcher,
	outbound chan<- channel.OutboundMessage,
) {
	// maps session name → cancel func for its running IdleDetector
	active := make(map[string]context.CancelFunc)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			for _, cancel := range active {
				cancel()
			}
			return
		case <-ticker.C:
			// Get current snapshot: chatKey → session for all watched chats
			watched := rtr.WatchedChats() // map[chatKey]session

			// Build set of sessions currently needed
			needed := make(map[string]bool)
			for _, session := range watched {
				needed[session] = true
			}

			// Stop detectors for sessions no longer watched
			for session, cancel := range active {
				if !needed[session] {
					cancel()
					delete(active, session)
				}
			}

			// Start detectors for newly watched sessions
			for _, session := range watched {
				if _, ok := active[session]; ok {
					continue // already running
				}
				s := session // capture for closure
				detCtx, cancel := context.WithCancel(ctx)
				active[s] = cancel

				onIdle := func(content string) {
					// Re-query current watchers each time so new subscribers
					// get output without restarting the detector.
					current := rtr.WatchedChats()
					for chatKey, sess := range current {
						if sess != s {
							continue
						}
						parts := strings.SplitN(chatKey, ":", 2)
						if len(parts) != 2 {
							continue
						}
						msg := channel.OutboundMessage{
							Channel: parts[0],
							ChatID:  parts[1],
							Text:    "```\n" + content + "\n```",
						}
						select {
						case outbound <- msg:
						default:
							slog.Warn("watchSubscriptions: outbound full, dropping capture",
								"session", s)
						}
					}
				}

				det := tmux.NewIdleDetector(bridge, s, watchMin, watchMax, maxLines, pm, onIdle)
				go det.Run(detCtx)
				slog.Info("watch: started idle detector", "session", s)
			}
		}
	}
}

// setupLogging configures the default slog handler to write to logFile at the
// given level. Relative paths are resolved relative to the executable's directory.
func setupLogging(level, logFile string) error {
	logPath := logFile
	if !filepath.IsAbs(logPath) {
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving executable path: %w", err)
		}
		logPath = filepath.Join(filepath.Dir(execPath), filepath.Base(logFile))
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", logPath, err)
	}

	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: lvl})))
	return nil
}

// parseClamped parses a duration string and clamps it to [min, max].
// Falls back to def if the string is empty or unparseable.
func parseClamped(s string, def, min, max time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d == 0 {
		d = def
	}
	if d < min {
		d = min
	}
	if d > max {
		d = max
	}
	return d
}
