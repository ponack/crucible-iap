// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ponack/crucible-iap/internal/audit"
)

type datadogAdapter struct {
	cfg    DatadogConfig
	client *http.Client
}

func newDatadogAdapter(cfg DatadogConfig) *datadogAdapter {
	if cfg.Site == "" {
		cfg.Site = "datadoghq.com"
	}
	if cfg.Service == "" {
		cfg.Service = "crucible-iap"
	}
	return &datadogAdapter{cfg: cfg, client: newHTTPClient(false)}
}

type ddLog struct {
	DDSource string `json:"ddsource"`
	Service  string `json:"service"`
	DDTags   string `json:"ddtags,omitempty"`
	Message  string `json:"message"`
}

func (a *datadogAdapter) Send(events []audit.Event) error {
	logs := make([]ddLog, len(events))
	for i, e := range events {
		msg, _ := json.Marshal(e)
		logs[i] = ddLog{
			DDSource: "crucible-iap",
			Service:  a.cfg.Service,
			DDTags:   a.cfg.Tags,
			Message:  string(msg),
		}
	}
	return a.post(logs)
}

func (a *datadogAdapter) TestConnection() error {
	return a.post([]ddLog{{
		DDSource: "crucible-iap",
		Service:  a.cfg.Service,
		Message:  `{"test":"crucible-iap siem test"}`,
	}})
}

func (a *datadogAdapter) post(logs []ddLog) error {
	body, _ := json.Marshal(logs)
	url := fmt.Sprintf("https://http-intake.logs.%s/api/v2/logs", a.cfg.Site)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("DD-API-KEY", a.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("datadog returned %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}
