package mail

import (
	"cmp"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// Role is what a special folder is for. The empty Role is an ordinary folder.
type Role string

// The roles Postbox gives a place of their own, in the order the folder list shows them.
const (
	RoleInbox   Role = "inbox"
	RoleDrafts  Role = "drafts"
	RoleSent    Role = "sent"
	RoleArchive Role = "archive"
	RoleJunk    Role = "junk"
	RoleTrash   Role = "trash"
)

var roleOrder = []Role{RoleInbox, RoleDrafts, RoleSent, RoleArchive, RoleJunk, RoleTrash}

var roleAttr = map[imap.MailboxAttr]Role{
	imap.MailboxAttrDrafts:  RoleDrafts,
	imap.MailboxAttrSent:    RoleSent,
	imap.MailboxAttrArchive: RoleArchive,
	imap.MailboxAttrJunk:    RoleJunk,
	imap.MailboxAttrTrash:   RoleTrash,
}

// When the server marks no folder for a role, the first existing folder with one of these names
// takes it (spec, "Special folders"). Names compare without regard to case.
var roleNames = map[Role][]string{
	RoleArchive: {"Archive", "Archives"},
	RoleTrash:   {"Trash", "Deleted Items", "Deleted Messages"},
	RoleDrafts:  {"Drafts"},
	RoleSent:    {"Sent", "Sent Items", "Sent Messages"},
	RoleJunk:    {"Junk", "Spam", "Junk E-mail"},
}

// Folder is one mailbox as the folder list shows it.
type Folder struct {
	Name       string `json:"name"`             // full name, used to address the folder
	Label      string `json:"label"`            // the last part of the name
	Parent     string `json:"parent,omitempty"` // full name of the parent; empty at the top level
	Role       Role   `json:"role,omitempty"`
	Unread     uint32 `json:"unread"`
	Total      uint32 `json:"total"`
	Selectable bool   `json:"selectable"` // false for a folder that only holds other folders
}

// Folders lists every folder with its role and counts, special folders first in a fixed order,
// then the rest by name with each folder after its parent.
func (c *Conn) Folders() ([]Folder, error) {
	data, err := c.List("", "*", &imap.ListOptions{
		ReturnSpecialUse: true,
		ReturnStatus:     &imap.StatusOptions{NumMessages: true, NumUnseen: true},
	}).Collect()
	if err != nil {
		return nil, err
	}
	return buildFolders(data), nil
}

func buildFolders(data []*imap.ListData) []Folder {
	folders := make([]Folder, 0, len(data))
	taken := map[Role]bool{}
	for _, d := range data {
		f := Folder{Name: d.Mailbox, Label: d.Mailbox, Selectable: true}
		if d.Delim != 0 {
			if i := strings.LastIndexByte(d.Mailbox, byte(d.Delim)); i >= 0 && d.Delim < 0x80 {
				f.Parent, f.Label = d.Mailbox[:i], d.Mailbox[i+1:]
			}
		}
		for _, attr := range d.Attrs {
			if attr == imap.MailboxAttrNoSelect || attr == imap.MailboxAttrNonExistent {
				f.Selectable = false
			}
			if role, ok := roleAttr[attr]; ok && !taken[role] {
				f.Role, taken[role] = role, true
			}
		}
		if strings.EqualFold(d.Mailbox, "INBOX") {
			f.Role, f.Label = RoleInbox, "Inbox"
		}
		if d.Status != nil {
			f.Unread = deref(d.Status.NumUnseen)
			f.Total = deref(d.Status.NumMessages)
		}
		folders = append(folders, f)
	}

	// Fallback names, only for roles no folder claimed and only at the top level.
	for _, role := range roleOrder[1:] {
		if taken[role] {
			continue
		}
	names:
		for _, name := range roleNames[role] {
			for i := range folders {
				if folders[i].Role == "" && folders[i].Parent == "" && folders[i].Selectable && strings.EqualFold(folders[i].Name, name) {
					folders[i].Role, taken[role] = role, true
					break names
				}
			}
		}
	}

	slices.SortStableFunc(folders, func(a, b Folder) int {
		ra, rb := slices.Index(roleOrder, a.Role), slices.Index(roleOrder, b.Role)
		switch {
		case ra >= 0 && rb >= 0:
			return cmp.Compare(ra, rb)
		case ra >= 0:
			return -1
		case rb >= 0:
			return 1
		}
		// A parent sorts before its children because its name is their prefix.
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return folders
}

func deref(n *uint32) uint32 {
	if n == nil {
		return 0
	}
	return *n
}
