// The built-in Mail app (docs/app-platform.md 16): a person's mailbox — on
// their organization's mail server or in Gmail. For the server the
// administrator sets the addresses and lets people give their mail account,
// which each person types into Leafen's own window; Gmail takes the person's
// address and an app password the same way, or a Google sign-in through
// Leafen where the administrator set up a Google client. Leafen fills either into the
// connections — the worker writes placeholders and never sees them. The
// worker lists the inbox, opens a message, sends one the person approved,
// and every five minutes tells them about new mail.
package main

import (
	"encoding/json"
	"fmt"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

const listed = 30

type summary struct {
	ID         string `json:"id"`
	From       person `json:"from"`
	Subject    string `json:"subject"`
	Date       string `json:"date"`
	Seen       bool   `json:"seen"`
	Attachment bool   `json:"attachment"`
	Size       int    `json:"size"`
	// The order within the mailbox: an IMAP UID, Gmail's arrival time.
	order int64
}

// mailbox is where the person's mail is.
type mailbox interface {
	// list is the newest messages, how many there are, and the mark past
	// what was listed.
	list(count int) ([]summary, int, seen, error)
	read(id string) (opened, bool, error)
	send(message outgoing, now time.Time) error
	// since is what came after the mark, and the mark past it.
	since(mark seen) ([]summary, seen, error)
	close()
}

// The newest mail the person was told of, in their own store: the IMAP
// mailbox's UIDVALIDITY and next UID, or Gmail's (validity -1) newest
// arrival time.
type seen struct {
	Validity int64 `json:"validity"`
	Next     int64 `json:"next"`
}

// openBox opens the mailbox the person chose (their store's "account"):
// Gmail through Google's sign-in, Gmail over IMAP with an app password, or
// the organization's server — its inbox, to read (or to mark), or nothing
// yet to send.
func openBox(tenancy, mode string) (mailbox, error) {
	chosen := struct {
		Value *struct {
			Kind string `json:"kind"`
		} `json:"value"`
	}{}
	server := companyServer
	if storage(tenancy, map[string]any{"command": "get", "key": "account"}, &chosen) == nil && chosen.Value != nil {
		switch chosen.Value.Kind {
		case "gmail":
			return gmailBox{}, nil
		case "gmail-imap":
			server = gmailServer
		}
	}
	if mode == "send" {
		return imapBox{server: server}, nil
	}
	client, err := openInbox(server, mode == "mark")
	if err != nil {
		return nil, err
	}
	return imapBox{c: client, server: server}, nil
}

// problem answers with what went wrong, for the screen to explain.
func problem(err error) int32 {
	code := errServer.Error()
	for _, known := range []error{errNotSet, errUnreachable, errLogin, errCredentials, errServer, errInput, errRecipients} {
		if err == known {
			code = known.Error()
		}
	}
	if code == errServer.Error() {
		leafen.Log("mail: " + err.Error())
	}
	_ = leafen.OutputJSON(map[string]string{"error": code})
	return 0
}

func storage(tenancy string, request map[string]any, target any) error {
	reply, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: "POST", Path: "/app-storage", Body: request})
	if err != nil {
		return err
	}
	if reply.Status >= 300 {
		return fmt.Errorf("app storage answered %d", reply.Status)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(reply.Body, target)
}

func readSeen(tenancy string) seen {
	stored := struct {
		Value *seen `json:"value"`
	}{}
	if err := storage(tenancy, map[string]any{"command": "get", "key": "seen"}, &stored); err != nil || stored.Value == nil {
		return seen{}
	}
	return *stored.Value
}

func saveSeen(tenancy string, value seen) {
	if err := storage(tenancy, map[string]any{"command": "set", "key": "seen", "value": value}, nil); err != nil {
		leafen.Log("mail: " + err.Error())
	}
}

//go:wasmexport inbox
func inbox() int32 {
	call, err := leafen.Action(nil)
	if err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	box, err := openBox(call.Tenancy, "list")
	if err != nil {
		return problem(err)
	}
	defer box.close()
	messages, total, mark, err := box.list(listed)
	if err != nil {
		return problem(err)
	}
	// What the person saw here needs no notice.
	saveSeen(call.Tenancy, mark)
	_ = leafen.OutputJSON(map[string]any{"messages": messages, "total": total})
	return 0
}

//go:wasmexport read
func read() int32 {
	input := struct {
		ID string `json:"id"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil || input.ID == "" {
		return problem(errInput)
	}
	box, err := openBox(call.Tenancy, "mark")
	if err != nil {
		return problem(err)
	}
	defer box.close()
	message, truncated, err := box.read(input.ID)
	if err != nil {
		return problem(err)
	}
	_ = leafen.OutputJSON(map[string]any{"id": input.ID, "message": message, "truncated": truncated})
	return 0
}

//go:wasmexport send
func send() int32 {
	input := outgoing{}
	call, err := leafen.Action(&input)
	if err != nil {
		return problem(errInput)
	}
	box, err := openBox(call.Tenancy, "send")
	if err != nil {
		return problem(err)
	}
	defer box.close()
	if err := box.send(input, time.Now()); err != nil {
		return problem(err)
	}
	_ = leafen.OutputJSON(map[string]bool{"sent": true})
	return 0
}

// check runs every five minutes for each person and tells them of mail
// that came since they last looked or were told.
//
//go:wasmexport check
func check() int32 {
	call := leafen.ScheduleCall{}
	if err := leafen.InputJSON(&call); err != nil || call.Person == "" {
		return 0
	}
	box, err := openBox(call.Tenancy, "list")
	if err != nil {
		// No account given, or the server can't be reached: nothing to tell.
		return 0
	}
	defer box.close()
	known := readSeen(call.Tenancy)
	fresh, mark, err := box.since(known)
	if err != nil {
		return 0
	}
	if mark != known {
		saveSeen(call.Tenancy, mark)
	}
	if len(fresh) == 0 {
		return 0
	}
	// The notice carries the app's name already.
	text := fmt.Sprintf("새 메일 %d통", len(fresh))
	if len(fresh) == 1 {
		sender := fresh[0].From.Name
		if sender == "" {
			sender = fresh[0].From.Address
		}
		text = sender + ": " + fresh[0].Subject
	}
	if err := leafen.Notify(leafen.NoticeRequest{Person: call.Person, Tenancy: call.Tenancy, Text: text}); err != nil {
		leafen.Log("mail: notice not sent: " + err.Error())
	}
	return 0
}

func main() {}
