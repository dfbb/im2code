package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var rebindCmd = &cobra.Command{
	Use:   "rebind <channel>",
	Short: "Reset channel binding and force re-authentication",
	Args:  cobra.ExactArgs(1),
	RunE:  runRebind,
}

func runRebind(cmd *cobra.Command, args []string) error {
	ch := strings.ToLower(args[0])
	switch ch {
	case "whatsapp":
		return rebindWhatsApp()
	default:
		return fmt.Errorf("rebind not supported for %q (only whatsapp)", ch)
	}
}

func rebindWhatsApp() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cfgPath := configPath()

	// Clear allow_from for whatsapp in config so #im2code can be used again.
	if err := updateConfig(cfgPath, func(raw map[string]any) {
		if channels, ok := raw["channels"].(map[string]any); ok {
			if wa, ok := channels["whatsapp"].(map[string]any); ok {
				delete(wa, "allow_from")
			}
		}
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update config: %v\n", err)
	} else {
		fmt.Println("Cleared whatsapp allow_from in config.")
	}

	// Delete the session database to force QR re-pairing on next start.
	sessionDir := home + "/.im2code/whatsapp"
	dbPath := sessionDir + "/session.db"
	if err := os.Remove(dbPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No session file found (already clean).")
		} else {
			return fmt.Errorf("removing session: %w", err)
		}
	} else {
		fmt.Printf("Deleted %s\n", dbPath)
	}

	fmt.Println("Done. Run 'im2code start --channels whatsapp' to re-pair via QR code,")
	fmt.Println("then send #im2code to activate.")
	return nil
}
