package mail

import (
	"errors"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// Errors for folder changes the browser explains to the person.
var (
	ErrFolderExists  = errors.New("a folder with that name exists")
	ErrSpecialFolder = errors.New("special folders cannot be renamed, moved or deleted")
	ErrNoRoleFolder  = errors.New("no folder for that role")
	ErrIntoItself    = errors.New("a folder cannot go inside itself")
)

// RoleFolder returns the folder for role, creating it under its usual name when the server has
// none (spec, "Special folders"). Inbox always exists.
func (c *Conn) RoleFolder(role Role) (string, error) {
	if role == RoleInbox {
		return "INBOX", nil
	}
	folders, _, err := c.folderTree()
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		if f.Role == role {
			return f.Name, nil
		}
	}
	names := roleNames[role]
	if len(names) == 0 {
		return "", ErrNoRoleFolder
	}
	// Another tab may create it at the same moment; that is as good.
	if err := c.Create(names[0], nil).Wait(); err != nil && !isAlreadyExists(err) {
		return "", err
	}
	c.Subscribe(names[0]).Wait()
	return names[0], nil
}

// Moved is where messages went: the folder and their new UIDs there, for an undo.
type Moved struct {
	Folder string     `json:"folder"`
	UIDs   []imap.UID `json:"uids"`
}

// Move moves uids from folder to dest.
func (c *Conn) Move(folder string, uids []imap.UID, dest string) (Moved, error) {
	if folder == dest {
		return Moved{Folder: dest, UIDs: uids}, nil
	}
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return Moved{}, err
	}
	data, err := c.Client.Move(imap.UIDSetNum(uids...), dest).Wait()
	if err != nil {
		return Moved{}, err
	}
	moved := Moved{Folder: dest, UIDs: []imap.UID{}}
	if set, ok := data.DestUIDs.(imap.UIDSet); ok {
		if nums, ok := set.Nums(); ok {
			moved.UIDs = nums
		}
	}
	return moved, nil
}

// ErrNotTrash means a permanent delete was asked for outside the Trash folder.
var ErrNotTrash = errors.New("messages are deleted for good only in Trash")

// Expunge deletes uids in folder for good. Only the Trash folder allows it: everywhere else
// Delete moves to Trash, and a mistake must not be able to wipe Inbox.
func (c *Conn) Expunge(folder string, uids []imap.UID) error {
	// Usually one LIST for this folder alone answers it. When the server does not mark the
	// folder \Trash, the whole tree decides, so the name "Trash" counts only when no other
	// folder is marked.
	data, err := c.List("", folder, &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return err
	}
	if len(data) != 1 || !slices.Contains(data[0].Attrs, imap.MailboxAttrTrash) {
		folders, _, err := c.folderTree()
		if err != nil {
			return err
		}
		if f, ok := folderNamed(folders, folder); !ok || f.Role != RoleTrash {
			return ErrNotTrash
		}
	}
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	set := imap.UIDSetNum(uids...)
	err = c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close()
	if err != nil {
		return err
	}
	// UID EXPUNGE removes only these messages, not others another client marked deleted.
	return c.UIDExpunge(set).Close()
}

// SetFlag adds or removes flag on uids in folder.
func (c *Conn) SetFlag(folder string, uids []imap.UID, flag imap.Flag, on bool) error {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	op := imap.StoreFlagsAdd
	if !on {
		op = imap.StoreFlagsDel
	}
	return c.Store(imap.UIDSetNum(uids...), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Close()
}

// folderTree lists every folder with its role, and the hierarchy delimiter.
func (c *Conn) folderTree() ([]Folder, string, error) {
	data, err := c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return nil, "", err
	}
	delim := "/"
	for _, d := range data {
		if d.Delim != 0 {
			delim = string(d.Delim)
			break
		}
	}
	return buildFolders(data), delim, nil
}

// Within reports whether name is folder or one of its subfolders, for the hierarchy delimiter
// delim.
func Within(name, folder, delim string) bool {
	return name == folder || strings.HasPrefix(name, folder+delim)
}

func folderNamed(folders []Folder, name string) (Folder, bool) {
	i := slices.IndexFunc(folders, func(f Folder) bool { return f.Name == name })
	if i < 0 {
		return Folder{}, false
	}
	return folders[i], true
}

// CreateFolder creates a folder called label under parent ("" for the top level) and returns its
// full name.
func (c *Conn) CreateFolder(parent, label string) (string, error) {
	folders, delim, err := c.folderTree()
	if err != nil {
		return "", err
	}
	name := label
	if parent != "" {
		name = parent + delim + label
	}
	if _, ok := folderNamed(folders, name); ok {
		return "", ErrFolderExists
	}
	if err := c.Create(name, nil).Wait(); err != nil {
		return "", err
	}
	c.Subscribe(name).Wait()
	return name, nil
}

// Renamed is a folder's new full name and the server's hierarchy delimiter, with which the
// names of its subfolders follow.
type Renamed struct {
	Name  string
	Delim string
}

// RenameFolder gives folder a new label, or a new parent ("" for the top level), or both. Its
// subfolders go with it.
func (c *Conn) RenameFolder(folder, parent, label string) (Renamed, error) {
	folders, delim, err := c.folderTree()
	if err != nil {
		return Renamed{}, err
	}
	f, ok := folderNamed(folders, folder)
	if !ok {
		return Renamed{}, ErrNotFound
	}
	if f.Role != "" {
		return Renamed{}, ErrSpecialFolder
	}
	name := label
	if parent != "" {
		name = parent + delim + label
	}
	if name == folder {
		return Renamed{name, delim}, nil
	}
	// A folder cannot become its own descendant.
	if Within(parent, folder, delim) {
		return Renamed{}, ErrIntoItself
	}
	if _, ok := folderNamed(folders, name); ok {
		return Renamed{}, ErrFolderExists
	}
	if err := c.Rename(folder, name, nil).Wait(); err != nil {
		return Renamed{}, err
	}
	// Subscriptions follow the names, the subfolders' too, so the folders keep showing in mail
	// apps that list subscribed folders only.
	for _, g := range folders {
		if Within(g.Name, folder, delim) {
			c.Unsubscribe(g.Name).Wait()
			c.Subscribe(name + strings.TrimPrefix(g.Name, folder)).Wait()
		}
	}
	return Renamed{name, delim}, nil
}

// DeleteFolder deletes folder and every folder under it, and returns the server's hierarchy
// delimiter. It refuses when any of them is a special folder.
func (c *Conn) DeleteFolder(folder string) (string, error) {
	folders, delim, err := c.folderTree()
	if err != nil {
		return "", err
	}
	if _, ok := folderNamed(folders, folder); !ok {
		return "", ErrNotFound
	}
	// Deepest first: a server may refuse to delete a folder that still has children.
	var doomed []string
	for _, g := range folders {
		if Within(g.Name, folder, delim) {
			if g.Role != "" {
				return "", ErrSpecialFolder
			}
			doomed = append(doomed, g.Name)
		}
	}
	slices.SortFunc(doomed, func(a, b string) int { return len(b) - len(a) })
	if m := c.Mailbox(); m != nil && slices.Contains(doomed, m.Name) {
		if err := c.Unselect().Wait(); err != nil {
			return "", err
		}
	}
	for _, name := range doomed {
		c.Unsubscribe(name).Wait()
		if err := c.Delete(name).Wait(); err != nil {
			return "", err
		}
	}
	return delim, nil
}

func isAlreadyExists(err error) bool {
	var status *imap.Error
	return errors.As(err, &status) && status.Code == imap.ResponseCodeAlreadyExists
}
