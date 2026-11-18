//go:build citest

package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// testRoutes adds the endpoints the browser tests need and a release must never have. Only a
// binary built with `-tags citest` contains this file.
func testRoutes(mux *http.ServeMux, a *apiHandler) {
	// POST {"seconds": n} moves Postbox's clock forward, for session expiry tests.
	mux.HandleFunc("POST /api/test/clock", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Seconds int64 `json:"seconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Seconds <= 0 {
			writeError(w, http.StatusBadRequest, errBadRequest)
			return
		}
		a.Clock.Advance(time.Duration(req.Seconds) * time.Second)
		w.WriteHeader(http.StatusNoContent)
	})
}
