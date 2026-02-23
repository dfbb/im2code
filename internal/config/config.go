package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Prefix       string         `yaml:"prefix"`
	LogLevel     string         `yaml:"loglevel"`
	LogFile      string         `yaml:"logfile"`
	CmdHistoryDB string         `yaml:"cmd_history_db"`
	Tmux         TmuxConfig     `yaml:"tmux"`
	Channels     ChannelConfigs `yaml:"channels"`
}

type TmuxConfig struct {
	IdleTimeout    string   `yaml:"idle_timeout"`
	MaxOutputLines int      `yaml:"max_output_lines"`
	PromptPatterns []string `yaml:"prompt_patterns"`
}

type ChannelConfigs struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Discord  DiscordConfig  `yaml:"discord"`
	Slack    SlackConfig    `yaml:"slack"`
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`
	Feishu   FeishuConfig   `yaml:"feishu"`
	DingTalk DingTalkConfig `yaml:"dingtalk"`
	QQ       QQConfig       `yaml:"qq"`
}

type TelegramConfig struct {
	Token     string   `yaml:"token"`
	AllowFrom []string `yaml:"allow_from"`
}

type DiscordConfig struct {
	Token     string   `yaml:"token"`
	AllowFrom []string `yaml:"allow_from"`
}

type SlackConfig struct {
	BotToken  string   `yaml:"bot_token"`
	AppToken  string   `yaml:"app_token"`
	AllowFrom []string `yaml:"allow_from"`
}

type WhatsAppConfig struct {
	SessionDir string   `yaml:"session_dir"`
	AllowFrom  []string `yaml:"allow_from"`
}

type FeishuConfig struct {
	AppID     string `yaml:"app_id"`
	AppSecret string `yaml:"app_secret"`
}

type DingTalkConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type QQConfig struct {
	AppID     string   `yaml:"app_id"`
	Secret    string   `yaml:"secret"`
	AllowFrom []string `yaml:"allow_from"`
}

func defaults() *Config {
	return &Config{
		Prefix:   "#",
		LogLevel: "warn",
		LogFile:  "./im2code.log",
		Tmux: TmuxConfig{
			IdleTimeout:    "2s",
			MaxOutputLines: 50,
			PromptPatterns: []string{`[$#>]\s*$`, `>>>\s*$`},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
