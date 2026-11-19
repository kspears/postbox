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
	data, err := c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return "", err
	}
	for _, f := range buildFolders(data) {
		if f.Role == role {
			return f.Name, nil
		}
	}
	names := roleNames[role]
	if len(names) == 0 {
		return "", ErrNoRoleFolder
	}
	if err := c.Create(names[0], nil).Wait(); err != nil {
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

// Expunge deletes uids in folder for good.
func (c *Conn) Expunge(folder string, uids []imap.UID) error {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	set := imap.UIDSetNum(uids...)
	err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close()
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
func (c *Conn) folderTree() ([]Folder, rune, error) {
	data, err := c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return nil, 0, err
	}
	delim := '/'
	for _, d := range data {
		if d.Delim != 0 {
			delim = d.Delim
			break
		}
	}
	return buildFolders(data), delim, nil
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
		name = parent + string(delim) + label
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

// RenameFolder gives folder a new label, or a new parent ("" for the top level), or both, and
// returns its new full name. Its subfolders go with it.
func (c *Conn) RenameFolder(folder, parent, label string) (string, error) {
	folders, delim, err := c.folderTree()
	if err != nil {
		return "", err
	}
	f, ok := folderNamed(folders, folder)
	if !ok {
		return "", ErrNotFound
	}
	if f.Role != "" {
		return "", ErrSpecialFolder
	}
	name := label
	if parent != "" {
		name = parent + string(delim) + label
	}
	if name == folder {
		return name, nil
	}
	// A folder cannot become its own descendant.
	if parent == folder || strings.HasPrefix(parent, folder+string(delim)) {
		return "", ErrIntoItself
	}
	if _, ok := folderNamed(folders, name); ok {
		return "", ErrFolderExists
	}
	if err := c.Rename(folder, name, nil).Wait(); err != nil {
		return "", err
	}
	// Subscriptions follow the name, so the folder keeps showing in other mail apps.
	c.Unsubscribe(folder).Wait()
	c.Subscribe(name).Wait()
	return name, nil
}

// DeleteFolder deletes folder and every folder under it.
func (c *Conn) DeleteFolder(folder string) error {
	folders, delim, err := c.folderTree()
	if err != nil {
		return err
	}
	f, ok := folderNamed(folders, folder)
	if !ok {
		return ErrNotFound
	}
	if f.Role != "" {
		return ErrSpecialFolder
	}
	// Deepest first: a server may refuse to delete a folder that still has children.
	var doomed []string
	for _, g := range folders {
		if g.Name == folder || strings.HasPrefix(g.Name, folder+string(delim)) {
			doomed = append(doomed, g.Name)
		}
	}
	slices.SortFunc(doomed, func(a, b string) int { return len(b) - len(a) })
	if m := c.Mailbox(); m != nil && slices.Contains(doomed, m.Name) {
		if err := c.Unselect().Wait(); err != nil {
			return err
		}
	}
	for _, name := range doomed {
		c.Unsubscribe(name).Wait()
		if err := c.Delete(name).Wait(); err != nil {
			return err
		}
	}
	return nil
}
