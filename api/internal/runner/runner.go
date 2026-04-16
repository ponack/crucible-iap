// SPDX-License-Identifier: AGPL-3.0-or-later
// Runner spawns ephemeral Docker containers for each infrastructure run.
// Security model: read-only rootfs, no-new-privileges, tmpfs workspace, scoped JWT auth.
package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/ponack/crucible-iap/internal/config"
)

// JobSpec defines what the runner needs to execute.
type JobSpec struct {
	RunID          string
	StackID        string
	Tool           string // opentofu | terraform | ansible | pulumi
	RunnerImage    string
	JobToken       string // short-lived JWT scoped to this run
	APIURL         string // Crucible API base URL for callbacks
	RepoURL        string
	RepoBranch     string
	ProjectRoot    string
	RunType        string   // tracked | proposed | destroy
	VCSToken       string   // plaintext token for authenticated git clone; empty = public repo
	ExtraEnv       []string // decrypted stack env vars as KEY=VALUE strings
	MemoryLimit    string   // Docker memory limit, e.g. "2g" — overrides config default if non-empty
	CPULimit       string   // Docker CPU limit, e.g. "1.0" — overrides config default if non-empty
	TimeoutMinutes int      // Job timeout — overrides config default if > 0
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
	r := &Runner{docker: docker, cfg: cfg}
	r.validateNetwork(context.Background())
	return r, nil
}

// validateNetwork checks the configured runner network exists and logs an
// actionable warning if not. Runs once at startup so operators learn about
// misconfiguration before the first job is queued, not mid-run.
func (r *Runner) validateNetwork(ctx context.Context) {
	networks, err := r.docker.NetworkList(ctx, filters.NewArgs(filters.Arg("name", r.cfg.RunnerNetwork)))
	if err != nil {
		slog.Warn("could not verify runner network — Docker API error", "network", r.cfg.RunnerNetwork, "err", err)
		return
	}
	for _, n := range networks {
		if n.Name == r.cfg.RunnerNetwork {
			return
		}
	}
	slog.Warn("runner network does not exist — containers will fail to start",
		"network", r.cfg.RunnerNetwork,
		"hint", fmt.Sprintf("docker network create %s", r.cfg.RunnerNetwork),
	)
}

// logline writes a prefixed informational line directly to logWriter so it
// appears in the run log regardless of container output.
func logline(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "[crucible] "+format+"\n", args...)
}

// Execute spawns an ephemeral container for the given job and streams logs to w.
// The container is automatically removed on exit (--rm equivalent).
func (r *Runner) Execute(ctx context.Context, spec JobSpec, logWriter io.Writer) error {
	image := spec.RunnerImage
	if image == "" {
		image = r.cfg.RunnerDefaultImage
	}

	containerName := fmt.Sprintf("crucible-run-%s", spec.RunID[:8])

	// Write a preamble so the user can see exactly what the runner is launching.
	vcsAuth := "none (public repo)"
	if spec.VCSToken != "" {
		vcsAuth = "token (via org integration)"
	}
	logline(logWriter, "run_id=%s stack_id=%s", spec.RunID, spec.StackID)
	logline(logWriter, "image=%s tool=%s run_type=%s", image, spec.Tool, spec.RunType)
	logline(logWriter, "repo=%s branch=%s project_root=%s", spec.RepoURL, spec.RepoBranch, spec.ProjectRoot)
	logline(logWriter, "vcs_auth=%s extra_env_vars=%d", vcsAuth, len(spec.ExtraEnv))
	logline(logWriter, "api_url=%s", spec.APIURL)
	logline(logWriter, "memory=%s cpu=%s timeout=%dm network=%s",
		coalesce(spec.MemoryLimit, r.cfg.RunnerMemoryLimit),
		coalesce(spec.CPULimit, r.cfg.RunnerCPULimit),
		func() int {
			if spec.TimeoutMinutes > 0 {
				return spec.TimeoutMinutes
			}
			return r.cfg.RunnerJobTimeoutMinutes
		}(),
		r.cfg.RunnerNetwork,
	)
	// Auto-pull image if not present locally — so operators never need to
	// manually docker pull before first use.
	if err := r.ensureImage(ctx, image, logWriter); err != nil {
		logline(logWriter, "ERROR: failed to pull image %q: %v", image, err)
		return fmt.Errorf("pull image: %w", err)
	}

	logline(logWriter, "--- spawning container %s ---", containerName)

	// Scoped environment — credentials injected as env vars, never in image.
	// ExtraEnv (decrypted stack env vars) is appended last so operators can
	// override tool behaviour without touching the image.
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
		"CRUCIBLE_VCS_TOKEN=" + spec.VCSToken, // empty string if no integration set
	}
	env = append(env, spec.ExtraEnv...)

	containerID, err := r.createContainer(ctx, spec, image, containerName, env, logWriter)
	if err != nil {
		return err
	}
	defer func() {
		_ = r.docker.ContainerRemove(context.Background(), containerID,
			container.RemoveOptions{Force: true})
	}()

	if err := r.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		logline(logWriter, "ERROR: failed to start container: %v", err)
		return fmt.Errorf("start container: %w", err)
	}

	slog.Info("runner started", "container", containerName, "run_id", spec.RunID)

	timeoutMins := spec.TimeoutMinutes
	if timeoutMins <= 0 {
		timeoutMins = r.cfg.RunnerJobTimeoutMinutes
	}
	return r.streamAndWait(ctx, containerID, spec.RunID, timeoutMins, logWriter)
}

