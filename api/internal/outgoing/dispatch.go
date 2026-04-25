// SPDX-License-Identifier: AGPL-3.0-or-later
package outgoing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/vault"
)

type webhookRow struct {
	id         string
	url        string
	secretEnc  []byte
	eventTypes []string
	headers    map[string]string
}

type runRow struct {
	id          string
	stackID     string
	stackName   string
	orgID       string
	status      string
	runType     string
	trigger     string
	commitSHA   string
	branch      string
	planAdd     *int
	planChange  *int
	planDestroy *int
	createdAt   time.Time
}

type payload struct {
	Event     string     `json:"event"`
	Timestamp time.Time  `json:"timestamp"`
	Run       runPayload `json:"run"`
	Stack     stackInfo  `json:"stack"`
}

type runPayload struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Type        string    `json:"type"`
	Trigger     string    `json:"trigger"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	PlanAdd     *int      `json:"plan_add,omitempty"`
	PlanChange  *int      `json:"plan_change,omitempty"`
	PlanDestroy *int      `json:"plan_destroy,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	RunURL      string    `json:"run_url"`
}

type stackInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OrgID string `json:"org_id"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Dispatch fires all active outgoing webhooks for the given stack and event.
// Delivery is attempted up to 3 times with exponential backoff. All failures
// are logged and silently ignored — they must never affect the run lifecycle.
func Dispatch(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, runID, event, baseURL string) {
	run, err := loadRun(ctx, pool, runID)
	if err != nil {
		slog.Warn("outgoing: failed to load run", "run_id", runID, "err", err)
		return
	}

	rows, err := pool.Query(ctx, `
		SELECT id, url, secret_enc, event_types, headers
		FROM outgoing_webhooks
		WHERE stack_id = $1 AND is_active = true AND $2 = ANY(event_types)
	`, run.stackID, event)
	if err != nil {
		slog.Warn("outgoing: failed to query webhooks", "stack_id", run.stackID, "err", err)
		return
	}
	defer rows.Close()

	var webhooks []webhookRow
	for rows.Next() {
		var w webhookRow
		if err := rows.Scan(&w.id, &w.url, &w.secretEnc, &w.eventTypes, &w.headers); err != nil {
			continue
		}
		webhooks = append(webhooks, w)
	}
	rows.Close()

	if len(webhooks) == 0 {
		return
	}

	p := buildPayload(run, event, baseURL)
	body, err := json.Marshal(p)
	if err != nil {
		slog.Warn("outgoing: failed to marshal payload", "err", err)
		return
	}

	for _, w := range webhooks {
		var secret string
		if len(w.secretEnc) > 0 {
			if dec, err := v.Decrypt(run.stackID, w.secretEnc); err == nil {
				secret = string(dec)
			}
		}
		deliver(ctx, pool, w, run.id, event, body, secret)
	}
}

func deliver(ctx context.Context, pool *pgxpool.Pool, w webhookRow, runID, event string, body []byte, secret string) {
	backoff := []time.Duration{0, time.Second, 5 * time.Second}

	for attempt := 1; attempt <= 3; attempt++ {
		if d := backoff[attempt-1]; d > 0 {
			time.Sleep(d)
		}

		statusCode, err := post(w.url, w.headers, body, secret)
		if err != nil || statusCode >= 400 {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else {
				errMsg = fmt.Sprintf("HTTP %d", statusCode)
			}
			logDelivery(ctx, pool, w.id, runID, event, attempt, statusCode, errMsg)
			if attempt == 3 {
				slog.Warn("outgoing: webhook delivery failed after 3 attempts",
					"webhook_id", w.id, "url", w.url, "err", errMsg)
			}
			continue
		}
		logDelivery(ctx, pool, w.id, runID, event, attempt, statusCode, "")
		return
	}
}

func post(url string, headers map[string]string, body []byte, secret string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Crucible-Webhook/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Crucible-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func logDelivery(ctx context.Context, pool *pgxpool.Pool, whID, runID, event string, attempt, statusCode int, errMsg string) {
	var sc *int
	if statusCode != 0 {
		sc = &statusCode
	}
	var e *string
	if errMsg != "" {
		e = &errMsg
	}
	_, _ = pool.Exec(context.Background(), `
		INSERT INTO outgoing_webhook_deliveries (webhook_id, run_id, event_type, attempt, status_code, error)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6)
	`, whID, runID, event, attempt, sc, e)
}

func loadRun(ctx context.Context, pool *pgxpool.Pool, runID string) (*runRow, error) {
	var r runRow
	err := pool.QueryRow(ctx, `
		SELECT r.id, r.stack_id, s.name, s.org_id,
		       r.status, r.type, r.trigger,
		       COALESCE(r.commit_sha,''), COALESCE(r.branch,''),
		       r.plan_add, r.plan_change, r.plan_destroy,
		       r.created_at
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1
	`, runID).Scan(
		&r.id, &r.stackID, &r.stackName, &r.orgID,
		&r.status, &r.runType, &r.trigger,
		&r.commitSHA, &r.branch,
		&r.planAdd, &r.planChange, &r.planDestroy,
		&r.createdAt,
	)
	return &r, err
}

func buildPayload(r *runRow, event, baseURL string) payload {
	return payload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Run: runPayload{
			ID:          r.id,
			Status:      r.status,
			Type:        r.runType,
			Trigger:     r.trigger,
			CommitSHA:   r.commitSHA,
			Branch:      r.branch,
			PlanAdd:     r.planAdd,
			PlanChange:  r.planChange,
			PlanDestroy: r.planDestroy,
			CreatedAt:   r.createdAt,
			RunURL:      baseURL + "/runs/" + r.id,
		},
		Stack: stackInfo{
			ID:    r.stackID,
			Name:  r.stackName,
			OrgID: r.orgID,
		},
	}
}
