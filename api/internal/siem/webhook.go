// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ponack/crucible-iap/internal/audit"
)

type webhookAdapter struct {
	cfg    WebhookConfig
	client *http.Client
}

func newWebhookAdapter(cfg WebhookConfig) *webhookAdapter {
	return &webhookAdapter{cfg: cfg, client: newHTTPClient(cfg.TLSInsecure)}
}

func (a *webhookAdapter) Send(events []audit.Event) error {
	body, _ := json.Marshal(map[string]any{"events": events})
	return a.post(body)
}

func (a *webhookAdapter) TestConnection() error {
	body, _ := json.Marshal(map[string]bool{"test": true})
	return a.post(body)
}

func (a *webhookAdapter) post(body []byte) error {
	req, err := http.NewRequest(http.MethodPost, a.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}
	if a.cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(a.cfg.Secret))
		mac.Write(body)
		req.Header.Set("X-Crucible-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}
