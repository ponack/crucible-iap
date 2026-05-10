// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaseURL string `yaml:"base_url"`
	Token   string `yaml:"token"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "crucible", "config.yaml"), nil
}

func LoadConfig(urlFlag, tokenFlag string) (*Config, error) {
	cfg := &Config{}

	path, err := configPath()
	if err == nil {
		data, err := os.ReadFile(path)
		if err == nil {
			_ = yaml.Unmarshal(data, cfg)
		}
	}

	if v := os.Getenv("CRUCIBLE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("CRUCIBLE_TOKEN"); v != "" {
		cfg.Token = v
	}
	if urlFlag != "" {
		cfg.BaseURL = urlFlag
	}
	if tokenFlag != "" {
		cfg.Token = tokenFlag
	}

	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("no base URL configured — run `crucible configure` or set CRUCIBLE_URL")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("no token configured — run `crucible configure` or set CRUCIBLE_TOKEN")
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
