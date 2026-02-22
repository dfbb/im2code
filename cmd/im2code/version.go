package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags="-X main.Version=x.y.z"
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("im2code", Version)
	},
}
