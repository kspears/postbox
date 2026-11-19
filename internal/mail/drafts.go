package mail

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/textproto"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2"
)

// Raw returns a whole message as the server stores it.
func (c *Conn) Raw(folder string, uid imap.UID) ([]byte, error) {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	whole := &imap.FetchItemBodySection{Peek: true}
	bufs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{whole}}).Collect()
	if err != nil {
		return nil, err
	}
	if len(bufs) == 0 {
		return nil, ErrNotFound
	}
	return bufs[0].FindBodySection(whole), nil
}

// Part returns one decoded part of a message, for a forwarded attachment.
func (c *Conn) Part(folder string, uid imap.UID, part string) ([]byte, error) {
	var buf bytes.Buffer
	err := c.Attachment(folder, uid, part, func(Attachment) {}, &buf)
	return buf.Bytes(), err
}

// SaveDraft stores msg in the Drafts folder and removes the copy it replaces (replace, when not
// 0). It returns the folder and the new copy's UID.
func (c *Conn) SaveDraft(msg []byte, replace imap.UID) (string, imap.UID, error) {
	drafts, err := c.RoleFolder(RoleDrafts)
	if err != nil {
		return "", 0, err
	}
	uid, err := c.appendMessage(drafts, msg, []imap.Flag{imap.FlagDraft, imap.FlagSeen})
	if err != nil {
		return "", 0, err
	}
	if replace != 0 && replace != uid {
		if err := c.remove(drafts, []imap.UID{replace}); err != nil {
			return drafts, uid, err
		}
	}
	return drafts, uid, nil
}

// DiscardDraft deletes a draft for good. folder is the Drafts folder ("" to look it up).
func (c *Conn) DiscardDraft(folder string, uid imap.UID) error {
	if folder == "" {
		var err error
		if folder, err = c.RoleFolder(RoleDrafts); err != nil {
			return err
		}
	}
	return c.remove(folder, []imap.UID{uid})
}

// FileSent puts a copy of a sent message in the Sent folder, marked read.
func (c *Conn) FileSent(msg []byte) error {
	sent, err := c.RoleFolder(RoleSent)
	if err != nil {
		return err
	}
	_, err = c.appendMessage(sent, msg, []imap.Flag{imap.FlagSeen})
	return err
}

func (c *Conn) appendMessage(folder string, msg []byte, flags []imap.Flag) (imap.UID, error) {
	cmd := c.Append(folder, int64(len(msg)), &imap.AppendOptions{Flags: flags, Time: time.Now()})
	if _, err := io.Copy(cmd, bytes.NewReader(msg)); err != nil {
		cmd.Close()
		return 0, err
	}
	if err := cmd.Close(); err != nil {
		return 0, err
	}
	data, err := cmd.Wait()
	if err != nil {
		return 0, err
	}
	if data.UID != 0 {
		return data.UID, nil
	}
	// Without UIDPLUS the server does not say where the message went; find it by Message-ID.
	return c.findByMessageID(folder, msg)
}

func (c *Conn) findByMessageID(folder string, msg []byte) (imap.UID, error) {
	header, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(msg))).ReadMIMEHeader()
	id := header.Get("Message-Id")
	if err != nil || id == "" {
		return 0, errors.New("the server did not say where it saved the message")
	}
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return 0, err
	}
	found, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: id}}}, nil).Wait()
	if err != nil {
		return 0, err
	}
	uids := found.AllUIDs()
	if len(uids) == 0 {
		return 0, ErrNotFound
	}
	return slices.Max(uids), nil
}
