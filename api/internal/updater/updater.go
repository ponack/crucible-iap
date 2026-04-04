// SPDX-License-Identifier: AGPL-3.0-or-later
// Package updater checks GitHub Releases for a newer version of Crucible IAP
// and exposes the result to callers (health endpoint, startup log). The check
// is best-effort — failures are logged and never propagate to callers.
package updater

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	releaseURL     = "https://api.github.com/repos/ponack/crucible-iap/releases/latest"
	checkInterval  = 6 * time.Hour
	requestTimeout = 10 * time.Second
)

// Checker polls GitHub Releases and exposes the latest known version.
type Checker struct {
	current string

	mu            sync.RWMutex
	latestVersion string
	updateAvail   bool
}

// New creates a Checker for the given running version string (e.g. "v0.3.1" or "dev").
func New(currentVersion string) *Checker {
	return &Checker{current: currentVersion}
}

// Start performs an immediate check then rechecks every 6 hours.
// Must be called after the signal context is created so the goroutine
// exits cleanly on shutdown.
func (c *Checker) Start(ctx context.Context) {
	c.check()

	go func() {
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.check()
			}
		}
	}()
}

// LatestVersion returns the most recently fetched latest release tag.
// Returns an empty string if no check has succeeded yet.
func (c *Checker) LatestVersion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latestVersion
}

// UpdateAvailable reports whether the latest release is newer than the
// currently running version.
func (c *Checker) UpdateAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updateAvail
}

// check fetches the latest GitHub release and updates internal state.
func (c *Checker) check() {
	// Skip version comparison for dev builds.
	if c.current == "dev" || c.current == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		slog.Warn("updater: failed to build request", "err", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("updater: version check failed", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("updater: unexpected status from GitHub", "status", resp.StatusCode)
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("updater: failed to decode release response", "err", err)
		return
	}

	latest := release.TagName
	avail := isNewer(latest, c.current)

	c.mu.Lock()
	c.latestVersion = latest
	c.updateAvail = avail
	c.mu.Unlock()

	if avail {
		slog.Warn("updater: new version available",
			"current", c.current,
			"latest", latest,
			"release_url", "https://github.com/ponack/crucible-iap/releases/latest",
		)
	} else {
		slog.Info("updater: running latest version", "version", c.current)
	}
}

// isNewer returns true if latest is a higher semver than current.
// Both are expected to be "vMAJOR.MINOR.PATCH" tags; falls back to
// a simple string inequality check if parsing fails.
func isNewer(latest, current string) bool {
	if latest == "" || latest == current {
		return false
	}
	lv := parseSemver(latest)
	cv := parseSemver(current)
	if lv == [3]int{} || cv == [3]int{} {
		// Unparseable — treat any difference as "newer"
		return latest != current
	}
	for i := range lv {
		if lv[i] > cv[i] {
			return true
		}
		if lv[i] < cv[i] {
			return false
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	var v [3]int
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return v
	}
	for i, p := range parts {
		// strip pre-release suffix (e.g. "-rc1")
		p, _, _ = strings.Cut(p, "-")
		n := 0
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return [3]int{}
			}
			n = n*10 + int(ch-'0')
		}
		v[i] = n
	}
	return v
}
