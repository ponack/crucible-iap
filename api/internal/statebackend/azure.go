// SPDX-License-Identifier: AGPL-3.0-or-later
package statebackend

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AzureConfig holds credentials and container info for Azure Blob Storage state.
// State is stored at https://{account}.blob.core.windows.net/{container}/{key_prefix}{stackID}/terraform.tfstate
//
// AccessKey is the base64-encoded storage account key (found in the Azure Portal
// under Storage Account → Access keys).
type AzureConfig struct {
	AccountName string `json:"account_name"`
	AccountKey  string `json:"account_key"` // base64-encoded storage account key
	Container   string `json:"container"`
	KeyPrefix   string `json:"key_prefix,omitempty"`
}

type AzureBackend struct {
	cfg    AzureConfig
	client *http.Client
}

func (b *AzureBackend) httpClient() *http.Client {
	if b.client != nil {
		return b.client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (b *AzureBackend) blobURL(stackID string) string {
	blobName := b.cfg.KeyPrefix + stackID + "/terraform.tfstate"
	return "https://" + b.cfg.AccountName + ".blob.core.windows.net/" +
		b.cfg.Container + "/" + blobName
}

func (b *AzureBackend) GetState(ctx context.Context, stackID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.blobURL(stackID), nil)
	if err != nil {
		return nil, err
	}

	if err := b.signRequest(req, nil); err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("azure get: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (b *AzureBackend) PutState(ctx context.Context, stackID string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, b.blobURL(stackID),
		bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-ms-blob-type", "BlockBlob")

	if err := b.signRequest(req, data); err != nil {
		return err
	}

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("azure put: status %d", resp.StatusCode)
	}
	return nil
}

func (b *AzureBackend) DeleteState(ctx context.Context, stackID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, b.blobURL(stackID), nil)
	if err != nil {
		return err
	}

	if err := b.signRequest(req, nil); err != nil {
		return err
	}

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("azure delete: status %d", resp.StatusCode)
	}
	return nil
}

// signRequest adds Azure Shared Key authentication to the request.
// Implements the Shared Key Lite scheme for Blob service.
func (b *AzureBackend) signRequest(req *http.Request, body []byte) error {
	accountKey, err := base64.StdEncoding.DecodeString(b.cfg.AccountKey)
	if err != nil {
		return fmt.Errorf("azure: decode account key: %w", err)
	}

	now := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("x-ms-date", now)
	req.Header.Set("x-ms-version", "2020-04-08")

	// Canonicalized headers: x-ms-* headers sorted alphabetically.
	xmsHeaders := []string{
		"x-ms-blob-type:" + req.Header.Get("x-ms-blob-type"),
		"x-ms-date:" + now,
		"x-ms-version:2020-04-08",
	}
	// Filter out empty headers.
	var filtered []string
	for _, h := range xmsHeaders {
		if !strings.HasSuffix(h, ":") {
			filtered = append(filtered, h)
		}
	}
	canonicalizedHeaders := strings.Join(filtered, "\n")

	// Canonicalized resource: /{account}/{container}/{blob}
	u := req.URL
	canonicalizedResource := "/" + b.cfg.AccountName + u.Path

	// Content-MD5 and Content-Type
	contentLength := ""
	if body != nil {
		contentLength = fmt.Sprintf("%d", len(body))
	}
	contentType := req.Header.Get("Content-Type")

	// String to sign (Shared Key Lite):
	// VERB\nContent-MD5\nContent-Type\nDate\nCanonicalizedHeaders\nCanonicalizedResource
	stringToSign := strings.Join([]string{
		req.Method,
		"", // Content-MD5
		contentType,
		"", // Date (use x-ms-date instead)
		canonicalizedHeaders,
		canonicalizedResource,
	}, "\n")
	_ = contentLength // used implicitly via body

	mac := hmac.New(sha256.New, accountKey)
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("Authorization", "SharedKeyLite "+b.cfg.AccountName+":"+sig)
	return nil
}
