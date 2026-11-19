package mail

import (
	"time"

	"github.com/emersion/go-imap/v2"
)

// Recipient is one To, Cc or Bcc address of a message, with the message's date.
type Recipient struct {
	Name  string
	Email string
	Date  time.Time
}

// SentRecipients returns every recipient of every message in the Sent folder. A server with no
// Sent folder, or one that will not open it, has none. It never creates the folder.
func (c *Conn) SentRecipients() ([]Recipient, error) {
	folder, err := c.SentFolder()
	if err != nil || folder == "" {
		return nil, err
	}
	box, err := c.Client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		if isClientNo(err) {
			return nil, nil
		}
		return nil, err
	}
	if box.NumMessages == 0 {
		return nil, nil
	}
	var all imap.SeqSet
	all.AddRange(1, 0)
	msgs, err := c.Fetch(all, &imap.FetchOptions{Envelope: true}).Collect()
	if err != nil {
		return nil, err
	}
	var out []Recipient
	for _, m := range msgs {
		if m.Envelope == nil {
			continue
		}
		for _, list := range [][]imap.Address{m.Envelope.To, m.Envelope.Cc, m.Envelope.Bcc} {
			for _, a := range list {
				if email := a.Addr(); email != "" {
					out = append(out, Recipient{Name: a.Name, Email: email, Date: m.Envelope.Date})
				}
			}
		}
	}
	return out, nil
}
