// SPDX-License-Identifier: AGPL-3.0-or-later
// Runner spawns ephemeral Docker containers for each infrastructure run.
// Security model: read-only rootfs, no-new-privileges, tmpfs workspace, scoped JWT auth.
package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/ponack/crucible-iap/internal/config"
)

// JobSpec defines what the runner needs to execute.
type JobSpec struct {
	RunID       string
	StackID     string
	Tool        string // opentofu | terraform | ansible | pulumi
	RunnerImage string
	JobToken    string // short-lived JWT scoped to this run
	APIURL      string // Crucible API base URL for callbacks
	RepoURL     string
	RepoBranch  string
	ProjectRoot string
	RunType     string // tracked | proposed | destroy
}

type Runner struct {
	docker *client.Client
	cfg    *config.Config
}

func New(cfg *config.Config) (*Runner, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	return &Runner{docker: docker, cfg: cfg}, nil
}

// Execute spawns an ephemeral container for the given job and streams logs to w.
// The container is automatically removed on exit (--rm equivalent).
func (r *Runner) Execute(ctx context.Context, spec JobSpec, logWriter io.Writer) error {
	image := spec.RunnerImage
	if image == "" {
		image = r.cfg.RunnerDefaultImage
	}

	containerName := fmt.Sprintf("crucible-run-%s", spec.RunID[:8])

	// Scoped environment — credentials injected as env vars, never in image
	env := []string{
		"CRUCIBLE_RUN_ID=" + spec.RunID,
		"CRUCIBLE_STACK_ID=" + spec.StackID,
		"CRUCIBLE_API_URL=" + spec.APIURL,
		"CRUCIBLE_JOB_TOKEN=" + spec.JobToken,
		"CRUCIBLE_TOOL=" + spec.Tool,
		"CRUCIBLE_REPO_URL=" + spec.RepoURL,
		"CRUCIBLE_REPO_BRANCH=" + spec.RepoBranch,
		"CRUCIBLE_PROJECT_ROOT=" + spec.ProjectRoot,
		"CRUCIBLE_RUN_TYPE=" + spec.RunType,
	}

	resp, err := r.docker.ContainerCreate(ctx,
		&container.Config{
			Image:           image,
			Env:             env,
			NetworkDisabled: false, // egress needed for cloud provider APIs
			StopTimeout:     timeoutPtr(30),
		},
		&container.HostConfig{
			AutoRemove:  true,
			ReadonlyRootfs: true,
			SecurityOpt: []string{"no-new-privileges"},
			CapDrop:     []string{"ALL"},
			Resources: container.Resources{
				Memory:   parseMemory(r.cfg.RunnerMemoryLimit),
				NanoCPUs: parseCPU(r.cfg.RunnerCPULimit),
			},
			Mounts: []mount.Mount{
				{
					// Ephemeral workspace in RAM — disappears on container exit
					Type:   mount.TypeTmpfs,
					Target: "/workspace",
					TmpfsOptions: &mount.TmpfsOptions{
						SizeBytes: 512 * 1024 * 1024, // 512 MB
					},
				},
			},
			// Isolate on a dedicated network; configure egress rules externally
			NetworkMode: "crucible-runner",
		},
		nil, nil, containerName,
	)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	defer func() {
		// Force-remove on unexpected exit (AutoRemove handles normal exit)
		_ = r.docker.ContainerRemove(context.Background(), resp.ID,
			container.RemoveOptions{Force: true})
	}()

	if err := r.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	slog.Info("runner started", "container", containerName, "run_id", spec.RunID)

	// Stream logs back to caller
	logCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.RunnerJobTimeoutMinutes)*time.Minute)
	defer cancel()

	logs, err := r.docker.ContainerLogs(logCtx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("attach logs: %w", err)
	}
	defer logs.Close()

	if _, err := io.Copy(logWriter, logs); err != nil && err != context.Canceled {
		slog.Warn("log stream interrupted", "run_id", spec.RunID, "err", err)
	}

	// Wait for completion
	statusCh, errCh := r.docker.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("runner exited with code %d", status.StatusCode)
		}
	case err := <-errCh:
		return fmt.Errorf("container wait: %w", err)
	case <-logCtx.Done():
		return fmt.Errorf("run timed out after %d minutes", r.cfg.RunnerJobTimeoutMinutes)
	}

	return nil
}

func timeoutPtr(i int) *int { return &i }

// parseMemory converts "2g" → bytes (simplified).
func parseMemory(s string) int64 {
	var val int64
	var unit string
	fmt.Sscanf(s, "%d%s", &val, &unit)
	switch unit {
	case "g", "G":
		return val * 1024 * 1024 * 1024
	case "m", "M":
		return val * 1024 * 1024
	}
	return val
}

// parseCPU converts "1.0" → NanoCPUs.
func parseCPU(s string) int64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return int64(f * 1e9)
}
