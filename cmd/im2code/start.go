package main

import (
	"fmt"

	"github.com/spf13/cobra"
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
	fmt.Println("Starting im2code... (not yet fully wired)")
	return nil
}
