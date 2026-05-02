// SPDX-License-Identifier: AGPL-3.0-or-later
package policygit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/policies"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/vault"
	"github.com/riverqueue/river"
)

var validTypes = map[string]bool{
	"pre_plan": true, "post_plan": true,
	"pre_apply": true, "post_apply": true,
	"approval": true, "trigger": true, "login": true,
}

// PolicySyncWorker fetches a policy git source archive and upserts .rego files as policies.
type PolicySyncWorker struct {
	river.WorkerDefaults[queue.PolicySyncArgs]
	pool   *pgxpool.Pool
	vault  *vault.Vault
	engine *policy.Engine
}

func NewPolicySyncWorker(pool *pgxpool.Pool, v *vault.Vault, e *policy.Engine) *PolicySyncWorker {
	return &PolicySyncWorker{pool: pool, vault: v, engine: e}
}

type sourceConfig struct {
	id          string
	orgID       string
	repoURL     string
	branch      string
	path        string
	vcsProvider string
	vcsBaseURL  string
	integID     string
	integEnc    []byte
	mirrorMode  bool
}

func (w *PolicySyncWorker) Work(ctx context.Context, job *river.Job[queue.PolicySyncArgs]) error {
	args := job.Args
	log := slog.With("source_id", args.SourceID)
	log.Info("starting policy sync")

	src, err := w.loadSource(ctx, args.SourceID)
	if err != nil {
		return fmt.Errorf("load source: %w", err)
	}

	token := w.loadToken(ctx, src)
	archiveURL := buildBranchArchiveURL(src.vcsProvider, src.vcsBaseURL, src.repoURL, src.branch)

	data, err := fetchArchive(ctx, archiveURL, token)
	if err != nil {
		w.recordError(ctx, args.SourceID, err.Error())
		return fmt.Errorf("fetch archive: %w", err)
	}

	files, err := extractRegoFiles(data, src.path)
	if err != nil {
		w.recordError(ctx, args.SourceID, err.Error())
		return fmt.Errorf("extract rego: %w", err)
	}

	created, updated := w.upsertPolicies(ctx, src, files)
	if src.mirrorMode {
		w.deleteStalePolicies(ctx, src, files)
	}

	if err := policies.LoadEngine(ctx, w.pool, w.engine); err != nil {
		slog.Warn("policy engine reload failed after git sync", "source_id", args.SourceID, "err", err)
	}

	sha := args.CommitSHA
	if sha == "" {
		sha = "HEAD"
	}
	_, _ = w.pool.Exec(ctx,
		`UPDATE policy_git_sources SET last_synced_at=$2, last_sync_sha=$3, last_sync_error='' WHERE id=$1`,
		args.SourceID, time.Now(), sha)

	log.Info("policy sync complete", "created", created, "updated", updated, "files", len(files))
	return nil
}

func (w *PolicySyncWorker) loadSource(ctx context.Context, id string) (sourceConfig, error) {
	var s sourceConfig
	s.id = id
	err := w.pool.QueryRow(ctx, `
		SELECT org_id, repo_url, branch, path, vcs_provider, vcs_base_url,
		       COALESCE(vcs_integration_id::text,''),
		       COALESCE(oi.config_enc, ''::bytea),
		       mirror_mode
		FROM policy_git_sources pgs
		LEFT JOIN org_integrations oi ON oi.id = pgs.vcs_integration_id
		WHERE pgs.id = $1
	`, id).Scan(&s.orgID, &s.repoURL, &s.branch, &s.path,
		&s.vcsProvider, &s.vcsBaseURL, &s.integID, &s.integEnc, &s.mirrorMode)
	return s, err
}

func (w *PolicySyncWorker) loadToken(ctx context.Context, src sourceConfig) string {
	if src.integID == "" || len(src.integEnc) == 0 {
		return ""
	}
	plain, err := w.vault.DecryptFor("crucible-integration:"+src.integID, src.integEnc)
	if err != nil {
		return ""
	}
	var cfg struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return ""
	}
	return cfg.Token
}

