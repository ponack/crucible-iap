// SPDX-License-Identifier: AGPL-3.0-or-later
package siem

import (
	"crypto/tls"
	"net/http"
	"time"
)

func newHTTPClient(tlsInsecure bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsInsecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}
}
