package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/session"
)

const folderOrderKey = "folder_order"

// decode reads a small JSON request body into v, answering 400 itself when it cannot.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return false
	}
	return true
}

// move answers POST /api/move {"folder", "uids", "to"} or {"folder", "uids", "role"}: it moves
// the messages to a folder by name, or to the account's Archive, Trash, Junk or Inbox, and
// returns where they went with their new UIDs, which an undo sends back.
func (a *apiHandler) move(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Folder string     `json:"folder"`
		UIDs   []imap.UID `json:"uids"`
		To     string     `json:"to"`
		Role   mail.Role  `json:"role"`
	}
	roles := []mail.Role{mail.RoleInbox, mail.RoleArchive, mail.RoleTrash, mail.RoleJunk}
	if !decode(w, r, &req) || req.Folder == "" || len(req.UIDs) == 0 ||
		(req.To == "") == (req.Role == "") || (req.Role != "" && !slices.Contains(roles, req.Role)) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var moved mail.Moved
	if a.withMail(w, r, s, func(c *mail.Conn) error {
		dest := req.To
		if req.Role != "" {
			var err error
			if dest, err = c.RoleFolder(req.Role); err != nil {
				return err
			}
		}
		var err error
		moved, err = c.Move(req.Folder, req.UIDs, dest)
		return err
	}) {
		writeJSON(w, http.StatusOK, moved)
	}
}

// expunge answers POST /api/expunge {"folder", "uids"}: delete for good, as "Delete" does inside
// Trash after the person confirms.
func (a *apiHandler) expunge(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Folder string     `json:"folder"`
		UIDs   []imap.UID `json:"uids"`
	}
	if !decode(w, r, &req) || req.Folder == "" || len(req.UIDs) == 0 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if a.withMail(w, r, s, func(c *mail.Conn) error { return c.Expunge(req.Folder, req.UIDs) }) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// flags answers POST /api/flags {"folder", "uids", "flag": "seen"|"flagged", "on"}.
func (a *apiHandler) flags(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Folder string     `json:"folder"`
		UIDs   []imap.UID `json:"uids"`
		Flag   string     `json:"flag"`
		On     bool       `json:"on"`
	}
	flag := map[string]imap.Flag{"seen": imap.FlagSeen, "flagged": imap.FlagFlagged}
	if !decode(w, r, &req) || req.Folder == "" || len(req.UIDs) == 0 || flag[req.Flag] == "" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if a.withMail(w, r, s, func(c *mail.Conn) error { return c.SetFlag(req.Folder, req.UIDs, flag[req.Flag], req.On) }) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// validLabel is a folder name part a person may type: not empty, no control characters, and
// none of the characters IMAP servers use to separate or match folder names.
func validLabel(label string) bool {
	return strings.TrimSpace(label) == label && label != "" && len(label) <= 200 &&
		!strings.ContainsAny(label, "/.\\%*\"") && !strings.ContainsFunc(label, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// createFolder answers POST /api/folders {"parent", "label"}.
func (a *apiHandler) createFolder(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Parent string `json:"parent"`
		Label  string `json:"label"`
	}
	if !decode(w, r, &req) || !validLabel(req.Label) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var name string
	if a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		name, err = c.CreateFolder(req.Parent, req.Label)
		return err
	}) {
		writeJSON(w, http.StatusOK, map[string]string{"name": name})
	}
}

// renameFolder answers POST /api/folders/rename {"folder", "parent", "label"}: a new label, a new
// parent (nesting), or both.
func (a *apiHandler) renameFolder(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Folder string `json:"folder"`
		Parent string `json:"parent"`
		Label  string `json:"label"`
	}
	if !decode(w, r, &req) || req.Folder == "" || !validLabel(req.Label) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var name string
	if !a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		name, err = c.RenameFolder(req.Folder, req.Parent, req.Label)
		return err
	}) {
		return
	}
	// The stored order names folders; keep it pointing at the renamed folder and its children.
	a.renameInOrder(r, s, req.Folder, name)
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (a *apiHandler) renameInOrder(r *http.Request, s *session.Session, from, to string) {
	var order []string
	if a.Store.Setting(r.Context(), s.Account, folderOrderKey, &order) != nil || len(order) == 0 {
		return
	}
	for i, name := range order {
		switch {
		case name == from:
			order[i] = to
		case strings.HasPrefix(name, from) && len(name) > len(from) && strings.ContainsRune("/.", rune(name[len(from)])):
			// A child: the character after the prefix is the delimiter, which labels may not
			// contain (validLabel).
			order[i] = to + name[len(from):]
		}
	}
	a.Store.SetSetting(r.Context(), s.Account, folderOrderKey, order)
}

// deleteFolder answers POST /api/folders/delete {"folder"}: the folder and its subfolders go, with
// the mail in them. The browser asks the person first.
func (a *apiHandler) deleteFolder(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Folder string `json:"folder"`
	}
	if !decode(w, r, &req) || req.Folder == "" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if a.withMail(w, r, s, func(c *mail.Conn) error { return c.DeleteFolder(req.Folder) }) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// folderOrder answers PUT /api/folder-order {"order": [full names]}: the arrangement the person
// made by dragging.
func (a *apiHandler) folderOrder(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Order []string `json:"order"`
	}
	if !decode(w, r, &req) || len(req.Order) > 5000 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if err := a.Store.SetSetting(r.Context(), s.Account, folderOrderKey, req.Order); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
