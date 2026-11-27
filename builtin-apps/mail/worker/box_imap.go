package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// imapBox is the person's inbox on a mail server over IMAP — the
// organization's or Gmail's; to send, SMTP alone.
type imapBox struct {
	c      *imapClient
	server mailServer
}

const headerFields = "(UID FLAGS RFC822.SIZE INTERNALDATE BODY.PEEK[HEADER.FIELDS (FROM SUBJECT DATE CONTENT-TYPE)])"

func (box imapBox) close() {
	if box.c != nil {
		box.c.logout()
	}
}

func summaries(items []map[string]value) []summary {
	out := []summary{}
	for _, item := range items {
		uid, _ := strconv.ParseInt(item["UID"].text, 10, 64)
		if uid == 0 {
			continue
		}
		header := readHeader(item["BODY"].text)
		internal, _ := time.Parse("02-Jan-2006 15:04:05 -0700", item["INTERNALDATE"].text)
		size, _ := strconv.Atoi(item["RFC822.SIZE"].text)
		media, _, _ := strings.Cut(strings.ToLower(header.Get("Content-Type")), ";")
		out = append(out, summary{
			ID: strconv.FormatInt(uid, 10), order: uid, From: first(people(header, "From")), Subject: headerText(header.Get("Subject")),
			Date: dateOf(header, internal), Seen: hasFlag(flagsOf(item["FLAGS"]), `\Seen`), Attachment: strings.TrimSpace(media) == "multipart/mixed", Size: size,
		})
	}
	sort.Slice(out, func(left, right int) bool { return out[left].order > out[right].order })
	return out
}

// recent lists the newest messages of the inbox.
func (c *imapClient) recent(count int) ([]summary, error) {
	if c.exists == 0 {
		return []summary{}, nil
	}
	start := max(c.exists-count+1, 1)
	items, err := c.fetch(fmt.Sprintf("FETCH %d:%d %s", start, c.exists, headerFields))
	if err != nil {
		return nil, errServer
	}
	return summaries(items), nil
}

// next is the inbox's next UID, when the server didn't say.
func (c *imapClient) next() int64 {
	if c.uidNext > 0 || c.exists == 0 {
		return max(c.uidNext, 1)
	}
	items, err := c.fetch("FETCH * (UID)")
	if err != nil || len(items) == 0 {
		return 1
	}
	uid, _ := strconv.ParseInt(items[0]["UID"].text, 10, 64)
	return uid + 1
}

func (box imapBox) list(count int) ([]summary, int, seen, error) {
	messages, err := box.c.recent(count)
	return messages, box.c.exists, seen{Validity: box.c.validity, Next: box.c.next()}, err
}

func (box imapBox) read(id string) (opened, bool, error) {
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil || uid < 1 {
		return opened{}, false, errInput
	}
	items, err := box.c.fetch(fmt.Sprintf("UID FETCH %d (UID FLAGS RFC822.SIZE BODY.PEEK[]<0.1048576>)", uid))
	if err != nil || len(items) == 0 {
		return opened{}, false, errInput
	}
	item := items[0]
	size, _ := strconv.Atoi(item["RFC822.SIZE"].text)
	if !hasFlag(flagsOf(item["FLAGS"]), `\Seen`) {
		if _, err := box.c.command(fmt.Sprintf(`UID STORE %d +FLAGS.SILENT (\Seen)`, uid)); err != nil {
			leafen.Log("mail: not marked read: " + err.Error())
		}
	}
	return openMessage([]byte(item["BODY"].text)), size > 1048576, nil
}

func (box imapBox) send(message outgoing, now time.Time) error {
	return sendMail(box.server, message, now)
}

func (box imapBox) since(mark seen) ([]summary, seen, error) {
	next := box.c.next()
	current := seen{Validity: box.c.validity, Next: next}
	if mark.Validity != box.c.validity || mark.Next == 0 || next <= mark.Next {
		return nil, current, nil
	}
	items, err := box.c.fetch(fmt.Sprintf("UID FETCH %d:* %s", mark.Next, headerFields))
	if err != nil {
		return nil, mark, err
	}
	fresh := []summary{}
	for _, message := range summaries(items) {
		if message.order >= mark.Next && !message.Seen {
			fresh = append(fresh, message)
		}
	}
	return fresh, current, nil
}
