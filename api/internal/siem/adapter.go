// SPDX-License-Identifier: AGPL-3.0-or-later
// Package siem implements fan-out delivery of audit events to external SIEM systems.
package siem

import (
	"encoding/json"
	"fmt"

	"github.com/ponack/crucible-iap/internal/audit"
)

// Adapter sends a batch of audit events to one external SIEM destination.
type Adapter interface {
	Send(events []audit.Event) error
	TestConnection() error
}

// NewAdapter constructs the concrete adapter for destType using the decrypted
// config JSON. Returns an error if the type is unknown or config is malformed.
func NewAdapter(destType string, configJSON []byte) (Adapter, error) {
	switch destType {
	case "splunk":
		var cfg SplunkConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("splunk config: %w", err)
		}
		return newSplunkAdapter(cfg), nil
	case "datadog":
		var cfg DatadogConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("datadog config: %w", err)
		}
		return newDatadogAdapter(cfg), nil
	case "elasticsearch":
		var cfg ElasticsearchConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("elasticsearch config: %w", err)
		}
		return newElasticsearchAdapter(cfg), nil
	case "webhook":
		var cfg WebhookConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("webhook config: %w", err)
		}
		return newWebhookAdapter(cfg), nil
	case "chronicle":
		var cfg ChronicleConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("chronicle config: %w", err)
		}
		return newChronicleAdapter(cfg), nil
	case "wazuh":
		var cfg WazuhConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("wazuh config: %w", err)
		}
		return newWazuhAdapter(cfg), nil
	case "graylog":
		var cfg GraylogConfig
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("graylog config: %w", err)
		}
		return newGraylogAdapter(cfg), nil
	default:
		return nil, fmt.Errorf("unknown SIEM destination type: %q", destType)
	}
}

// ── Config structs ────────────────────────────────────────────────────────────

type SplunkConfig struct {
	URL         string `json:"url"`
	Token       string `json:"token"`
	Index       string `json:"index"`
	Source      string `json:"source"`
	SourceType  string `json:"sourcetype"`
	TLSInsecure bool   `json:"tls_insecure"`
}

type DatadogConfig struct {
	APIKey  string `json:"api_key"`
	Site    string `json:"site"`    // datadoghq.com | datadoghq.eu | us3.datadoghq.com
	Service string `json:"service"` // defaults to "crucible-iap"
	Tags    string `json:"tags"`    // comma-separated key:value
}

type ElasticsearchConfig struct {
	URL         string `json:"url"`
	Index       string `json:"index"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	APIKey      string `json:"api_key"`     // base64(id:key), alternative to user/pass
	PipelineID  string `json:"pipeline_id"` // optional ingest pipeline
	TLSInsecure bool   `json:"tls_insecure"`
}

type WebhookConfig struct {
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Secret      string            `json:"secret"` // HMAC-SHA256 signing secret
	TLSInsecure bool              `json:"tls_insecure"`
}

type ChronicleConfig struct {
	Region             string `json:"region"`       // us | eu | asia
	CustomerID         string `json:"customer_id"`
	LogType            string `json:"log_type"`     // defaults to "THIRD_PARTY_APP"
	ServiceAccountJSON string `json:"service_account_json"`
}

type WazuhConfig struct {
	// REST API (default)
	URL         string `json:"url"`      // https://wazuh-manager:55000
	Username    string `json:"username"`
	Password    string `json:"password"`
	AgentID     string `json:"agent_id"` // defaults to "000" (manager)
	TLSInsecure bool   `json:"tls_insecure"`
	// Syslog mode — used when SyslogAddress is set (mutually exclusive with REST)
	SyslogAddress string `json:"syslog_address"` // host:port TCP syslog
}

type GraylogConfig struct {
	URL         string `json:"url"` // http://host:12201/gelf
	TLSInsecure bool   `json:"tls_insecure"`
}