// createContainer creates (but does not start) the ephemeral job container,
// logging actionable hints when common errors are detected.
func (r *Runner) createContainer(ctx context.Context, spec JobSpec, img, containerName string, env []string, logWriter io.Writer) (string, error) {
	resp, err := r.docker.ContainerCreate(ctx,
		&container.Config{
			Image:           img,
			Env:             env,
			NetworkDisabled: false, // egress needed for cloud provider APIs
			StopTimeout:     timeoutPtr(30),
		},
		&container.HostConfig{
			AutoRemove:     true,
			ReadonlyRootfs: true,
			SecurityOpt:    []string{"no-new-privileges"},
			CapDrop:        []string{"ALL"},
			Resources: container.Resources{
				Memory:   parseMemory(coalesce(spec.MemoryLimit, r.cfg.RunnerMemoryLimit)),
				NanoCPUs: parseCPU(coalesce(spec.CPULimit, r.cfg.RunnerCPULimit)),
			},
			// Tmpfs mounts use the string-options form so we can set the exec
			// flag explicitly. mount.TmpfsOptions has no field for mount flags,
			// meaning some Docker versions silently apply noexec — which
			// prevents provider binaries from being executed after download.
			Tmpfs: map[string]string{
				// Ephemeral workspace: repo clone, provider cache, plan artifact.
				"/workspace": "size=536870912,mode=0777,exec",
				// Staging area for provider zip downloads before extraction.
				// OpenTofu writes here before moving binaries into /workspace.
				"/tmp": "size=268435456,mode=0777,exec",
			},
			// Override with RUNNER_NETWORK env var (network must exist before first run).
			NetworkMode: container.NetworkMode(r.cfg.RunnerNetwork),
		},
		nil, nil, containerName,
	)
	if err != nil {
		logline(logWriter, "ERROR: failed to create container: %v", err)
		errStr := err.Error()
		switch {
		case strings.Contains(errStr, "No such image") || strings.Contains(errStr, "pull access denied"):
			logline(logWriter, "hint: runner image %q not present on Docker host — run: docker pull %s", img, img)
		case strings.Contains(errStr, "network") && strings.Contains(errStr, "not found"):
			logline(logWriter, "hint: Docker network %q does not exist — run: docker network create %s", r.cfg.RunnerNetwork, r.cfg.RunnerNetwork)
		}
		return "", fmt.Errorf("create container: %w", err)
	}
	return resp.ID, nil
}

// streamAndWait attaches to container logs, streams them to logWriter, then
// waits for the container to exit. Returns an error if the container exits
// non-zero or the timeout is exceeded.
func (r *Runner) streamAndWait(ctx context.Context, containerID, runID string, timeoutMins int, logWriter io.Writer) error {
	logCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMins)*time.Minute)
	defer cancel()

	logs, err := r.docker.ContainerLogs(logCtx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("attach logs: %w", err)
	}
	defer logs.Close()

	// Docker multiplexes stdout/stderr with an 8-byte frame header per chunk.
	// stdcopy.StdCopy strips those headers; plain io.Copy would emit binary garbage.
	if _, err := stdcopy.StdCopy(logWriter, logWriter, logs); err != nil && err != context.Canceled {
		slog.Warn("log stream interrupted", "run_id", runID, "err", err)
	}

	statusCh, errCh := r.docker.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("runner exited with code %d", status.StatusCode)
		}
	case err := <-errCh:
		return fmt.Errorf("container wait: %w", err)
	case <-logCtx.Done():
		return fmt.Errorf("run timed out after %d minutes", timeoutMins)
	}
	return nil
}

// ensureImage pulls the image if it is not already present on the Docker host.
// Logs progress so the user can see what's happening during a cold start.
func (r *Runner) ensureImage(ctx context.Context, img string, logWriter io.Writer) error {
	// Check if image already exists locally.
	images, err := r.docker.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", img)),
	})
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}
	if len(images) > 0 {
		return nil // already present
	}

	logline(logWriter, "image not found locally — pulling %s (first run may take a moment)", img)
	rc, err := r.docker.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	// Drain pull output (progress layers) — discard, we just need it to complete.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("pull stream: %w", err)
	}
	logline(logWriter, "image pulled successfully")
	return nil
}

func timeoutPtr(i int) *int { return &i }

// coalesce returns a if non-empty, otherwise b.
func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseMemory converts "2g" → bytes. Returns 2 GB and logs a warning on
// invalid input so containers never run unbounded (Docker treats 0 as unlimited).
func parseMemory(s string) int64 {
	const defaultMemory = 2 * 1024 * 1024 * 1024 // 2 GB
	var val int64
	var unit string
	if n, _ := fmt.Sscanf(s, "%d%s", &val, &unit); n == 0 || val <= 0 {
		slog.Warn("invalid runner memory limit, using 2GB default", "value", s)
		return defaultMemory
	}
	switch unit {
	case "g", "G":
		return val * 1024 * 1024 * 1024
	case "m", "M":
		return val * 1024 * 1024
	default:
		slog.Warn("unrecognised memory unit, using 2GB default", "value", s)
		return defaultMemory
	}
}

// parseCPU converts "1.0" → NanoCPUs. Returns 1 CPU and logs a warning on
// invalid input so containers never run unbounded (Docker treats 0 as unlimited).
func parseCPU(s string) int64 {
	var f float64
	if n, _ := fmt.Sscanf(s, "%f", &f); n == 0 || f <= 0 {
		slog.Warn("invalid runner CPU limit, using 1.0 default", "value", s)
		return int64(1e9)
	}
	return int64(f * 1e9)
}
