// SPDX-License-Identifier: AGPL-3.0-or-later
package pagination_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/pagination"
)

func echoCtx(query string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	return e.NewContext(req, httptest.NewRecorder())
}

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		limit  int
		offset int
	}{
		{"defaults", "", 50, 0},
		{"explicit limit", "limit=10", 10, 0},
		{"explicit offset", "offset=20", 50, 20},
		{"both", "limit=25&offset=100", 25, 100},
		{"cap over max", "limit=500", 200, 0},
		{"zero limit ignored", "limit=0", 50, 0},
		{"negative limit ignored", "limit=-5", 50, 0},
		{"negative offset ignored", "offset=-1", 50, 0},
		{"non-numeric limit", "limit=abc", 50, 0},
		{"non-numeric offset", "offset=xyz", 50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pagination.Parse(echoCtx(tt.query))
			if p.Limit != tt.limit {
				t.Errorf("Limit = %d, want %d", p.Limit, tt.limit)
			}
			if p.Offset != tt.offset {
				t.Errorf("Offset = %d, want %d", p.Offset, tt.offset)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	t.Run("nil data becomes empty slice", func(t *testing.T) {
		r := pagination.Wrap[string](nil, pagination.Page{Limit: 10}, 0)
		if r.Data == nil {
			t.Error("expected non-nil slice, got nil")
		}
	})

	t.Run("has_more true when more remain", func(t *testing.T) {
		p := pagination.Page{Limit: 10, Offset: 0}
		r := pagination.Wrap([]string{"a", "b"}, p, 5)
		if !r.Pagination.HasMore {
			t.Error("expected HasMore=true")
		}
	})

	t.Run("has_more false when at end", func(t *testing.T) {
		p := pagination.Page{Limit: 10, Offset: 0}
		r := pagination.Wrap([]string{"a", "b", "c"}, p, 3)
		if r.Pagination.HasMore {
			t.Error("expected HasMore=false")
		}
	})

	t.Run("meta fields correct", func(t *testing.T) {
		p := pagination.Page{Limit: 5, Offset: 10}
		r := pagination.Wrap([]int{1, 2}, p, 20)
		if r.Pagination.Limit != 5 {
			t.Errorf("Limit = %d, want 5", r.Pagination.Limit)
		}
		if r.Pagination.Offset != 10 {
			t.Errorf("Offset = %d, want 10", r.Pagination.Offset)
		}
		if r.Pagination.Total != 20 {
			t.Errorf("Total = %d, want 20", r.Pagination.Total)
		}
	})
}
