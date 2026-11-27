package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"mime"
	"net/mail"
	"strings"
	"time"
)

// Sending (RFC 5321 over implicit TLS, AUTH LOGIN): Leafen fills the
// person's address and login; the message is plain UTF-8 text in base64, so
// nothing the person typed reaches the server as a command. How many people
// one message reaches and how long it is are the mail server's to bound (and
// the input Leafen passes an action): a recipient the server won't take is
// refused with its own reason.

const writeMax = 48 * 1024

type outgoing struct {
	To        string `json:"to"`
	Cc        string `json:"cc"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	InReplyTo string `json:"inReplyTo"`
}

// check refuses what can't go out: no recipient, or a header that would
// carry a line break or a credential placeholder.
func (o outgoing) check() ([]*mail.Address, []*mail.Address, error) {
	for _, field := range []string{o.To, o.Cc, o.Subject, o.InReplyTo} {
		if strings.ContainsAny(field, "\r\n") || strings.Contains(field, "{{") {
			return nil, nil, errInput
		}
	}
	parse := func(list string) ([]*mail.Address, error) {
		if strings.TrimSpace(list) == "" {
			return nil, nil
		}
		return mail.ParseAddressList(list)
	}
	to, err := parse(o.To)
	if err != nil || len(to) == 0 {
		return nil, nil, errInput
	}
	cc, err := parse(o.Cc)
	if err != nil {
		return nil, nil, errInput
	}
	if o.InReplyTo != "" && (!strings.HasPrefix(o.InReplyTo, "<") || !strings.HasSuffix(o.InReplyTo, ">") || len(o.InReplyTo) > 300) {
		return nil, nil, errInput
	}
	return to, cc, nil
}

func addresses(list []*mail.Address) []string {
	out := []string{}
	for _, address := range list {
		out = append(out, address.String())
	}
	return out
}

// folded is a header of parts joined by sep, its lines folded before 78
// characters where a part allows (RFC 5322 2.2.3), so a long list of
// recipients or a long subject stays within a line's 998.
func folded(name string, parts []string, sep string) string {
	var header strings.Builder
	header.WriteString(name + ": ")
	line := len(name) + 2
	for index, part := range parts {
		if index > 0 {
			if line+len(sep)+len(part) > 78 {
				header.WriteString(strings.TrimRight(sep, " ") + "\r\n ")
				line = 1
			} else {
				header.WriteString(sep)
				line += len(sep)
			}
		}
		header.WriteString(part)
		line += len(part)
	}
	return header.String()
}

// compose is the message: its headers — From only when given, as Leafen
// fills it for SMTP — and the body as base64 lines.
func (o outgoing) compose(from string, to, cc []*mail.Address, now time.Time) (string, string) {
	random := make([]byte, 12)
	_, _ = rand.Read(random)
	headers := []string{}
	if from != "" {
		headers = append(headers, "From: "+from)
	}
	headers = append(headers, folded("To", addresses(to), ", "))
	if len(cc) > 0 {
		headers = append(headers, folded("Cc", addresses(cc), ", "))
	}
	headers = append(headers,
		folded("Subject", strings.Split(mime.BEncoding.Encode("utf-8", o.Subject), " "), " "),
		"Date: "+now.Format(time.RFC1123Z),
		"Message-ID: <"+hex.EncodeToString(random)+"@mail.leafen>",
	)
	if o.InReplyTo != "" {
		headers = append(headers, "In-Reply-To: "+o.InReplyTo, "References: "+o.InReplyTo)
	}
	headers = append(headers, "MIME-Version: 1.0", "Content-Type: text/plain; charset=utf-8", "Content-Transfer-Encoding: base64", "", "")
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(o.Body, "\r\n", "\n")))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		lines.WriteString(encoded + "\r\n")
	}
	return strings.Join(headers, "\r\n"), lines.String()
}

// message is the headers (with the person's address left for Leafen) and
// the body for SMTP's DATA, each a separate write, the last ending it.
func (o outgoing) message(server mailServer, to, cc []*mail.Address, now time.Time) (string, []string) {
	headers, text := o.compose(server.field("address", ""), to, cc, now)
	chunks := []string{}
	for len(text) > writeMax {
		cut := strings.LastIndex(text[:writeMax], "\r\n") + 2
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	chunks = append(chunks, text+".\r\n")
	return headers, chunks
}

func sendMail(server mailServer, o outgoing, now time.Time) error {
	to, cc, err := o.check()
	if err != nil {
		return err
	}
	connection, err := dial(server.smtp)
	if err != nil {
		return err
	}
	defer connection.Close()
	r := &reader{w: connection}
	expect := func(line string, wanted ...int) (int, error) {
		if line != "" {
			if err := connection.Write([]byte(line + "\r\n")); err != nil {
				return 0, failure(err)
			}
		}
		code, _, err := smtpReply(r)
		if err != nil {
			return 0, errServer
		}
		for _, want := range wanted {
			if code == want {
				return code, nil
			}
		}
		return code, errServer
	}
	if _, err := expect("", 220); err != nil {
		return err
	}
	if _, err := expect("EHLO mail.leafen", 250); err != nil {
		return err
	}
	if _, err := expect("AUTH LOGIN", 334); err != nil {
		return err
	}
	if _, err := expect(server.field(server.user, "base64"), 334); err != nil {
		return err
	}
	if code, err := expect(server.field("password", "base64"), 235); err != nil {
		if code == 535 || code == 534 || code == 454 {
			return errLogin
		}
		return err
	}
	if _, err := expect("MAIL FROM:<"+server.field("address", "")+">", 250); err != nil {
		return err
	}
	for _, address := range append(append([]*mail.Address{}, to...), cc...) {
		if code, err := expect("RCPT TO:<"+address.Address+">", 250, 251); err != nil {
			// The server won't take this recipient, or this many.
			if code >= 400 {
				return errRecipients
			}
			return err
		}
	}
	if _, err := expect("DATA", 354); err != nil {
		return err
	}
	headers, chunks := o.message(server, to, cc, now)
	if err := connection.Write([]byte(headers)); err != nil {
		return failure(err)
	}
	for index, chunk := range chunks {
		if index < len(chunks)-1 {
			if err := connection.Write([]byte(chunk)); err != nil {
				return failure(err)
			}
			continue
		}
		if _, err := expect(strings.TrimSuffix(chunk, "\r\n"), 250); err != nil {
			return err
		}
	}
	_, _ = expect("QUIT", 221)
	return nil
}
