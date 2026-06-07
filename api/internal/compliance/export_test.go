// SPDX-License-Identifier: AGPL-3.0-or-later
package compliance

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/testutil"
)

func TestBundleSignatureRoundtrip(t *testing.T) {
	secret := []byte("test-secret-key")
	contents := bundleContents{
		OrgID:   "org-1",
		Filters: exportFilters{Start: time.Now().Add(-24 * time.Hour), End: time.Now()},
		Runs:    []runRow{{RunID: "r1", StackName: "demo", QueuedAt: time.Now()}},
	}

	data, err := writeZip(contents, secret)
	if err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	manifest, sig, err := readManifestAndSig(zr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Signature must verify with the same key.
	mac := hmac.New(sha256.New, secret)
	mac.Write(manifest)
	want := hex.EncodeToString(mac.Sum(nil))
	if want != sig {
		t.Errorf("signature mismatch: got %s want %s", sig, want)
	}

	// Signature must NOT verify with a different key (sanity).
	macWrong := hmac.New(sha256.New, []byte("wrong-key"))
	macWrong.Write(manifest)
	if hex.EncodeToString(macWrong.Sum(nil)) == sig {
		t.Error("signature accepted under wrong key")
	}

	// Manifest must list correct counts.
	var m Manifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.Counts["runs"] != 1 {
		t.Errorf("runs count: got %d want 1", m.Counts["runs"])
	}
	if m.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q want %q", m.SchemaVersion, "1")
	}
}

func TestBundleContainsExpectedFiles(t *testing.T) {
	data, err := writeZip(bundleContents{OrgID: "org-1"}, []byte("k"))
	if err != nil {
		t.Fatalf("writeZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	want := []string{
		"runs.json", "audit.json", "policy-results.json", "approvals.json",
		"runs.csv", "audit.csv",
		"manifest.json", "manifest.json.sig",
	}
	got := map[string]bool{}
	for _, f := range zr.File {
		got[f.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing file in bundle: %s", name)
		}
	}
}

func TestLoadRuns_FiltersByOrgAndWindow(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()
	h := NewHandler(pool, "k")

	org := testutil.InsertOrg(t, pool)
	otherOrg := testutil.InsertOrg(t, pool)
	stack := testutil.InsertStack(t, pool, org)
	otherStack := testutil.InsertStack(t, pool, otherOrg)

	// In-window run
	inWin := testutil.InsertRun(t, pool, stack, "finished", "tracked")
	// Out-of-window run (backdated)
	old := testutil.InsertRun(t, pool, stack, "finished", "tracked")
	if _, err := pool.Exec(ctx,
		`UPDATE runs SET queued_at = $1 WHERE id = $2`,
		time.Now().Add(-30*24*time.Hour), old); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	// Run in another org
	otherOrgRun := testutil.InsertRun(t, pool, otherStack, "finished", "tracked")

	start := time.Now().Add(-7 * 24 * time.Hour)
	end := time.Now().Add(24 * time.Hour)
	got, err := h.loadRuns(ctx, org, exportFilters{Start: start, End: end})
	if err != nil {
		t.Fatalf("loadRuns: %v", err)
	}

	ids := map[string]bool{}
	for _, r := range got {
		ids[r.RunID] = true
	}
	if !ids[inWin] {
		t.Errorf("expected in-window run %s to be included", inWin)
	}
	if ids[old] {
		t.Errorf("out-of-window run %s should be excluded", old)
	}
	if ids[otherOrgRun] {
		t.Errorf("other-org run %s should be excluded", otherOrgRun)
	}
}

func TestLoadAudit_FiltersByOrgAndWindow(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()
	h := NewHandler(pool, "k")

	org := testutil.InsertOrg(t, pool)
	otherOrg := testutil.InsertOrg(t, pool)

	insertAudit := func(targetOrg string, offset time.Duration) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO audit_events (actor_type, action, org_id, context, occurred_at)
			VALUES ('user', 'test', $1, '{}', now() + $2)
			RETURNING id
		`, targetOrg, offset).Scan(&id)
		if err != nil {
			t.Fatalf("insert audit: %v", err)
		}
		return id
	}

	inWin := insertAudit(org, -1*time.Hour)
	old := insertAudit(org, -30*24*time.Hour)
	otherOrgEvent := insertAudit(otherOrg, -1*time.Hour)

	start := time.Now().Add(-7 * 24 * time.Hour)
	end := time.Now().Add(24 * time.Hour)
	got, err := h.loadAudit(ctx, org, exportFilters{Start: start, End: end})
	if err != nil {
		t.Fatalf("loadAudit: %v", err)
	}

	ids := map[int64]bool{}
	for _, a := range got {
		ids[a.ID] = true
	}
	if !ids[inWin] {
		t.Errorf("expected in-window event %d", inWin)
	}
	if ids[old] {
		t.Errorf("out-of-window event %d should be excluded", old)
	}
	if ids[otherOrgEvent] {
		t.Errorf("other-org event %d should be excluded", otherOrgEvent)
	}
}

// readManifestAndSig pulls manifest.json bytes and the signature out of an
// unzipped bundle. Test helper.
func readManifestAndSig(zr *zip.Reader) (manifest []byte, sig string, err error) {
	for _, f := range zr.File {
		switch f.Name {
		case "manifest.json":
			rc, err := f.Open()
			if err != nil {
				return nil, "", err
			}
			manifest, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, "", err
			}
		case "manifest.json.sig":
			rc, err := f.Open()
			if err != nil {
				return nil, "", err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, "", err
			}
			sig = string(b)
		}
	}
	if manifest == nil {
		return nil, "", io.EOF
	}
	return manifest, sig, nil
}

// Silence unused import warning for pgxpool in case test setup grows.
var _ = (*pgxpool.Pool)(nil)
