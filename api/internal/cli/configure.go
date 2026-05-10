// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func NewConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Set Crucible IAP base URL and API token",
		RunE: func(cmd *cobra.Command, args []string) error {
			existing := &Config{}
			if path, err := configPath(); err == nil {
				// load existing so we can show defaults
				if data, err := os.ReadFile(path); err == nil {
					_ = yaml.Unmarshal(data, existing)
				}
			}

			scanner := bufio.NewScanner(os.Stdin)

			fmt.Printf("Base URL [%s]: ", strOr(existing.BaseURL, "https://crucible.example.com"))
			scanner.Scan()
			baseURL := strings.TrimSpace(scanner.Text())
			if baseURL == "" {
				baseURL = existing.BaseURL
			}

			fmt.Printf("API token [%s]: ", maskToken(existing.Token))
			scanner.Scan()
			token := strings.TrimSpace(scanner.Text())
			if token == "" {
				token = existing.Token
			}

			cfg := &Config{BaseURL: strings.TrimRight(baseURL, "/"), Token: token}
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			path, _ := configPath()
			fmt.Printf("Config saved to %s\n", path)
			return nil
		},
	}
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return strings.Repeat("*", len(t))
	}
	return t[:4] + strings.Repeat("*", len(t)-8) + t[len(t)-4:]
}
