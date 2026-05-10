// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type runSummary struct {
	ID           string     `json:"id"`
	StackID      string     `json:"stack_id"`
	StackName    string     `json:"stack_name"`
	Status       string     `json:"status"`
	Type         string     `json:"type"`
	Trigger      string     `json:"trigger"`
	PlanAdd      *int       `json:"plan_add"`
	PlanChange   *int       `json:"plan_change"`
	PlanDestroy  *int       `json:"plan_destroy"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

var terminalStatuses = map[string]bool{
	"finished": true, "failed": true, "discarded": true,
	"cancelled": true, "error": true,
}

func NewRunsCmd(urlFlag, tokenFlag *string, jsonFlag, quietFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Manage runs",
	}
	cmd.AddCommand(
		newRunsListCmd(urlFlag, tokenFlag, jsonFlag),
		newRunsTriggerCmd(urlFlag, tokenFlag, jsonFlag, quietFlag),
		newRunsApproveCmd(urlFlag, tokenFlag),
		newRunsConfirmCmd(urlFlag, tokenFlag),
		newRunsDiscardCmd(urlFlag, tokenFlag),
		newRunsStatusCmd(urlFlag, tokenFlag, jsonFlag),
	)
	return cmd
}

func newRunsListCmd(urlFlag, tokenFlag *string, jsonFlag *bool) *cobra.Command {
	var stackID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(*urlFlag, *tokenFlag)
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			var path string
			if stackID != "" {
				path = "/stacks/" + stackID + "/runs?limit=20"
			} else {
				path = "/runs?limit=20"
			}

			if *jsonFlag {
				data, err := client.RawGet(path)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			var runs []runSummary
			if err := client.Get(path, &runs); err != nil {
				return err
			}

			w := NewTabWriter(os.Stdout)
			fmt.Fprintln(w, "ID\tSTACK\tTYPE\tSTATUS\tPLAN\tQUEUED")
			for _, r := range runs {
				stack := shortID(r.StackID)
				if r.StackName != "" {
					stack = truncate(r.StackName, 20)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					shortID(r.ID),
					stack,
					r.Type,
					r.Status,
					strOr(planSummary(r.PlanAdd, r.PlanChange, r.PlanDestroy), "—"),
					r.QueuedAt.Local().Format("2006-01-02 15:04"),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&stackID, "stack", "", "filter by stack ID")
	return cmd
}

func newRunsTriggerCmd(urlFlag, tokenFlag *string, jsonFlag, quietFlag *bool) *cobra.Command {
	var runType string
	cmd := &cobra.Command{
		Use:   "trigger <stack-id>",
		Short: "Trigger a new run on a stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(*urlFlag, *tokenFlag)
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			body := map[string]string{"type": runType}
			if *jsonFlag {
				data, err := client.RawPost("/stacks/"+args[0]+"/runs", body)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			var run runSummary
			if err := client.Post("/stacks/"+args[0]+"/runs", body, &run); err != nil {
				return err
			}
			if *quietFlag {
				fmt.Println(run.ID)
				return nil
			}
			fmt.Printf("Run triggered: %s (status: %s)\n", run.ID, run.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&runType, "type", "proposed", "run type: proposed, tracked, or destroy")
	return cmd
}

func newRunsApproveCmd(urlFlag, tokenFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "approve <run-id>",
		Short: "Approve a run in pending_approval state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return simplePost(urlFlag, tokenFlag, "/runs/"+args[0]+"/approve", "approved")
		},
	}
}

func newRunsConfirmCmd(urlFlag, tokenFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "confirm <run-id>",
		Short: "Confirm a run in unconfirmed state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return simplePost(urlFlag, tokenFlag, "/runs/"+args[0]+"/confirm", "confirmed")
		},
	}
}

func newRunsDiscardCmd(urlFlag, tokenFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "discard <run-id>",
		Short: "Discard a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return simplePost(urlFlag, tokenFlag, "/runs/"+args[0]+"/discard", "discarded")
		},
	}
}

func newRunsStatusCmd(urlFlag, tokenFlag *string, jsonFlag *bool) *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "status <run-id>",
		Short: "Get the status of a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(*urlFlag, *tokenFlag)
			if err != nil {
				return err
			}
			client := NewClient(cfg)
			path := "/runs/" + args[0]

			for {
				if *jsonFlag {
					data, err := client.RawGet(path)
					if err != nil {
						return err
					}
					fmt.Println(string(data))
					return nil
				}

				var run runSummary
				if err := client.Get(path, &run); err != nil {
					return err
				}
				printRunStatus(run)

				if !watch || terminalStatuses[run.Status] {
					return nil
				}
				time.Sleep(5 * time.Second)
			}
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "poll every 5s until terminal state")
	return cmd
}

func printRunStatus(r runSummary) {
	w := os.Stdout
	kvLine(w, "ID", r.ID)
	kvLine(w, "Stack", r.StackID)
	kvLine(w, "Type", r.Type)
	kvLine(w, "Status", r.Status)
	kvLine(w, "Trigger", r.Trigger)
	kvLine(w, "Queued", r.QueuedAt.Local().Format(time.DateTime))
	if r.StartedAt != nil {
		kvLine(w, "Started", r.StartedAt.Local().Format(time.DateTime))
	}
	if r.FinishedAt != nil {
		kvLine(w, "Finished", r.FinishedAt.Local().Format(time.DateTime))
	}
	if p := planSummary(r.PlanAdd, r.PlanChange, r.PlanDestroy); p != "" {
		kvLine(w, "Plan", p)
	}
}

func simplePost(urlFlag, tokenFlag *string, path, successVerb string) error {
	cfg, err := LoadConfig(*urlFlag, *tokenFlag)
	if err != nil {
		return err
	}
	client := NewClient(cfg)
	if _, err := client.RawPost(path, nil); err != nil {
		return err
	}
	fmt.Printf("Run %s\n", successVerb)
	return nil
}
