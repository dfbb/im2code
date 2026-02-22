package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
		slog.Info("no config file found, using defaults", "path", configPath())
		cfg = &config.Config{
			Prefix: "#",
			Tmux: config.TmuxConfig{
				IdleTimeout:    "2s",
				MaxOutputLines: 50,
				PromptPatterns: []string{`[$#>]\s*$`, `>>>\s*$`},
			},
		}
	}

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

	subs, err := state.NewSubscriptions(dataDir + "/subscriptions.json")
	if err != nil {
		return fmt.Errorf("loading subscriptions: %w", err)
	}

	idleTimeout, err := time.ParseDuration(cfg.Tmux.IdleTimeout)
	if err != nil {
		idleTimeout = 2 * time.Second
	}

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
		mgr.Register(whatsapp.New(cfg.Channels.WhatsApp.SessionDir, nil, inbound))
	}
	if enabled("feishu") && cfg.Channels.Feishu.AppID != "" {
		mgr.Register(feishu.New(cfg.Channels.Feishu.AppID, cfg.Channels.Feishu.AppSecret, nil, inbound))
	}
	if enabled("dingtalk") && cfg.Channels.DingTalk.ClientID != "" {
		mgr.Register(dingtalk.New(cfg.Channels.DingTalk.ClientID, cfg.Channels.DingTalk.ClientSecret, nil, inbound))
	}
	if enabled("qq") && cfg.Channels.QQ.AppID != "" {
		mgr.Register(qq.New(nil, inbound))
	}

	rtr := router.New(prefix, subs, bridge, outbound)

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
		watchSubscriptions(ctx, subs, bridge, idleTimeout, cfg.Tmux.MaxOutputLines, promptMatcher, outbound)
	}()

	slog.Info("im2code started", "prefix", prefix)
	mgr.Run(ctx)
	wg.Wait()
	slog.Info("im2code stopped")
	return nil
}

// watchSubscriptions periodically checks which subscriptions have watch mode enabled
// and runs idle detectors for them. For now it's a polling skeleton that can be
// expanded once the Router exposes watch state.
func watchSubscriptions(
	ctx context.Context,
	subs *state.Subscriptions,
	bridge *tmux.Bridge,
	timeout time.Duration,
	maxLines int,
	pm *tmux.PromptMatcher,
	outbound chan<- channel.OutboundMessage,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// TODO: cancel all active idle-detector goroutines here.
			return
		case <-ticker.C:
			// TODO: query router watch state and start/stop IdleDetector
			// goroutines per subscription. See internal/tmux/idle.go.
		}
	}
}
