package api

import (
	"encoding/json"
	"log"
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
	if !decode(w, r, &req) {
		return
	}
	// A folder moved under a new parent keeps the label it has, even one another mail app gave
	// it that Postbox would not accept typed.
	keeping := req.Label != "" && strings.HasSuffix(req.Folder, req.Label)
	if req.Folder == "" || (!keeping && !validLabel(req.Label)) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var renamed mail.Renamed
	if !a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		renamed, err = c.RenameFolder(req.Folder, req.Parent, req.Label)
		return err
	}) {
		return
	}
	// The stored order names folders; keep it pointing at the renamed folder and its children.
	a.updateOrder(r, s, func(order []string) []string { return renameInOrder(order, req.Folder, renamed.Name, renamed.Delim) })
	writeJSON(w, http.StatusOK, map[string]string{"name": renamed.Name, "delimiter": renamed.Delim})
}

// updateOrder applies change to the account's stored folder order, if it has one. The order is
// a convenience: failing to keep it in step is logged, not reported.
func (a *apiHandler) updateOrder(r *http.Request, s *session.Session, change func([]string) []string) {
	var order []string
	if err := a.Store.Setting(r.Context(), s.Account, folderOrderKey, &order); err != nil || len(order) == 0 {
		return
	}
	if err := a.Store.SetSetting(r.Context(), s.Account, folderOrderKey, change(order)); err != nil {
		log.Printf("folder order: %v", err)
	}
}

func renameInOrder(order []string, from, to, delim string) []string {
	for i, name := range order {
		if mail.Within(name, from, delim) {
			order[i] = to + strings.TrimPrefix(name, from)
		}
	}
	return order
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
	var delim string
	if !a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		delim, err = c.DeleteFolder(req.Folder)
		return err
	}) {
		return
	}
	// Forget the deleted folders' places, so a new folder of the same name starts afresh.
	a.updateOrder(r, s, func(order []string) []string {
		return slices.DeleteFunc(order, func(name string) bool { return mail.Within(name, req.Folder, delim) })
	})
	w.WriteHeader(http.StatusNoContent)
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
