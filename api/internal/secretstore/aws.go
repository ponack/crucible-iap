// SPDX-License-Identifier: AGPL-3.0-or-later
package secretstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AWSConfig holds the credentials and target secrets for AWS Secrets Manager.
type AWSConfig struct {
	Region string `json:"region"`
	// Static credentials (optional — falls back to AWS_ACCESS_KEY_ID /
	// AWS_SECRET_ACCESS_KEY environment variables when omitted).
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	// SecretNames is the list of secret ARNs or names to fetch.
	// For JSON-valued secrets, each key/value pair is expanded into a separate
	// env var. For plain-string secrets the last path component of the name
	// becomes the env var key (normalized to UPPER_SNAKE_CASE).
	SecretNames []string `json:"secret_names"`
}

// AWSProvider implements Provider for AWS Secrets Manager (no external SDK —
// authentication uses a built-in AWS Signature Version 4 implementation).
type AWSProvider struct {
	cfg    AWSConfig
	client *http.Client
}

func (p *AWSProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *AWSProvider) FetchSecrets(ctx context.Context) (map[string]string, error) {
	if p.cfg.Region == "" {
		return nil, fmt.Errorf("aws_sm: region is required")
	}
	if len(p.cfg.SecretNames) == 0 {
		return nil, fmt.Errorf("aws_sm: secret_names must not be empty")
	}

	accessKeyID := p.cfg.AccessKeyID
	secretAccessKey := p.cfg.SecretAccessKey
	if accessKeyID == "" {
		accessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if secretAccessKey == "" {
		secretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if accessKeyID == "" || secretAccessKey == "" {
		return nil, fmt.Errorf("aws_sm: credentials required — set access_key_id/secret_access_key or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY environment variables")
	}

	endpoint := "https://secretsmanager." + p.cfg.Region + ".amazonaws.com"
	result := make(map[string]string)

	for _, name := range p.cfg.SecretNames {
		secretStr, err := p.getSecretValue(ctx, endpoint, accessKeyID, secretAccessKey, name)
		if err != nil {
			return nil, fmt.Errorf("aws_sm: get %q: %w", name, err)
		}

		// If JSON object, expand each key as a separate env var.
		var jsonObj map[string]string
		if err := json.Unmarshal([]byte(secretStr), &jsonObj); err == nil {
			for k, v := range jsonObj {
				result[normalize(k)] = v
			}
		} else {
			result[normalize(filepath.Base(name))] = secretStr
		}
	}

	return result, nil
}

func (p *AWSProvider) getSecretValue(ctx context.Context, endpoint, keyID, keySecret, secretName string) (string, error) {
	body, _ := json.Marshal(map[string]string{"SecretId": secretName})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	awsSignRequest(req, body, keyID, keySecret, p.cfg.Region, "secretsmanager")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Message != "" {
			return "", fmt.Errorf("status %d: %s", resp.StatusCode, apiErr.Message)
		}
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var out struct {
		SecretString string `json:"SecretString"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return out.SecretString, nil
}

// ── AWS Signature Version 4 ───────────────────────────────────────────────────

// awsSignRequest adds AWS Signature Version 4 authentication headers to req.
func awsSignRequest(req *http.Request, body []byte, keyID, keySecret, region, service string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	bodyHash := hex.EncodeToString(sha256Sum(body))
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", bodyHash)

	host := req.URL.Host

	// Canonical headers must be in lexicographic order.
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

// normalize converts a name to UPPER_SNAKE_CASE for use as an env var key.
func normalize(s string) string {
	s = strings.ToUpper(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
