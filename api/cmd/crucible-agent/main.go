// SPDX-License-Identifier: AGPL-3.0-or-later
// crucible-agent is the external worker-agent binary for Crucible IAP.
// Deploy it on any host with Docker access; configure it with CRUCIBLE_API_URL,
// CRUCIBLE_ORG_ID, CRUCIBLE_POOL_TOKEN, and optionally CRUCIBLE_CAPACITY.
//
// The agent polls the Crucible API for queued runs assigned to its worker pool,
// executes each run inside an ephemeral Docker container (the same runner images
// used by the built-in worker), streams logs back to the API, and reports the
// final outcome so the server can drive policy evaluation, auto-apply, and
// downstream triggers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/runner"
)

func main() {
	apiURL := mustEnv("CRUCIBLE_API_URL")
	orgID := mustEnv("CRUCIBLE_ORG_ID")
	token := mustEnv("CRUCIBLE_POOL_TOKEN")
	capacity, pollInterval := loadAgentSettings()

	slog.Info("crucible-agent starting", "api_url", apiURL, "org_id", orgID, "capacity", capacity)

	r, err := runner.New(loadRunnerConfig())
	if err != nil {
		slog.Error("failed to connect to Docker", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	runLoop(ctx, apiURL, orgID, token, r, capacity, pollInterval)
}

func loadAgentSettings() (capacity int, pollInterval time.Duration) {
	capacity = 3
	if s := os.Getenv("CRUCIBLE_CAPACITY"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			capacity = n
		}
	}
	pollInterval = 5 * time.Second
	if s := os.Getenv("CRUCIBLE_POLL_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			pollInterval = d
		}
	}
	return
}

func loadRunnerConfig() *config.Config {
	c := &config.Config{RunnerJobTimeoutMinutes: 60}
	if s := os.Getenv("RUNNER_JOB_TIMEOUT_MINUTES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			c.RunnerJobTimeoutMinutes = n
		}
	}
	if img := os.Getenv("RUNNER_DEFAULT_IMAGE"); img != "" {
		c.RunnerDefaultImage = img
	}
	if net := os.Getenv("RUNNER_NETWORK"); net != "" {
		c.RunnerNetwork = net
	}
	if mem := os.Getenv("RUNNER_MEMORY_LIMIT"); mem != "" {
		c.RunnerMemoryLimit = mem
	}
	if cpu := os.Getenv("RUNNER_CPU_LIMIT"); cpu != "" {
		c.RunnerCPULimit = cpu
	}
	return c
}

func runLoop(ctx context.Context, apiURL, orgID, token string, r *runner.Runner, capacity int, pollInterval time.Duration) {
	sem := make(chan struct{}, capacity)
	var wg sync.WaitGroup

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down — waiting for in-flight runs")
			wg.Wait()
			slog.Info("crucible-agent stopped")
			return
		case <-ticker.C:
			drainQueue(ctx, apiURL, orgID, token, r, sem, &wg)
		}
	}
}

func drainQueue(ctx context.Context, apiURL, orgID, token string, r *runner.Runner, sem chan struct{}, wg *sync.WaitGroup) {
	for {
		select {
		case sem <- struct{}{}:
		default:
			return // all slots occupied
		}

		spec, ok := claim(ctx, apiURL, orgID, token)
		if !ok {
			<-sem
			return
		}

		wg.Add(1)
		go func(s jobSpec) {
			defer wg.Done()
			defer func() { <-sem }()
			executeRun(ctx, apiURL, orgID, token, r, s)
		}(spec)
	}
}

// ── Job spec (mirrors agent.JobSpec) ─────────────────────────────────────────

type jobSpec struct {
	RunID        string   `json:"run_id"`
	StackID      string   `json:"stack_id"`
	Tool         string   `json:"tool"`
	RunnerImage  string   `json:"runner_image"`
	RepoURL      string   `json:"repo_url"`
	RepoBranch   string   `json:"repo_branch"`
	ProjectRoot  string   `json:"project_root"`
	RunType      string   `json:"run_type"`
	AutoApply    bool     `json:"auto_apply"`
	VarOverrides []string `json:"var_overrides,omitempty"`
	Env          []string `json:"env,omitempty"`
	VCSToken     string   `json:"vcs_token,omitempty"`
	JobToken     string   `json:"job_token"`
	APIURL       string   `json:"api_url"`
}

// ── API helpers ───────────────────────────────────────────────────────────────

func claim(ctx context.Context, apiURL, orgID, token string) (jobSpec, bool) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/v1/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Org-ID", orgID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode == http.StatusNoContent {
		if resp != nil {
			resp.Body.Close()
		}
		return jobSpec{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("claim: unexpected status", "status", resp.StatusCode)
		return jobSpec{}, false
	}

	var spec jobSpec
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		slog.Error("claim: failed to decode spec", "err", err)
		return jobSpec{}, false
	}
	slog.Info("claimed run", "run_id", spec.RunID, "stack_id", spec.StackID, "run_type", spec.RunType)
	return spec, true
}

func sendLog(ctx context.Context, apiURL, orgID, token, runID string, data []byte) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		apiURL+"/api/v1/agent/runs/"+runID+"/log", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Org-ID", orgID)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("log upload failed", "run_id", runID, "err", err)
		return
	}
	resp.Body.Close()
}

func finish(ctx context.Context, apiURL, orgID, token, runID string, success bool, errMsg string) {
	body, _ := json.Marshal(map[string]any{"success": success, "error": errMsg})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		apiURL+"/api/v1/agent/runs/"+runID+"/finish", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Org-ID", orgID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("finish failed", "run_id", runID, "err", err)
		return
	}
	resp.Body.Close()
}

// ── Run execution ─────────────────────────────────────────────────────────────

func executeRun(ctx context.Context, apiURL, orgID, token string, r *runner.Runner, spec jobSpec) {
	log := slog.With("run_id", spec.RunID, "run_type", spec.RunType)
	log.Info("executing run")

	// Internal callback URL: agent uses the api_url from the spec (set by server).
	// Build runner spec — env from server already includes decrypted secrets.
	rspec := runner.JobSpec{
		RunID:          spec.RunID,
		StackID:        spec.StackID,
		Tool:           spec.Tool,
		RunnerImage:    spec.RunnerImage,
		JobToken:       spec.JobToken,
		APIURL:         spec.APIURL,
		RepoURL:        spec.RepoURL,
		RepoBranch:     spec.RepoBranch,
		ProjectRoot:    spec.ProjectRoot,
		RunType:        spec.RunType,
		VCSToken:       spec.VCSToken,
		ExtraEnv:       spec.Env,
		MemoryLimit:    os.Getenv("RUNNER_MEMORY_LIMIT"),
		CPULimit:       os.Getenv("RUNNER_CPU_LIMIT"),
		TimeoutMinutes: loadRunnerConfig().RunnerJobTimeoutMinutes,
		// MinIO not needed for external agents — state is managed by the server's MinIO.
	}

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(&logBuf, os.Stdout)

	runErr := r.Execute(ctx, rspec, logWriter)
	if runErr != nil {
		fmt.Fprintf(logWriter, "\n[crucible-agent] run failed: %v\n", runErr)
	}

	bg := context.Background()
	sendLog(bg, apiURL, orgID, token, spec.RunID, logBuf.Bytes())

	if runErr != nil {
		log.Error("run failed", "err", runErr)
		finish(bg, apiURL, orgID, token, spec.RunID, false, runErr.Error())
	} else {
		log.Info("run succeeded")
		finish(bg, apiURL, orgID, token, spec.RunID, true, "")
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "var", key)
		os.Exit(1)
	}
	return v
}
