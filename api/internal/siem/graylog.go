// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ponack/crucible-iap/internal/audit"
)

type graylogAdapter struct {
	cfg    GraylogConfig
	client *http.Client
}

func newGraylogAdapter(cfg GraylogConfig) *graylogAdapter {
	return &graylogAdapter{cfg: cfg, client: newHTTPClient(cfg.TLSInsecure)}
}

func (a *graylogAdapter) Send(events []audit.Event) error {
	for _, e := range events {
		if err := a.postGELF(toGELF(e)); err != nil {
			return err
		}
	}
	return nil
}

func (a *graylogAdapter) TestConnection() error {
	return a.postGELF(map[string]any{
		"version":       "1.1",
		"host":          "crucible-iap",
		"short_message": "crucible-iap siem test",
		"timestamp":     float64(time.Now().UnixNano()) / 1e9,
		"level":         6,
	})
}

func (a *graylogAdapter) postGELF(gelf map[string]any) error {
	body, _ := json.Marshal(gelf)
	req, err := http.NewRequest(http.MethodPost, a.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("graylog returned %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}

func toGELF(e audit.Event) map[string]any {
	return map[string]any{
		"version":       "1.1",
		"host":          "crucible-iap",
		"short_message": e.Action,
		"timestamp":     float64(e.OccurredAt.UnixNano()) / 1e9,
		"level":         6, // informational
		"_actor_id":     e.ActorID,
		"_actor_type":   e.ActorType,
		"_resource_id":  e.ResourceID,
		"_resource_type": e.ResourceType,
		"_org_id":       e.OrgID,
		"_event_id":     e.ID,
	}
}
