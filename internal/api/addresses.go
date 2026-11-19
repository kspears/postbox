package api

import (
	"net/http"
	"strings"

	"github.com/kspears/postbox/internal/session"
)

// maxAddressQuery caps the text matched against collected addresses.
const maxAddressQuery = 200

// addresses answers GET /api/addresses?q=: the collected addresses whose name or address starts
// with q, for the recipient fields' autocomplete.
func (a *apiHandler) addresses(w http.ResponseWriter, r *http.Request, s *session.Session) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > maxAddressQuery {
		q = strings.TrimSpace(strings.ToValidUTF8(q[:maxAddressQuery], ""))
	}
	found, err := a.Addresses.Match(r.Context(), s.Account, q)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": found})
}
