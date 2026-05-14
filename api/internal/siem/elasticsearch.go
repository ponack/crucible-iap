// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ponack/crucible-iap/internal/audit"
)

type elasticsearchAdapter struct {
	cfg    ElasticsearchConfig
	client *http.Client
}

func newElasticsearchAdapter(cfg ElasticsearchConfig) *elasticsearchAdapter {
	if cfg.Index == "" {
		cfg.Index = "crucible-audit"
	}
	return &elasticsearchAdapter{cfg: cfg, client: newHTTPClient(cfg.TLSInsecure)}
}

func (a *elasticsearchAdapter) Send(events []audit.Event) error {
	var ndjson strings.Builder
	for _, e := range events {
		ndjson.WriteString(`{"index":{}}`)
		ndjson.WriteByte('\n')
		doc, _ := json.Marshal(e)
		ndjson.Write(doc)
		ndjson.WriteByte('\n')
	}
	url := strings.TrimRight(a.cfg.URL, "/") + "/" + a.cfg.Index + "/_bulk"
	if a.cfg.PipelineID != "" {
		url += "?pipeline=" + a.cfg.PipelineID
	}
	return a.doRequest(http.MethodPost, url, strings.NewReader(ndjson.String()), "application/x-ndjson")
}

func (a *elasticsearchAdapter) TestConnection() error {
	doc, _ := json.Marshal(map[string]string{"test": "crucible-iap siem test"})
	url := strings.TrimRight(a.cfg.URL, "/") + "/" + a.cfg.Index + "/_doc/crucible-siem-test"
	return a.doRequest(http.MethodPut, url, bytes.NewReader(doc), "application/json")
}

func (a *elasticsearchAdapter) doRequest(method, url string, body io.Reader, contentType string) error {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if a.cfg.APIKey != "" {
		req.Header.Set("Authorization", "ApiKey "+a.cfg.APIKey)
	} else if a.cfg.Username != "" {
		req.SetBasicAuth(a.cfg.Username, a.cfg.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("elasticsearch returned %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}
