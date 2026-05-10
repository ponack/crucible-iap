// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type stackSummary struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Tool          string     `json:"tool"`
	LastRunStatus string     `json:"last_run_status"`
	LastRunAt     *time.Time `json:"last_run_at"`
	HealthScore   int        `json:"health_score"`
	ProjectID     *string    `json:"project_id"`
	IsLocked      bool       `json:"is_locked"`
}

type stackDetail struct {
	stackSummary
	RepoURL        string `json:"repo_url"`
	RepoBranch     string `json:"repo_branch"`
	ProjectRoot    string `json:"project_root"`
	ToolVersion    string `json:"tool_version"`
	AutoApply      bool   `json:"auto_apply"`
	DriftDetection bool   `json:"drift_detection"`
	Description    string `json:"description"`
}

func NewStacksCmd(urlFlag, tokenFlag *string, jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stacks",
		Short: "Manage stacks",
	}
	cmd.AddCommand(
		newStacksListCmd(urlFlag, tokenFlag, jsonFlag),
		newStacksShowCmd(urlFlag, tokenFlag, jsonFlag),
	)
	return cmd
}

func newStacksListCmd(urlFlag, tokenFlag *string, jsonFlag *bool) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stacks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(*urlFlag, *tokenFlag)
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			path := "/stacks"
			if projectID != "" {
				path += "?project_id=" + projectID
			}

			if *jsonFlag {
				data, err := client.RawGet(path)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			var stacks []stackSummary
			if err := client.Get(path, &stacks); err != nil {
				return err
			}

			w := NewTabWriter(os.Stdout)
			fmt.Fprintln(w, "ID\tNAME\tTOOL\tSTATUS\tHEALTH\tLOCKED")
			for _, s := range stacks {
				locked := ""
				if s.IsLocked {
					locked = "locked"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					shortID(s.ID),
					truncate(s.Name, 30),
					s.Tool,
					strOr(s.LastRunStatus, "—"),
					healthLabel(s.HealthScore),
					locked,
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "filter by project ID")
	return cmd
}

func newStacksShowCmd(urlFlag, tokenFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "show <stack-id>",
		Short: "Show stack details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(*urlFlag, *tokenFlag)
			if err != nil {
				return err
			}
			client := NewClient(cfg)
			path := "/stacks/" + args[0]

			if *jsonFlag {
				data, err := client.RawGet(path)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			var s stackDetail
			if err := client.Get(path, &s); err != nil {
				return err
			}

			w := os.Stdout
			kvLine(w, "ID", s.ID)
			kvLine(w, "Name", s.Name)
			kvLine(w, "Tool", s.Tool+func() string {
				if s.ToolVersion != "" {
					return " " + s.ToolVersion
				}
				return ""
			}())
			kvLine(w, "Repo", s.RepoURL)
			kvLine(w, "Branch", s.RepoBranch)
			if s.ProjectRoot != "" {
				kvLine(w, "Project root", s.ProjectRoot)
			}
			kvLine(w, "Auto-apply", fmt.Sprintf("%v", s.AutoApply))
			kvLine(w, "Drift detection", fmt.Sprintf("%v", s.DriftDetection))
			kvLine(w, "Last run status", strOr(s.LastRunStatus, "—"))
			if s.LastRunAt != nil {
				kvLine(w, "Last run at", s.LastRunAt.Local().Format(time.DateTime))
			}
			kvLine(w, "Health", healthLabel(s.HealthScore))
			if s.IsLocked {
				kvLine(w, "Locked", "yes")
			}
			if s.Description != "" {
				kvLine(w, "Description", s.Description)
			}
			return nil
		},
	}
}
