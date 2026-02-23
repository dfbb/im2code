package main

import (
	"fmt"

	"github.com/dfbb/im2code/internal/channel/discord"
	slackch "github.com/dfbb/im2code/internal/channel/slack"
	"github.com/dfbb/im2code/internal/channel/telegram"
	"github.com/dfbb/im2code/internal/config"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check [channel]",
	Short: "Check channel connectivity",
	RunE:  runCheck,
}

func runCheck(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath())
	if err != nil {
		// Config may not exist yet — treat as empty config
		cfg = &config.Config{}
	}

	type entry struct {
		name    string
		enabled func() bool
		check   func() (string, error)
	}

	entries := []entry{
		{"telegram", func() bool { return cfg.Channels.Telegram.Token != "" },
			func() (string, error) { return checkTelegramToken(cfg.Channels.Telegram.Token) }},
		{"discord", func() bool { return cfg.Channels.Discord.Token != "" },
			func() (string, error) { return checkDiscordToken(cfg.Channels.Discord.Token) }},
		{"slack", func() bool { return cfg.Channels.Slack.BotToken != "" },
			func() (string, error) { return checkSlackToken(cfg.Channels.Slack.BotToken) }},
		{"whatsapp", func() bool { return true },
			func() (string, error) { return "session-based (run start to check)", nil }},
		{"feishu", func() bool { return cfg.Channels.Feishu.AppID != "" },
			func() (string, error) { return "configured", nil }},
		{"dingtalk", func() bool { return cfg.Channels.DingTalk.ClientID != "" },
			func() (string, error) { return "configured", nil }},
		{"qq", func() bool { return cfg.Channels.QQ.AppID != "" && cfg.Channels.QQ.Secret != "" },
			func() (string, error) { return "configured (secret set)", nil }},
	}

	filter := ""
	if len(args) > 0 {
		filter = args[0]
	}

	ok, failed, skipped := 0, 0, 0
	for _, e := range entries {
		if filter != "" && e.name != filter {
			continue
		}
		if !e.enabled() {
			fmt.Printf("  - %-12s skipped    (not configured)\n", e.name)
			skipped++
			continue
		}
		detail, err := e.check()
		if err != nil {
			fmt.Printf("  ✗ %-12s failed     (%v)\n", e.name, err)
			failed++
		} else {
			fmt.Printf("  ✓ %-12s ok         (%s)\n", e.name, detail)
			ok++
		}
	}
	fmt.Printf("\n%d ok, %d failed, %d skipped\n", ok, failed, skipped)
	return nil
}

func checkTelegramToken(token string) (string, error) {
	return telegram.CheckToken(token)
}

func checkDiscordToken(token string) (string, error) {
	return discord.CheckToken(token)
}

func checkSlackToken(token string) (string, error) {
	return slackch.CheckToken(token)
}
