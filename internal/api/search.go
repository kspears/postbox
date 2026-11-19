package api

import (
	"net/http"
	"strconv"

	"github.com/kspears/postbox/internal/session"
)

// maxSearchQuery caps the text searched for.
const maxSearchQuery = 500

// search answers GET /api/search?q=&folder=[&offset=]: a page of the messages matching q,
// newest first, searched from folder (which decides whether Junk and Trash are included).
func (a *apiHandler) search(w http.ResponseWriter, r *http.Request, s *session.Session) {
	q := r.URL.Query()
	query, folder := q.Get("q"), q.Get("folder")
	offset, err := strconv.Atoi(q.Get("offset"))
	if (err != nil && q.Has("offset")) || offset < 0 || folder == "" || len(query) > maxSearchQuery {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	results, err := a.Mail.Search(r.Context(), a.account(r, s), folder, query, offset)
	if a.answerMailError(w, r, s, err) {
		writeJSON(w, http.StatusOK, results)
	}
}
