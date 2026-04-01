// SPDX-License-Identifier: AGPL-3.0-or-later
package statebackend

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// S3Config holds credentials and bucket info for AWS S3 state storage.
// State is stored at s3://{bucket}/{key_prefix}{stackID}/terraform.tfstate
type S3Config struct {
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	KeyPrefix       string `json:"key_prefix,omitempty"` // e.g. "crucible/state/"
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	// EndpointURL allows pointing at MinIO or other S3-compatible stores.
	EndpointURL string `json:"endpoint_url,omitempty"`
}

type S3Backend struct {
	cfg    S3Config
	client *http.Client
}

func (b *S3Backend) httpClient() *http.Client {
	if b.client != nil {
		return b.client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (b *S3Backend) objectURL(stackID string) string {
	prefix := b.cfg.KeyPrefix
	key := prefix + stackID + "/terraform.tfstate"
	if b.cfg.EndpointURL != "" {
		base := strings.TrimRight(b.cfg.EndpointURL, "/")
		return base + "/" + b.cfg.Bucket + "/" + key
	}
	return "https://" + b.cfg.Bucket + ".s3." + b.cfg.Region + ".amazonaws.com/" + key
}

func (b *S3Backend) creds() (keyID, secret string) {
	keyID = b.cfg.AccessKeyID
	secret = b.cfg.SecretAccessKey
	if keyID == "" {
		keyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if secret == "" {
		secret = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	return
}

func (b *S3Backend) GetState(ctx context.Context, stackID string) (io.ReadCloser, error) {
	url := b.objectURL(stackID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	keyID, secret := b.creds()
	s3SignRequest(req, nil, keyID, secret, b.cfg.Region)

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("s3 get: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (b *S3Backend) PutState(ctx context.Context, stackID string, data []byte) error {
	url := b.objectURL(stackID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	keyID, secret := b.creds()
	s3SignRequest(req, data, keyID, secret, b.cfg.Region)

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("s3 put: status %d", resp.StatusCode)
	}
	return nil
}

func (b *S3Backend) DeleteState(ctx context.Context, stackID string) error {
	url := b.objectURL(stackID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	keyID, secret := b.creds()
	s3SignRequest(req, nil, keyID, secret, b.cfg.Region)

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("s3 delete: status %d", resp.StatusCode)
	}
	return nil
}

// s3SignRequest adds AWS Signature Version 4 headers (re-uses the same algorithm
// as the secretstore AWS provider, duplicated here to keep packages independent).
func s3SignRequest(req *http.Request, body []byte, keyID, secret, region string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	if body == nil {
		body = []byte{}
	}
	bodyHash := hex.EncodeToString(s3SHA256(body))
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", bodyHash)

	host := req.URL.Host
	ct := req.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	canonicalHeaders := "content-type:" + ct + "\n" +
		"host:" + host + "\n" +
		"x-amz-content-sha256:" + bodyHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"

	uri := req.URL.EscapedPath()
	if uri == "" {
		uri = "/"
	}

	canonical := strings.Join([]string{
		req.Method, uri, req.URL.RawQuery,
		canonicalHeaders, signedHeaders, bodyHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" +
		hex.EncodeToString(s3SHA256([]byte(canonical)))

	sigKey := s3DeriveKey(secret, dateStamp, region, "s3")
	sig := hex.EncodeToString(s3HMAC(sigKey, []byte(toSign)))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+keyID+"/"+scope+
			", SignedHeaders="+signedHeaders+
			", Signature="+sig)
}

func s3DeriveKey(secret, date, region, service string) []byte {
	k := s3HMAC([]byte("AWS4"+secret), []byte(date))
	k = s3HMAC(k, []byte(region))
	k = s3HMAC(k, []byte(service))
	return s3HMAC(k, []byte("aws4_request"))
}

func s3HMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func s3SHA256(data []byte) []byte { h := sha256.Sum256(data); return h[:] }
