// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ponack/crucible-iap/internal/audit"
)

type chronicleAdapter struct {
	cfg ChronicleConfig
}

func newChronicleAdapter(cfg ChronicleConfig) *chronicleAdapter {
	if cfg.LogType == "" {
		cfg.LogType = "THIRD_PARTY_APP"
	}
	if cfg.Region == "" {
		cfg.Region = "us"
	}
	return &chronicleAdapter{cfg: cfg}
}

// serviceAccountJSON is the subset of fields we need from a GCP service account key file.
type serviceAccountJSON struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

const chronicleScope = "https://www.googleapis.com/auth/chronicle-backstory"

func (a *chronicleAdapter) fetchToken() (string, error) {
	var sa serviceAccountJSON
	if err := json.Unmarshal([]byte(a.cfg.ServiceAccountJSON), &sa); err != nil {
		return "", fmt.Errorf("chronicle: parse service account JSON: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("chronicle: decode private key PEM: no block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("chronicle: parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("chronicle: private key is not RSA")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"sub":   sa.ClientEmail,
		"aud":   sa.TokenURI,
		"scope": chronicleScope,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(rsaKey)
	if err != nil {
		return "", fmt.Errorf("chronicle: sign JWT: %w", err)
	}

	resp, err := http.PostForm(sa.TokenURI, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signed},
	})
	if err != nil {
		return "", fmt.Errorf("chronicle: token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("chronicle: token exchange status %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("chronicle: parse token response: %s", string(body))
	}
	return tok.AccessToken, nil
}

func (a *chronicleAdapter) Send(events []audit.Event) error {
	udmEvents := make([]map[string]any, len(events))
	for i, e := range events {
		udmEvents[i] = toUDM(e)
	}
	body, _ := json.Marshal(map[string]any{"events": udmEvents})
	return a.post(body)
}

func (a *chronicleAdapter) TestConnection() error {
	body, _ := json.Marshal(map[string]any{
		"events": []map[string]any{toUDM(audit.Event{
			Action:     "test.connection",
			ActorType:  "system",
			OccurredAt: time.Now(),
		})},
	})
	return a.post(body)
}

func (a *chronicleAdapter) post(body []byte) error {
	token, err := a.fetchToken()
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("https://%s-chronicle.googleapis.com/v2/udmevents:batchCreate", a.cfg.Region)
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := newHTTPClient(false)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf strings.Builder
		_, _ = io.Copy(&buf, resp.Body)
		return fmt.Errorf("chronicle returned %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}

// toUDM maps a Crucible audit event to a minimal Chronicle UDM event.
func toUDM(e audit.Event) map[string]any {
	udm := map[string]any{
		"metadata": map[string]any{
			"event_timestamp": e.OccurredAt.UTC().Format(time.RFC3339),
			"event_type":      "GENERIC_EVENT",
			"product_name":    "crucible-iap",
			"vendor_name":     "Forged in Feathers Technology",
		},
		"additional": map[string]any{
			"action":        e.Action,
			"actor_type":    e.ActorType,
			"actor_id":      e.ActorID,
			"resource_id":   e.ResourceID,
			"resource_type": e.ResourceType,
			"org_id":        e.OrgID,
		},
	}
	if e.ActorID != "" {
		udm["principal"] = map[string]any{
			"user": map[string]any{"userid": e.ActorID},
		}
	}
	if e.ResourceID != "" {
		udm["target"] = map[string]any{
			"resource": map[string]any{"name": e.ResourceType + "/" + e.ResourceID},
		}
	}
	return udm
}
