package mail

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func n(v uint32) *uint32 { return &v }

func list(name string, attrs ...imap.MailboxAttr) *imap.ListData {
	return &imap.ListData{Mailbox: name, Delim: '/', Attrs: attrs, Status: &imap.StatusData{Mailbox: name, NumMessages: n(10), NumUnseen: n(2)}}
}

func TestFoldersOrderAndRoles(t *testing.T) {
	got := buildFolders([]*imap.ListData{
		list("Projects"),
		list("Trash", imap.MailboxAttrTrash),
		list("Projects/Garden"),
		list("INBOX"),
		list("Archive", imap.MailboxAttrArchive),
		list("Sent", imap.MailboxAttrSent),
		list("Drafts", imap.MailboxAttrDrafts),
		list("Junk", imap.MailboxAttrJunk),
		list("Holding", imap.MailboxAttrNoSelect),
	})
	want := []struct {
		name, label, parent string
		role                Role
	}{
		{"INBOX", "Inbox", "", RoleInbox},
		{"Drafts", "Drafts", "", RoleDrafts},
		{"Sent", "Sent", "", RoleSent},
		{"Archive", "Archive", "", RoleArchive},
		{"Junk", "Junk", "", RoleJunk},
		{"Trash", "Trash", "", RoleTrash},
		{"Holding", "Holding", "", ""},
		{"Projects", "Projects", "", ""},
		{"Projects/Garden", "Garden", "Projects", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d folders: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.name || g.Label != w.label || g.Parent != w.parent || g.Role != w.role {
			t.Errorf("%d: got %+v, want %+v", i, g, w)
		}
	}
	if got[0].Unread != 2 || got[0].Total != 10 {
		t.Errorf("counts: %+v", got[0])
	}
	if got[6].Selectable {
		t.Error(`a \Noselect folder is selectable`)
	}
}

func TestFallbackNamesWhenNothingIsMarked(t *testing.T) {
	got := buildFolders([]*imap.ListData{
		list("INBOX"), list("Deleted Items"), list("Sent Messages"), list("spam"), list("Work/Archive"),
	})
	roles := map[string]Role{}
	for _, f := range got {
		roles[f.Name] = f.Role
	}
	if roles["Deleted Items"] != RoleTrash || roles["Sent Messages"] != RoleSent || roles["spam"] != RoleJunk {
		t.Errorf("fallback roles: %v", roles)
	}
	if roles["Work/Archive"] != "" {
		t.Error("a nested folder took the Archive role")
	}
}

func TestMarkedFolderBeatsName(t *testing.T) {
	got := buildFolders([]*imap.ListData{list("Trash"), list("Bin", imap.MailboxAttrTrash)})
	for _, f := range got {
		if f.Name == "Trash" && f.Role != "" {
			t.Error("the name won over the server's marking")
		}
	}
}
