// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/ponack/crucible-iap/internal/audit"
)

const chronicleScope = "https://www.googleapis.com/auth/chronicle-backstory"

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
			Action:    "test.connection",
			ActorType: "system",
			OccurredAt: time.Now(),
		})},
	})
	return a.post(body)
}

func (a *chronicleAdapter) post(body []byte) error {
	ctx := context.Background()
	creds, err := google.CredentialsFromJSON(ctx, []byte(a.cfg.ServiceAccountJSON), chronicleScope)
	if err != nil {
		return fmt.Errorf("chronicle: parse service account: %w", err)
	}
	token, err := creds.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("chronicle: obtain token: %w", err)
	}

	url := fmt.Sprintf("https://%s-chronicle.googleapis.com/v2/udmevents:batchCreate", a.cfg.Region)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	client := newHTTPClient(false)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
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
