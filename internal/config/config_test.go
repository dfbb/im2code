package config_test

import (
	"os"
	"testing"

	"github.com/dfbb/im2code/internal/config"
)

func TestLoad(t *testing.T) {
	cfg, err := config.Load("../../testdata/config.yaml")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Prefix != "#" {
		t.Errorf("Prefix = %q, want %q", cfg.Prefix, "#")
	}
	if cfg.Tmux.IdleTimeout != "2s" {
		t.Errorf("IdleTimeout = %q, want %q", cfg.Tmux.IdleTimeout, "2s")
	}
	if cfg.Channels.Telegram.Token != "test-token" {
		t.Errorf("Telegram.Token = %q, want %q", cfg.Channels.Telegram.Token, "test-token")
	}
}

func TestLoad_Defaults(t *testing.T) {
	f, _ := os.CreateTemp("", "*.yaml")
	f.WriteString("")
	f.Close()
	defer os.Remove(f.Name())

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Prefix != "#" {
		t.Errorf("default Prefix = %q, want %q", cfg.Prefix, "#")
	}
}
