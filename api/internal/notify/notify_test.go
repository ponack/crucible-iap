// SPDX-License-Identifier: AGPL-3.0-or-later
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testNotifier() *Notifier {
	return &Notifier{
		baseURL: "https://crucible.example.com",
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

// ── parseRepo ─────────────────────────────────────────────────────────────────

func TestParseRepo(t *testing.T) {
	tests := []struct {
		name         string
		repoURL      string
		vcsProvider  string
		vcsBaseURL   string
		wantOwner    string
		wantRepo     string
		wantProvider string
	}{
		{
			"github https",
			"https://github.com/owner/repo.git", "", "",
			"owner", "repo", "github",
		},
		{
			"github ssh",
			"git@github.com:owner/repo.git", "", "",
			"owner", "repo", "github",
		},
		{
			"gitlab.com https",
			"https://gitlab.com/owner/repo.git", "", "",
			"owner", "repo", "gitlab",
		},
		{
			"gitea with explicit provider and base URL",
			"https://gitea.example.com/owner/repo.git",
			"gitea", "https://gitea.example.com",
			"owner", "repo", "gitea",
		},
		{
			"gitlab subgroup takes last path segment",
			"https://gitlab.com/group/sub/myrepo.git", "gitlab", "",
			"group", "myrepo", "gitlab",
		},
		{
			"unknown host without provider hint",
			"https://unknown.example.com/owner/repo.git", "", "",
			"", "", "",
		},
		{
			"no .git suffix",
			"https://github.com/owner/repo", "", "",
			"owner", "repo", "github",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, provider := parseRepo(tt.repoURL, tt.vcsProvider, tt.vcsBaseURL)
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if provider != tt.wantProvider {
				t.Errorf("provider = %q, want %q", provider, tt.wantProvider)
			}
		})
	}
}

// ── slackPost ─────────────────────────────────────────────────────────────────

func TestSlackPost(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := testNotifier()
	n.slackPost(context.Background(), srv.URL, "hello slack")

	if gotBody["text"] != "hello slack" {
		t.Errorf("text = %q, want 'hello slack'", gotBody["text"])
	}
}

func TestSlackPostErr_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	n := testNotifier()
	err := n.slackPostErr(context.Background(), srv.URL, "test")
	if err == nil {
		t.Error("expected error for 401 response, got nil")
	}
}

// ── discordPost ───────────────────────────────────────────────────────────────

func TestDiscordPost(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := testNotifier()
	n.discordPost(context.Background(), srv.URL, "discord message")

	if gotBody["content"] != "discord message" {
		t.Errorf("content = %q, want 'discord message'", gotBody["content"])
	}
}

// ── teamsPost ─────────────────────────────────────────────────────────────────

func TestTeamsPost(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := testNotifier()
	n.teamsPost(context.Background(), srv.URL, "teams message")

	if gotBody["text"] != "teams message" {
		t.Errorf("text = %q, want 'teams message'", gotBody["text"])
	}
}

// ── ntfyPost ──────────────────────────────────────────────────────────────────

func TestNtfyPost(t *testing.T) {
	var gotHeaders http.Header
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := testNotifier()
	if err := n.ntfyPost(context.Background(), srv.URL, "tok123", "My Title", "my body", "high"); err != nil {
		t.Fatalf("ntfyPost: %v", err)
	}

	if gotBody != "my body" {
		t.Errorf("body = %q, want 'my body'", gotBody)
	}
	if gotHeaders.Get("Title") != "My Title" {
		t.Errorf("Title = %q, want 'My Title'", gotHeaders.Get("Title"))
	}
	if gotHeaders.Get("Priority") != "high" {
		t.Errorf("Priority = %q, want 'high'", gotHeaders.Get("Priority"))
	}
	if gotHeaders.Get("Authorization") != "Bearer tok123" {
		t.Errorf("Authorization = %q, want 'Bearer tok123'", gotHeaders.Get("Authorization"))
	}
}

func TestNtfyPost_NoToken(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := testNotifier()
	_ = n.ntfyPost(context.Background(), srv.URL, "", "title", "body", "default")

	if gotHeaders.Get("Authorization") != "" {
		t.Errorf("expected no Authorization header for empty token, got %q", gotHeaders.Get("Authorization"))
	}
}

func TestNtfyPost_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	n := testNotifier()
	err := n.ntfyPost(context.Background(), srv.URL, "", "t", "b", "default")
	if err == nil {
		t.Error("expected error for 403 response, got nil")
	}
}

// ── pure helpers ──────────────────────────────────────────────────────────────

func TestRunTitle(t *testing.T) {
	if got := runTitle("my-stack", true); got != "my-stack — run succeeded" {
		t.Errorf("success title = %q", got)
	}
	if got := runTitle("my-stack", false); got != "my-stack — run failed" {
		t.Errorf("failure title = %q", got)
	}
}

func TestBuildEmailMsg(t *testing.T) {
	msg := buildEmailMsg("from@example.com", "to@example.com", "Subject", "Body text")
	s := string(msg)
	for _, want := range []string{
		"From: from@example.com\r\n",
		"To: to@example.com\r\n",
		"Subject: Subject\r\n",
		"Body text",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q", want)
		}
	}
}
