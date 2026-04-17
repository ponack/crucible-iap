// SPDX-License-Identifier: AGPL-3.0-or-later
package worker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/secretstore"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/riverqueue/river"
)

// ModulePublishWorker downloads a VCS archive for a tagged release and publishes
// it as a Terraform module to the private registry.
type ModulePublishWorker struct {
	river.WorkerDefaults[queue.ModulePublishArgs]
	pool    *pgxpool.Pool
	storage *storage.Client
	vault   *vault.Vault
}

func (w *ModulePublishWorker) Work(ctx context.Context, job *river.Job[queue.ModulePublishArgs]) error {
	args := job.Args
	log := slog.With("stack_id", args.StackID, "tag", args.TagName,
		"module", args.Namespace+"/"+args.Name+"/"+args.Provider)

	log.Info("starting module publish job")

	var repoURL, projectRoot, orgID, vcsProvider, vcsBaseURL string
	err := w.pool.QueryRow(ctx, `
		SELECT repo_url, project_root, org_id,
		       COALESCE(vcs_provider,'github'), COALESCE(vcs_base_url,'')
		FROM stacks WHERE id = $1
	`, args.StackID).Scan(&repoURL, &projectRoot, &orgID, &vcsProvider, &vcsBaseURL)
	if err != nil {
		return fmt.Errorf("load stack: %w", err)
	}

	vcsToken, err := secretstore.LoadVCSToken(ctx, w.pool, w.vault, args.StackID)
	if err != nil {
		log.Warn("no VCS token, attempting unauthenticated download", "err", err)
	}

	archiveURL := buildArchiveURL(vcsProvider, vcsBaseURL, repoURL, args.TagName)
	data, err := downloadArchive(ctx, archiveURL, vcsToken)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}

	repackaged, readme, err := repackageModule(data, projectRoot)
	if err != nil {
		return fmt.Errorf("repackage module: %w", err)
	}

	key := storage.ModuleKey(args.Namespace, args.Name, args.Provider, args.Version)
	if err := w.storage.PutModule(ctx, key, bytes.NewReader(repackaged), int64(len(repackaged))); err != nil {
		return fmt.Errorf("store module: %w", err)
	}

	_, err = w.pool.Exec(ctx, `
		INSERT INTO registry_modules
		  (org_id, namespace, name, provider, version, storage_key, readme)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (org_id, namespace, name, provider, version)
		DO UPDATE SET storage_key=$6, readme=$7, published_at=NOW(), yanked=FALSE
	`, orgID, args.Namespace, args.Name, args.Provider, args.Version, key, readme)
	if err != nil {
		return fmt.Errorf("record module: %w", err)
	}

	log.Info("module published", "version", args.Version, "key", key)
	return nil
}

// buildArchiveURL constructs the VCS provider archive download URL for the given tag.
// repoURL is expected in the form https://github.com/owner/repo (no trailing slash).
func buildArchiveURL(vcsProvider, vcsBaseURL, repoURL, tagName string) string {
	switch vcsProvider {
	case "gitlab":
		// Derive project path from repoURL: https://gitlab.com/owner/repo -> owner%2Frepo
		base := strings.TrimSuffix(repoURL, ".git")
		parts := strings.SplitN(base, "/", 4)
		if len(parts) < 4 {
			return repoURL
		}
		projectPath := strings.ReplaceAll(parts[3], "/", "%2F")
		host := parts[0] + "//" + parts[2]
		if vcsBaseURL != "" {
			host = strings.TrimSuffix(vcsBaseURL, "/")
		}
		return host + "/api/v4/projects/" + projectPath + "/repository/archive.tar.gz?sha=" + tagName
	default:
		// GitHub / Gitea / Gogs: https://github.com/owner/repo/archive/refs/tags/v1.0.0.tar.gz
		base := strings.TrimSuffix(repoURL, ".git")
		return base + "/archive/refs/tags/" + tagName + ".tar.gz"
	}
}

func downloadArchive(ctx context.Context, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive fetch returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// repackageModule reads a VCS tar.gz archive, strips the top-level directory
// added by GitHub/GitLab/Gitea, optionally strips a projectRoot subdirectory,
// and returns a clean tar.gz suitable for Terraform module use.
// It also extracts the README.md text while iterating.
func repackageModule(data []byte, projectRoot string) ([]byte, string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("gzip open: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// Pass 1: collect all entries so we can determine the strip prefix.
	type entry struct {
		name string
		data []byte
		mode int64
	}
	var entries []entry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, 10<<20))
		if err != nil {
			return nil, "", fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}
		entries = append(entries, entry{name: hdr.Name, data: b, mode: hdr.Mode})
	}

	if len(entries) == 0 {
		return nil, "", fmt.Errorf("archive is empty")
	}

	// Determine the top-level prefix to strip (GitHub adds "repo-tagname/").
	stripPrefix := ""
	if i := strings.Index(entries[0].name, "/"); i != -1 {
		stripPrefix = entries[0].name[:i+1]
	}

	// Combine stripPrefix + projectRoot into a single prefix to strip.
	fullPrefix := stripPrefix
	if projectRoot != "" {
		fullPrefix = stripPrefix + strings.Trim(projectRoot, "/") + "/"
	}

	var readme string
	var outBuf bytes.Buffer
	gw := gzip.NewWriter(&outBuf)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		stripped := strings.TrimPrefix(e.name, fullPrefix)
		if stripped == "" || strings.HasPrefix(stripped, "../") {
			continue
		}
		base := path.Base(stripped)
		if strings.EqualFold(base, "readme.md") && readme == "" {
			readme = string(e.data)
		}
		hdr := &tar.Header{
			Name: stripped,
			Mode: e.mode,
			Size: int64(len(e.data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", err
		}
		if _, err := tw.Write(e.data); err != nil {
			return nil, "", err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	if err := gw.Close(); err != nil {
		return nil, "", err
	}
	return outBuf.Bytes(), readme, nil
}
