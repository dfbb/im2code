package main

import (
	"fmt"
	"os"

	"github.com/dfbb/im2code/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active subscriptions",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	home, _ := os.UserHomeDir()
	subs, err := state.NewSubscriptions(home + "/.im2code/subscriptions.json")
	if err != nil {
		return err
	}
	all := subs.All()
	if len(all) == 0 {
		fmt.Println("No active subscriptions.")
		return nil
	}
	fmt.Println("Active subscriptions:")
	for k, v := range all {
		fmt.Printf("  %-30s → %s\n", k, v)
	}
	return nil
}
