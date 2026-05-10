// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package main

import (
	"fmt"
	"os"

	"github.com/ponack/crucible-iap/internal/cli"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var urlFlag, tokenFlag string
	var jsonFlag, quietFlag bool

	root := &cobra.Command{
		Use:          "crucible",
		Short:        "Crucible IAP CLI — trigger runs, check status, approve or discard from your terminal",
		Version:      version,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&urlFlag, "url", "", "Crucible base URL (overrides config and CRUCIBLE_URL)")
	root.PersistentFlags().StringVar(&tokenFlag, "token", "", "API token (overrides config and CRUCIBLE_TOKEN)")
	root.PersistentFlags().BoolVar(&jsonFlag, "json", false, "output raw JSON")
	root.PersistentFlags().BoolVarP(&quietFlag, "quiet", "q", false, "print only IDs (for scripting)")

	root.AddCommand(
		cli.NewConfigureCmd(),
		cli.NewStacksCmd(&urlFlag, &tokenFlag, &jsonFlag),
		cli.NewRunsCmd(&urlFlag, &tokenFlag, &jsonFlag, &quietFlag),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
