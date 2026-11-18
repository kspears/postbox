package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const checkFolder = "mailbench-check"

// check runs each IMAP command Postbox relies on against the server and prints one line per
// command. A failure does not stop the run: the point is the whole list.
func check(t target) error {
	failed := 0
	report := func(name string, err error, detail string) {
		status := "ok  "
		if err != nil {
			status, detail = "FAIL", err.Error()
			failed++
		}
		fmt.Printf("%s  %-28s %s\n", status, name, detail)
	}

	exists := make(chan uint32, 16)
	c, err := t.dial(&imapclient.UnilateralDataHandler{
		Mailbox: func(data *imapclient.UnilateralDataMailbox) {
			if data.NumMessages != nil {
				exists <- *data.NumMessages
			}
		},
	})
	if err != nil {
		return err
	}
	defer c.Close()

	id, err := c.ID(&imap.IDData{Name: "postbox-mailbench"}).Wait()
	report("ID before LOGIN", err, fmt.Sprintf("server is %q", nameOf(id)))

	if err := c.Login(t.user, t.password).Wait(); err != nil {
		return fmt.Errorf("login as %s: %w", t.user, err)
	}
	var missing []string
	for _, want := range []imap.Cap{"IDLE", "MOVE", "SPECIAL-USE", "THREAD=REFS", "THREAD=REFERENCES", "SORT", "ID", "LIST-STATUS", "UIDPLUS", "ESEARCH", "CONDSTORE", "LITERAL+", "PREVIEW"} {
		if !c.Caps().Has(want) {
			missing = append(missing, string(want))
		}
	}
	report("capabilities", nil, "not advertised: "+orNone(missing))

	boxes, err := c.List("", "*", &imap.ListOptions{
		ReturnSpecialUse: true,
		ReturnStatus:     &imap.StatusOptions{NumMessages: true, NumUnseen: true},
	}).Collect()
	var special []string
	withStatus := 0
	for _, box := range boxes {
		for _, attr := range box.Attrs {
			if slices.Contains([]imap.MailboxAttr{imap.MailboxAttrArchive, imap.MailboxAttrDrafts, imap.MailboxAttrJunk, imap.MailboxAttrSent, imap.MailboxAttrTrash}, attr) {
				special = append(special, string(attr))
			}
		}
		if box.Status != nil && box.Status.NumMessages != nil && box.Status.NumUnseen != nil {
			withStatus++
		}
	}
	if err == nil && (len(special) != 5 || withStatus != len(boxes)) {
		err = fmt.Errorf("special-use %v, status on %d of %d folders", special, withStatus, len(boxes))
	}
	report("LIST special-use and status", err, fmt.Sprintf("%d folders, %d special, unread counts on all", len(boxes), len(special)))

	// A scratch folder with a three-message conversation; left-overs from a failed run go first.
	c.Delete(checkFolder).Wait()
	if err := c.Create(checkFolder, nil).Wait(); err != nil {
		return fmt.Errorf("create %s: %w", checkFolder, err)
	}
	defer func() { c.Delete(checkFolder).Wait() }()

	var uids []imap.UID
	refs := ""
	for i := 1; i <= 3; i++ {
		msgID := fmt.Sprintf("<check-%d@mailbench.test>", i)
		msg := fmt.Sprintf("From: a@example.org\r\nTo: %s\r\nSubject: check\r\nDate: %s\r\nMessage-ID: %s\r\n", t.user, time.Now().Format(time.RFC1123Z), msgID)
		if refs != "" {
			msg += "References: " + refs + "\r\n"
		}
		msg += "\r\nxylophone quartz " + fmt.Sprint(i) + "\r\n"
		refs = strings.TrimSpace(refs + " " + msgID)
		// A minute apart: arrival time has one-second resolution, and the sort check needs an order.
		data, appendErr := appendMessage(c, checkFolder, []byte(msg), time.Now().Add(time.Duration(i-4)*time.Minute))
		if appendErr != nil {
			err = appendErr
			break
		}
		uids = append(uids, data.UID)
	}
	report("APPEND returns the new UID", err, fmt.Sprint(uids))
	if err != nil || len(uids) != 3 || uids[0] == 0 {
		return errors.New("cannot continue without the appended messages")
	}

	if _, err := c.Select(checkFolder, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", checkFolder, err)
	}
	all := &imap.SearchCriteria{}

	threads, err := c.UIDThread(&imapclient.ThreadOptions{Algorithm: "REFS", SearchCriteria: all}).Wait()
	if err == nil && (len(threads) != 1 || countThread(threads[0]) != 3) {
		err = fmt.Errorf("want one thread of 3, got %+v", threads)
	}
	report("UID THREAD REFS", err, "one conversation of three")

	threads, err = c.UIDThread(&imapclient.ThreadOptions{Algorithm: imap.ThreadReferences, SearchCriteria: all}).Wait()
	if err == nil && (len(threads) != 1 || countThread(threads[0]) != 3) {
		err = fmt.Errorf("want one thread of 3, got %+v", threads)
	}
	report("UID THREAD REFERENCES", err, "one conversation of three")

	sorted, err := c.UIDSort(&imapclient.SortOptions{
		SearchCriteria: all,
		SortCriteria:   []imapclient.SortCriterion{{Key: imapclient.SortKeyArrival, Reverse: true}},
	}).Wait()
	if err == nil && (len(sorted) != 3 || imap.UID(sorted[0]) != uids[2]) {
		err = fmt.Errorf("want newest first, got %v", sorted)
	}
	report("UID SORT (REVERSE ARRIVAL)", err, "newest first")

	whole := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID: true, Envelope: true, Flags: true, InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{whole},
	}).Collect()
	if err == nil {
		switch {
		case len(msgs) != 3:
			err = fmt.Errorf("want 3 messages, got %d", len(msgs))
		case msgs[0].Envelope == nil || msgs[0].Envelope.MessageID != "check-1@mailbench.test":
			err = fmt.Errorf("envelope missing or wrong: %+v", msgs[0].Envelope)
		case len(msgs[0].FindBodySection(whole)) == 0:
			err = errors.New("no body returned")
		}
	}
	report("UID FETCH envelope and body", err, "three messages")

	msgs, err = c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err == nil {
		for _, m := range msgs {
			if slices.Contains(m.Flags, imap.FlagSeen) {
				err = errors.New("BODY.PEEK marked a message read")
			}
		}
	}
	report("BODY.PEEK leaves mail unread", err, "")

	stored, err := c.Store(imap.UIDSetNum(uids[0]), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen, imap.FlagFlagged}}, nil).Collect()
	if err == nil && (len(stored) != 1 || !slices.Contains(stored[0].Flags, imap.FlagFlagged)) {
		err = fmt.Errorf("flags not reported back: %+v", stored)
	}
	report("UID STORE flags", err, `\Seen and \Flagged set`)

	moved, err := c.Move(imap.UIDSetNum(uids[2]), "Trash").Wait()
	var back imap.UIDSet
	if err == nil {
		dest, ok := moved.DestUIDs.(imap.UIDSet)
		if !ok || len(dest) == 0 {
			err = fmt.Errorf("no destination UIDs in %+v", moved)
		}
		back = dest
	}
	report("UID MOVE returns new UIDs", err, fmt.Sprint(back))
	if err == nil {
		// Undo: move it back using the UID the server reported.
		if _, err = c.Select("Trash", nil).Wait(); err == nil {
			_, err = c.Move(back, checkFolder).Wait()
		}
		if _, selectErr := c.Select(checkFolder, nil).Wait(); err == nil {
			err = selectErr
		}
		report("undo a move with those UIDs", err, "")
	}

	found, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: "<check-2@mailbench.test>"}}}, nil).Wait()
	if err == nil && len(found.AllUIDs()) != 1 {
		err = fmt.Errorf("want 1 match, got %v", found.AllUIDs())
	}
	report("UID SEARCH by Message-ID", err, "")

	found, err = c.UIDSearch(&imap.SearchCriteria{Text: []string{"ylophon"}}, nil).Wait()
	if err == nil && len(found.AllUIDs()) != 3 {
		err = fmt.Errorf("want 3 matches for a substring, got %v", found.AllUIDs())
	}
	report("UID SEARCH TEXT, substring", err, "matched the middle of a word")

	// IDLE: a second connection adds a message; the idling one must hear about it.
	for len(exists) > 0 {
		<-exists
	}
	idle, err := c.Idle()
	if err == nil {
		other, loginErr := t.login(nil)
		if loginErr != nil {
			return loginErr
		}
		start := time.Now()
		_, err = appendMessage(other, checkFolder, []byte("Subject: idle\r\n\r\nnew\r\n"), time.Now())
		other.Close()
		if err == nil {
			select {
			case n := <-exists:
				report("IDLE announces new mail", nil, fmt.Sprintf("%d messages, heard after %s", n, time.Since(start).Round(time.Millisecond)))
			case <-time.After(5 * time.Second):
				err = errors.New("nothing heard within 5s")
			}
		}
		if closeErr := idle.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = idle.Wait()
		}
	}
	if err != nil {
		report("IDLE announces new mail", err, "")
	}

	if failed > 0 {
		return fmt.Errorf("%d checks failed", failed)
	}
	return nil
}

func appendMessage(c *imapclient.Client, folder string, msg []byte, when time.Time) (*imap.AppendData, error) {
	cmd := c.Append(folder, int64(len(msg)), &imap.AppendOptions{Time: when})
	if _, err := cmd.Write(msg); err != nil {
		return nil, err
	}
	if err := cmd.Close(); err != nil {
		return nil, err
	}
	return cmd.Wait()
}

func countThread(t imapclient.ThreadData) int {
	n := len(t.Chain)
	for _, sub := range t.SubThreads {
		n += countThread(sub)
	}
	return n
}

func nameOf(id *imap.IDData) string {
	if id == nil {
		return ""
	}
	return id.Name
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}
