// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ponack/crucible-iap/internal/audit"
)

type splunkAdapter struct {
	cfg    SplunkConfig
	client *http.Client
}

func newSplunkAdapter(cfg SplunkConfig) *splunkAdapter {
	return &splunkAdapter{cfg: cfg, client: newHTTPClient(cfg.TLSInsecure)}
}

func (a *splunkAdapter) Send(events []audit.Event) error {
	var buf strings.Builder
	for _, e := range events {
		obj := map[string]any{
			"time":       float64(e.OccurredAt.UnixNano()) / 1e9,
			"sourcetype": a.cfg.SourceType,
			"event":      e,
		}
		if a.cfg.SourceType == "" {
			obj["sourcetype"] = "_json"
		}
		if a.cfg.Source != "" {
			obj["source"] = a.cfg.Source
		}
		if a.cfg.Index != "" {
			obj["index"] = a.cfg.Index
		}
		b, _ := json.Marshal(obj)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return a.post(buf.String())
}

func (a *splunkAdapter) TestConnection() error {
	payload, _ := json.Marshal(map[string]any{
		"time":       float64(time.Now().UnixNano()) / 1e9,
		"sourcetype": "_json",
		"event":      map[string]string{"test": "crucible-iap siem test"},
	})
	return a.post(string(payload))
}

func (a *splunkAdapter) post(body string) error {
	req, err := http.NewRequest(http.MethodPost, a.cfg.URL+"/services/collector/event", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Splunk "+a.cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("splunk HEC returned %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}
