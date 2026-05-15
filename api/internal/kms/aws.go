// SPDX-License-Identifier: AGPL-3.0-or-later
package kms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// AWSKMSProvider implements Provider against AWS KMS using a built-in Sig v4
// signer — no AWS SDK dependency. Credentials are loaded from environment
// variables (CRUCIBLE_KMS_AWS_* with AWS_* fallback).
type AWSKMSProvider struct {
	region          string
	keyID           string // KMS key ARN, alias ARN, or `alias/name`
	accessKeyID     string
	secretAccessKey string
	client          *http.Client
}

func newAWSKMSProvider(keyID string) (*AWSKMSProvider, error) {
	region := os.Getenv("CRUCIBLE_KMS_AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		return nil, fmt.Errorf("aws_kms: CRUCIBLE_KMS_AWS_REGION is required")
	}
	if keyID == "" {
		return nil, fmt.Errorf("aws_kms: key id is required")
	}

	accessKeyID := os.Getenv("CRUCIBLE_KMS_AWS_ACCESS_KEY_ID")
	if accessKeyID == "" {
		accessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	secretAccessKey := os.Getenv("CRUCIBLE_KMS_AWS_SECRET_ACCESS_KEY")
	if secretAccessKey == "" {
		secretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	// Both keys must be set together. If neither is set, we leave them blank
	// and let the caller's environment surface the misconfiguration —
	// instance-role / IRSA support is a future enhancement.
	if (accessKeyID == "") != (secretAccessKey == "") {
		return nil, fmt.Errorf("aws_kms: access key and secret must be set together")
	}

	return &AWSKMSProvider{
		region:          region,
		keyID:           keyID,
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		client:          &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (p *AWSKMSProvider) Wrap(ctx context.Context, plaintext []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"KeyId":     p.keyID,
		"Plaintext": base64.StdEncoding.EncodeToString(plaintext),
	})
	var out struct {
		CiphertextBlob string `json:"CiphertextBlob"`
	}
	if err := p.call(ctx, "TrentService.Encrypt", body, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.CiphertextBlob)
}

func (p *AWSKMSProvider) Unwrap(ctx context.Context, ciphertext []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"CiphertextBlob": base64.StdEncoding.EncodeToString(ciphertext),
	})
	var out struct {
		Plaintext string `json:"Plaintext"`
	}
	if err := p.call(ctx, "TrentService.Decrypt", body, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Plaintext)
}

// TestAccess proves the configured credentials can reach KMS and the key is
// usable for both Encrypt and Decrypt by round-tripping a 32-byte canary.
func (p *AWSKMSProvider) TestAccess(ctx context.Context) error {
	canary := []byte("crucible-byok-test-access-canary")
	wrapped, err := p.Wrap(ctx, canary)
	if err != nil {
		return fmt.Errorf("wrap canary: %w", err)
	}
	unwrapped, err := p.Unwrap(ctx, wrapped)
	if err != nil {
		return fmt.Errorf("unwrap canary: %w", err)
	}
	if !bytes.Equal(canary, unwrapped) {
		return fmt.Errorf("kms canary roundtrip mismatch")
	}
	return nil
}

func (p *AWSKMSProvider) call(ctx context.Context, action string, body []byte, out any) error {
	if p.accessKeyID == "" || p.secretAccessKey == "" {
		return fmt.Errorf("aws_kms: credentials required — set CRUCIBLE_KMS_AWS_ACCESS_KEY_ID/SECRET_ACCESS_KEY")
	}
	endpoint := "https://kms." + p.region + ".amazonaws.com/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", action)

	awsSignRequest(req, body, p.accessKeyID, p.secretAccessKey, p.region, "kms")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Message != "" {
			return fmt.Errorf("aws_kms %s: %s (%s)", action, apiErr.Message, apiErr.Type)
		}
		return fmt.Errorf("aws_kms %s: status %d", action, resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("aws_kms %s: decode response: %w", action, err)
	}
	return nil
}

// ── AWS Signature Version 4 ───────────────────────────────────────────────────
// Same implementation as api/internal/secretstore/aws.go — kept here so the kms
// package has no internal cross-dependency.

func awsSignRequest(req *http.Request, body []byte, keyID, keySecret, region, service string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	bodyHash := hex.EncodeToString(sha256Sum(body))
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", bodyHash)

	host := req.URL.Host

	canonicalHeaders := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + host + "\n" +
		"x-amz-content-sha256:" + bodyHash + "\n" +
		"x-amz-date:" + amzDate + "\n" +
		"x-amz-target:" + req.Header.Get("X-Amz-Target") + "\n"
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date;x-amz-target"

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")

	credentialScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		hex.EncodeToString(sha256Sum([]byte(canonicalRequest)))

	signingKey := awsDeriveKey(keySecret, dateStamp, region, service)
	signature := hex.EncodeToString(awsHMAC(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 "+
			"Credential="+keyID+"/"+credentialScope+", "+
			"SignedHeaders="+signedHeaders+", "+
			"Signature="+signature)
}

func awsDeriveKey(secret, date, region, service string) []byte {
	kDate := awsHMAC([]byte("AWS4"+secret), []byte(date))
	kRegion := awsHMAC(kDate, []byte(region))
	kService := awsHMAC(kRegion, []byte(service))
	return awsHMAC(kService, []byte("aws4_request"))
}

func awsHMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
