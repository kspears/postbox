package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

var dist = fstest.MapFS{
	"index.html":    {Data: []byte("<!doctype html><title>Postbox</title>")},
	"assets/app.js": {Data: []byte("console.log(1)")},
	"favicon.svg":   {Data: []byte("<svg/>")},
}

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestHealthz(t *testing.T) {
	w := get(t, newHandler(t, dist), "GET", "/healthz")
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("got %d %q, want 200 \"ok\"", w.Code, w.Body.String())
	}
}

func TestFrontend(t *testing.T) {
	h := newHandler(t, dist)
	index := string(dist["index.html"].Data)
	tests := []struct {
		path, body, cache string
	}{
		{"/", index, "no-cache"},
		{"/folder/INBOX", index, "no-cache"},
		{"/assets", index, "no-cache"},
		{"/assets/app.js", "console.log(1)", "public, max-age=31536000, immutable"},
		{"/favicon.svg", "<svg/>", ""},
	}
	for _, tt := range tests {
		w := get(t, h, "GET", tt.path)
		if w.Code != 200 || w.Body.String() != tt.body {
			t.Errorf("%s: got %d %q", tt.path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != tt.cache {
			t.Errorf("%s: Cache-Control %q, want %q", tt.path, got, tt.cache)
		}
	}
}

func TestUnknownAPIPathIsNotTheFrontend(t *testing.T) {
	if w := get(t, newHandler(t, dist), "GET", "/api/nope"); w.Code != 404 {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestMissingAssetIsNotTheFrontend(t *testing.T) {
	if w := get(t, newHandler(t, dist), "GET", "/assets/old-release.js"); w.Code != 404 {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestFrontendRejectsWrites(t *testing.T) {
	h := newHandler(t, dist)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Origin", origin)
	if h.ServeHTTP(w, r); w.Code != 405 {
		t.Errorf("got %d, want 405", w.Code)
	}
}

func TestFrontendNotBuilt(t *testing.T) {
	w := get(t, newHandler(t, fstest.MapFS{".gitkeep": {}}), "GET", "/")
	if w.Code != 503 {
		t.Errorf("got %d, want 503", w.Code)
	}
}