func (w *PolicySyncWorker) upsertPolicies(ctx context.Context, src sourceConfig, files map[string]string) (int, int) {
	created, updated := 0, 0
	for filePath, content := range files {
		name, ptype := extractPolicyMeta(filePath, content)
		tag, err := w.pool.Exec(ctx, `
			UPDATE policies SET body=$4, type=$5, updated_at=NOW()
			WHERE org_id=$1 AND git_source_id=$2 AND git_source_path=$3
		`, src.orgID, src.id, filePath, content, ptype)
		if err != nil {
			slog.Warn("update policy failed", "path", filePath, "err", err)
			continue
		}
		if tag.RowsAffected() > 0 {
			updated++
			continue
		}
		_, err = w.pool.Exec(ctx, `
			INSERT INTO policies (org_id, name, type, body, git_source_id, git_source_path)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT DO NOTHING
		`, src.orgID, name, ptype, content, src.id, filePath)
		if err != nil {
			slog.Warn("insert policy failed", "path", filePath, "err", err)
		} else {
			created++
		}
	}
	return created, updated
}

func (w *PolicySyncWorker) deleteStalePolicies(ctx context.Context, src sourceConfig, files map[string]string) {
	kept := make([]string, 0, len(files))
	for fp := range files {
		kept = append(kept, fp)
	}
	_, _ = w.pool.Exec(ctx, `
		DELETE FROM policies
		WHERE org_id=$1 AND git_source_id=$2
		  AND NOT (git_source_path = ANY($3::text[]))
	`, src.orgID, src.id, kept)
}

func (w *PolicySyncWorker) recordError(ctx context.Context, id, msg string) {
	_, _ = w.pool.Exec(ctx,
		`UPDATE policy_git_sources SET last_sync_error=$2 WHERE id=$1`, id, msg)
}

// ── VCS helpers ───────────────────────────────────────────────────────────────

func buildBranchArchiveURL(vcsProvider, vcsBaseURL, repoURL, branch string) string {
	switch vcsProvider {
	case "gitlab":
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
		return host + "/api/v4/projects/" + projectPath + "/repository/archive.tar.gz?sha=" + branch
	default:
		base := strings.TrimSuffix(repoURL, ".git")
		return base + "/archive/refs/heads/" + branch + ".tar.gz"
	}
}

func fetchArchive(ctx context.Context, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive fetch returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// extractRegoFiles unpacks a VCS tar.gz and returns path→content for all .rego files
// under the configured path prefix.
func extractRegoFiles(data []byte, pathPrefix string) (map[string]string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	files := map[string]string{}
	stripPrefix := ""

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if stripPrefix == "" {
			if i := strings.Index(hdr.Name, "/"); i != -1 {
				stripPrefix = hdr.Name[:i+1]
			}
		}
		rel := resolveRelPath(hdr.Name, stripPrefix, pathPrefix)
		if rel == "" || path.Ext(rel) != ".rego" {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		files[rel] = string(b)
	}
	return files, nil
}

func resolveRelPath(name, stripPrefix, pathPrefix string) string {
	rel := strings.TrimPrefix(name, stripPrefix)
	if pathPrefix == "" || pathPrefix == "." {
		return rel
	}
	prefix := strings.Trim(pathPrefix, "/") + "/"
	if !strings.HasPrefix(rel, prefix) {
		return ""
	}
	return strings.TrimPrefix(rel, prefix)
}

// extractPolicyMeta derives name and type from file path and content.
// Type resolution: parent dir → # crucible:type comment → "post_plan".
func extractPolicyMeta(filePath, content string) (name, ptype string) {
	base := path.Base(filePath)
	name = strings.TrimSuffix(base, ".rego")
	dir := path.Dir(filePath)
	if dir != "." && validTypes[dir] {
		return name, dir
	}
	return name, extractTypeComment(content)
}

func extractTypeComment(content string) string {
	for _, line := range strings.SplitN(content, "\n", 20) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# crucible:type ") {
			continue
		}
		t := strings.TrimSpace(strings.TrimPrefix(line, "# crucible:type "))
		if validTypes[t] {
			return t
		}
	}
	return "post_plan"
}
