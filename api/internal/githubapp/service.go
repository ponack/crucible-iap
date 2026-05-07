// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/vault"
)

// githubAPIBase is overridable in tests.
var githubAPIBase = "https://api.github.com"

// Service holds the runtime state needed to mint installation tokens and call
// the GitHub API on behalf of an installed app.
type Service struct {
	pool   *pgxpool.Pool
	vault  *vault.Vault
	cache  *TokenCache
	client *http.Client
}

func NewService(pool *pgxpool.Pool, v *vault.Vault) *Service {
	return &Service{
		pool:   pool,
		vault:  v,
		cache:  NewTokenCache(),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// loadAppByID returns the app's numeric appID and decrypted private-key PEM
// for the given app UUID.
func (s *Service) loadAppByID(ctx context.Context, appUUID string) (int64, []byte, error) {
	var appID int64
	var keyEnc []byte
	err := s.pool.QueryRow(ctx, `
		SELECT app_id, private_key_enc FROM github_apps WHERE id = $1
	`, appUUID).Scan(&appID, &keyEnc)
	if err != nil {
		return 0, nil, fmt.Errorf("load github app: %w", err)
	}
	pem, err := s.vault.DecryptFor(vaultContext(appUUID), keyEnc)
	if err != nil {
		return 0, nil, fmt.Errorf("decrypt private key: %w", err)
	}
	return appID, pem, nil
}

// loadAppByOrgID returns the app's numeric appID and decrypted private-key PEM
// for the org's registered GitHub App.
func (s *Service) loadAppByOrgID(ctx context.Context, orgID string) (int64, []byte, error) {
	var appUUID string
	var appID int64
	var keyEnc []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, app_id, private_key_enc FROM github_apps WHERE org_id = $1
	`, orgID).Scan(&appUUID, &appID, &keyEnc)
	if err != nil {
		return 0, nil, fmt.Errorf("load github app for org: %w", err)
	}
	pem, err := s.vault.DecryptFor(vaultContext(appUUID), keyEnc)
	if err != nil {
		return 0, nil, fmt.Errorf("decrypt private key: %w", err)
	}
	return appID, pem, nil
}

// loadAppByInstallation looks up the parent app for an installation row.
func (s *Service) loadAppByInstallation(ctx context.Context, installationID int64) (string, int64, []byte, error) {
	var appUUID string
	err := s.pool.QueryRow(ctx, `
		SELECT app_uuid FROM github_app_installations WHERE installation_id = $1
	`, installationID).Scan(&appUUID)
	if err != nil {
		return "", 0, nil, fmt.Errorf("load installation: %w", err)
	}
	appID, pem, err := s.loadAppByID(ctx, appUUID)
	if err != nil {
		return "", 0, nil, err
	}
	return appUUID, appID, pem, nil
}

// installationTokenResponse is the shape returned by GitHub for token exchange.
type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// InstallationToken returns a valid installation access token, minting a new
// one if the cached value is missing or near expiry.
func (s *Service) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	if tok, ok := s.cache.Lookup(installationID); ok {
		return tok, nil
	}
	_, appID, pem, err := s.loadAppByInstallation(ctx, installationID)
	if err != nil {
		return "", err
	}
	jwtTok, err := MintJWT(pem, appID)
	if err != nil {
		return "", fmt.Errorf("mint jwt: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBase, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwtTok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return "", fmt.Errorf("github returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var out installationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	s.cache.Store(installationID, out.Token, out.ExpiresAt)
	return out.Token, nil
}

// installationMetadata is what GitHub returns when querying an installation
// via the app JWT. We use it to enrich the row we created at install time.
type installationMetadata struct {
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

// RefreshInstallationMetadata fetches the installation's account login + type
// from GitHub and updates the row. Best-effort — failure is logged via the
// returned error and the row keeps its placeholder values.
func (s *Service) RefreshInstallationMetadata(ctx context.Context, installationID int64) error {
	_, appID, pem, err := s.loadAppByInstallation(ctx, installationID)
	if err != nil {
		return err
	}
	jwtTok, err := MintJWT(pem, appID)
	if err != nil {
		return fmt.Errorf("mint jwt: %w", err)
	}
	url := fmt.Sprintf("%s/app/installations/%d", githubAPIBase, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwtTok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("github HTTP %d: %s", resp.StatusCode, string(body))
	}
	var meta installationMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return err
	}
	if meta.Account.Login == "" {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE github_app_installations
		SET account_login = $1, account_type = $2
		WHERE installation_id = $3
	`, meta.Account.Login, meta.Account.Type, installationID)
	return err
}

// Repo is the metadata returned to the UI when listing repos accessible via an
// installation. We surface only what the stack-creation form needs.
type Repo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
	Private  bool   `json:"private"`
	Default  string `json:"default_branch"`
}

// ListRepos enumerates repos accessible to the given installation. Pages through
// the GitHub API until exhausted (or 10 pages, whichever comes first).
func (s *Service) ListRepos(ctx context.Context, installationID int64) ([]Repo, error) {
	tok, err := s.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	type listResp struct {
		Repositories []Repo `json:"repositories"`
	}

	out := []Repo{}
	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", githubAPIBase, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "token "+tok)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("github returned HTTP %d: %s", resp.StatusCode, string(body))
		}
		var lr listResp
		if err := json.Unmarshal(body, &lr); err != nil {
			return nil, fmt.Errorf("decode list response: %w", err)
		}
		if len(lr.Repositories) == 0 {
			break
		}
		out = append(out, lr.Repositories...)
		if len(lr.Repositories) < 100 {
			break
		}
	}
	return out, nil
}
