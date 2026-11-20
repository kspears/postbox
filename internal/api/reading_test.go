package api

import (
	"net/http/httptest"
	"testing"
)

func TestFromAppFrame(t *testing.T) {
	tests := []struct {
		name       string
		dest, site string
		want       bool
	}{
		{"no Fetch Metadata", "", "", true},
		{"the app's own iframe", "iframe", "same-origin", true},
		{"top-level navigation", "document", "cross-site", false},
		{"iframe of another site", "iframe", "cross-site", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/api/body", nil)
		if tt.dest != "" {
			r.Header.Set("Sec-Fetch-Dest", tt.dest)
			r.Header.Set("Sec-Fetch-Site", tt.site)
		}
		if got := fromAppFrame(r); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
