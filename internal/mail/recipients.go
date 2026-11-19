package mail

import (
	"github.com/emersion/go-imap/v2"
)

// SentRecipients calls found with every To, Cc and Bcc address of every message in the Sent
// folder, one message at a time. A server with no Sent folder, or one that will not open it, has
// none. It never creates the folder.
func (c *Conn) SentRecipients(found func(imap.Address, *imap.Envelope)) error {
	folder, err := c.SentFolder()
	if err != nil || folder == "" {
		return err
	}
	box, err := c.Client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		if isClientNo(err) {
			return nil
		}
		return err
	}
	if box.NumMessages == 0 {
		return nil
	}
	var all imap.SeqSet
	all.AddRange(1, 0)
	fetch := c.Fetch(all, &imap.FetchOptions{Envelope: true})
	for m := fetch.Next(); m != nil; m = fetch.Next() {
		data, err := m.Collect()
		if err != nil {
			fetch.Close()
			return err
		}
		if data.Envelope == nil {
			continue
		}
		for _, list := range [][]imap.Address{data.Envelope.To, data.Envelope.Cc, data.Envelope.Bcc} {
			for _, a := range list {
				if !a.IsGroupStart() && !a.IsGroupEnd() && a.Addr() != "" {
					found(a, data.Envelope)
				}
			}
		}
	}
	return fetch.Close()
}
