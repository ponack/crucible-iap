// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ponack/crucible-iap/internal/audit"
)

type wazuhAdapter struct {
	cfg    WazuhConfig
	client *http.Client
}

func newWazuhAdapter(cfg WazuhConfig) *wazuhAdapter {
	if cfg.AgentID == "" {
		cfg.AgentID = "000"
	}
	return &wazuhAdapter{cfg: cfg, client: newHTTPClient(cfg.TLSInsecure)}
}

func (a *wazuhAdapter) Send(events []audit.Event) error {
	if a.cfg.SyslogAddress != "" {
		return a.sendSyslog(events)
	}
	return a.sendREST(events)
}

func (a *wazuhAdapter) TestConnection() error {
	if a.cfg.SyslogAddress != "" {
		conn, err := net.DialTimeout("tcp", a.cfg.SyslogAddress, 5*time.Second)
		if err != nil {
			return fmt.Errorf("wazuh syslog: dial %s: %w", a.cfg.SyslogAddress, err)
		}
		return conn.Close()
	}
	// REST: authenticate and check manager info
	token, err := a.fetchToken()
	if err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(a.cfg.URL, "/")+"/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (a *wazuhAdapter) fetchToken() (string, error) {
	url := strings.TrimRight(a.cfg.URL, "/") + "/security/user/authenticate"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(a.cfg.Username, a.cfg.Password)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("wazuh authenticate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("wazuh authenticate: status %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("wazuh authenticate: decode: %w", err)
	}
	return result.Data.Token, nil
}

func (a *wazuhAdapter) sendREST(events []audit.Event) error {
	token, err := a.fetchToken()
	if err != nil {
		return err
	}
	for _, e := range events {
		payload, _ := json.Marshal(map[string]any{
			"event":    e,
			"agent_id": a.cfg.AgentID,
		})
		url := strings.TrimRight(a.cfg.URL, "/") + "/events"
		req, reqErr := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := a.client.Do(req)
		if doErr != nil {
			return doErr
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("wazuh events: status %d", resp.StatusCode)
		}
	}
	return nil
}

func (a *wazuhAdapter) sendSyslog(events []audit.Event) error {
	conn, err := net.DialTimeout("tcp", a.cfg.SyslogAddress, 5*time.Second)
	if err != nil {
		return fmt.Errorf("wazuh syslog: dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	for _, e := range events {
		msg, _ := json.Marshal(e)
		// RFC 5424 minimal frame: <14>1 TIMESTAMP HOSTNAME APP PROCID MSGID - MSG
		frame := fmt.Sprintf("<14>1 %s crucible-iap - %s - %s\n",
			e.OccurredAt.UTC().Format(time.RFC3339),
			e.Action,
			string(msg),
		)
		if _, err := fmt.Fprint(conn, frame); err != nil {
			return fmt.Errorf("wazuh syslog: write: %w", err)
		}
	}
	return nil
}
