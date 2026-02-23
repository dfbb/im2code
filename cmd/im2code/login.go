package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var loginCmd = &cobra.Command{
	Use:   "login <channel>",
	Short: "Login / configure an IM channel",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogin,
}

func runLogin(cmd *cobra.Command, args []string) error {
	ch := strings.ToLower(args[0])
	cfgPath := configPath()

	switch ch {
	case "telegram":
		return loginToken(cfgPath, "telegram", "Bot Token")
	case "discord":
		return loginToken(cfgPath, "discord", "Bot Token")
	case "slack":
		return loginSlack(cfgPath)
	case "whatsapp":
		return loginWhatsApp()
	case "feishu":
		return loginFeishu(cfgPath)
	case "dingtalk":
		return loginDingTalk(cfgPath)
	case "qq":
		return loginQQ(cfgPath)
	default:
		return fmt.Errorf("unknown channel: %s\nSupported: telegram, discord, slack, whatsapp, feishu, dingtalk, qq", ch)
	}
}

func loginToken(cfgPath, chName, label string) error {
	fmt.Printf("%s %s: ", chName, label)
	token, err := readSecret()
	if err != nil {
		return err
	}
	return saveConfig(cfgPath, func(raw map[string]any) {
		channels := getOrCreateMap(raw, "channels")
		ch := getOrCreateMap(channels, chName)
		ch["token"] = token
	})
}

func loginSlack(cfgPath string) error {
	fmt.Print("Bot Token (xoxb-...): ")
	botToken, _ := readSecret()
	fmt.Print("App Token (xapp-...): ")
	appToken, _ := readSecret()
	return saveConfig(cfgPath, func(raw map[string]any) {
		channels := getOrCreateMap(raw, "channels")
		ch := getOrCreateMap(channels, "slack")
		ch["bot_token"] = botToken
		ch["app_token"] = appToken
	})
}

func loginFeishu(cfgPath string) error {
	fmt.Print("App ID: ")
	appID, _ := readLine()
	fmt.Print("App Secret: ")
	appSecret, _ := readSecret()
	return saveConfig(cfgPath, func(raw map[string]any) {
		channels := getOrCreateMap(raw, "channels")
		ch := getOrCreateMap(channels, "feishu")
		ch["app_id"] = appID
		ch["app_secret"] = appSecret
	})
}

func loginDingTalk(cfgPath string) error {
	fmt.Print("Client ID: ")
	clientID, _ := readLine()
	fmt.Print("Client Secret: ")
	clientSecret, _ := readSecret()
	return saveConfig(cfgPath, func(raw map[string]any) {
		channels := getOrCreateMap(raw, "channels")
		ch := getOrCreateMap(channels, "dingtalk")
		ch["client_id"] = clientID
		ch["client_secret"] = clientSecret
	})
}

func loginQQ(cfgPath string) error {
	fmt.Print("App ID: ")
	appID, _ := readLine()
	fmt.Print("Secret: ")
	secret, _ := readSecret()
	return saveConfig(cfgPath, func(raw map[string]any) {
		channels := getOrCreateMap(raw, "channels")
		ch := getOrCreateMap(channels, "qq")
		ch["app_id"] = appID
		ch["secret"] = secret
	})
}

func loginWhatsApp() error {
	fmt.Println("WhatsApp login: run the daemon with whatsapp enabled.")
	fmt.Println("A QR code will be printed on first run.")
	fmt.Println("Run: im2code start --channels whatsapp")
	return nil
}

func readSecret() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return readLine()
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return strings.TrimSpace(string(b)), err
}

func readLine() (string, error) {
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	return strings.TrimSpace(s), err
}

func configPath() string {
	if flagConfig != "" {
		return flagConfig
	}
	home, _ := os.UserHomeDir()
	return home + "/.im2code/config.yaml"
}

func saveConfig(path string, fn func(map[string]any)) error {
	if err := updateConfig(path, fn); err != nil {
		return err
	}
	fmt.Printf("Saved to %s\n", path)
	return nil
}

func updateConfig(path string, fn func(map[string]any)) error {
	dir := path[:strings.LastIndex(path, "/")]
	os.MkdirAll(dir, 0700)
	raw := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		yaml.Unmarshal(data, &raw)
	}
	fn(raw)
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func getOrCreateMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if sub, ok := v.(map[string]any); ok {
			return sub
		}
	}
	sub := make(map[string]any)
	m[key] = sub
	return sub
}
