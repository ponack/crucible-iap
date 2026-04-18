// SPDX-License-Identifier: AGPL-3.0-or-later
package oidcprovider

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers the two OIDC discovery endpoints on e (public, no auth).
func (p *Provider) RegisterRoutes(e *echo.Echo) {
	e.GET("/.well-known/openid-configuration", p.HandleDiscovery)
	e.GET("/.well-known/jwks.json", p.HandleJWKS)
}

// HandleDiscovery serves the OIDC provider metadata document.
func (p *Provider) HandleDiscovery(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"issuer":                                p.issuer,
		"jwks_uri":                              p.issuer + "/.well-known/jwks.json",
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "nbf",
			"stack_id", "stack_slug", "org_id", "run_id", "run_type", "branch", "trigger",
		},
	})
}

// HandleJWKS serves the JSON Web Key Set.
func (p *Provider) HandleJWKS(c echo.Context) error {
	jwks, err := p.JWKS()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate JWKS")
	}
	c.Response().Header().Set(echo.HeaderContentType, "application/json")
	return c.JSONBlob(http.StatusOK, jwks)
}
