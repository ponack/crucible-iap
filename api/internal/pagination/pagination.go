// SPDX-License-Identifier: AGPL-3.0-or-later
package pagination

import (
	"strconv"

	"github.com/labstack/echo/v4"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// Page holds validated pagination parameters parsed from query string.
type Page struct {
	Limit  int
	Offset int
}

// Parse reads ?limit= and ?offset= from the request, applying defaults and caps.
func Parse(c echo.Context) Page {
	limit := defaultLimit
	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset := 0
	if v := c.QueryParam("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	return Page{Limit: limit, Offset: offset}
}

// Meta is the pagination envelope included in every list response.
type Meta struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	Total   int  `json:"total"`
	HasMore bool `json:"has_more"`
}

// Response wraps a data slice with pagination metadata.
type Response[T any] struct {
	Data       []T  `json:"data"`
	Pagination Meta `json:"pagination"`
}

// Wrap builds a Response from a slice and total count.
func Wrap[T any](data []T, p Page, total int) Response[T] {
	if data == nil {
		data = []T{}
	}
	return Response[T]{
		Data: data,
		Pagination: Meta{
			Limit:   p.Limit,
			Offset:  p.Offset,
			Total:   total,
			HasMore: p.Offset+len(data) < total,
		},
	}
}
