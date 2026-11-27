package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// wire is a connection to a mail server: a Leafen socket in the app, a plain
// connection in a test.
type wire interface {
	Write(data []byte) error
	Read(max int, wait time.Duration) ([]byte, bool, error)
	Close() error
}

type socket int

func (s socket) Write(data []byte) error { return leafen.SocketWrite(int(s), data) }
func (s socket) Read(max int, wait time.Duration) ([]byte, bool, error) {
	return leafen.SocketRead(int(s), max, wait)
}
func (s socket) Close() error { return leafen.SocketClose(int(s)) }

// What went wrong, as the screen names it.
var (
	errNotSet      = errors.New("settings")
	errUnreachable = errors.New("connect")
	errLogin       = errors.New("login")
	errCredentials = errors.New("credentials")
	errServer      = errors.New("server")
	errInput       = errors.New("input")
	errRecipients  = errors.New("recipients")
)

// mailServer is where a mailbox is reached over IMAP and SMTP: the
// organization's server at the addresses the administrator set, with the
// mail account the person gave; or Gmail at Google's addresses, with the
// address and app password the person gave. Leafen fills the placeholders.
type mailServer struct {
	imap, smtp string // {{setting:KEY}}, or host:port
	credential string // the credential the person gave
	user       string // its field the server takes as the user name
}

var (
	companyServer = mailServer{imap: "{{setting:imap}}", smtp: "{{setting:smtp}}", credential: "mail", user: "user"}
	gmailServer   = mailServer{imap: "imap.gmail.com:993", smtp: "smtp.gmail.com:465", credential: "gmail", user: "address"}
)

// field is the placeholder Leafen fills with one field of the credential.
func (s mailServer) field(name, filter string) string {
	if filter != "" {
		filter = "|" + filter
	}
	return "{{credential:" + s.credential + "." + name + filter + "}}"
}

// dial opens a mail server: over TLS as the app is approved, or plain where
// an entry is approved that way (a test server on the private network).
var dial = func(address string) (wire, error) {
	host, port := address, 0
	if !strings.HasPrefix(address, "{{") {
		if cut := strings.LastIndex(address, ":"); cut > 0 {
			host = address[:cut]
			port, _ = strconv.Atoi(address[cut+1:])
		}
	}
	id, err := leafen.SocketOpen(host, port, true)
	if err == nil {
		return socket(id), nil
	}
	if !strings.Contains(err.Error(), "approved") {
		return nil, errUnreachable
	}
	if id, err = leafen.SocketOpen(host, port, false); err == nil {
		return socket(id), nil
	}
	if strings.Contains(err.Error(), "approved") {
		return nil, errNotSet
	}
	return nil, errUnreachable
}

// failure names a write Leafen refused to fill: the person gave no
// credential, or the administrator didn't allow giving one.
func failure(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "credential") {
		return errCredentials
	}
	return err
}

// reader buffers what a server sends.
type reader struct {
	w   wire
	buf []byte
	pos int
}

const readWait = 6 * time.Second

func (r *reader) more() error {
	data, closed, err := r.w.Read(65536, readWait)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if closed {
			return errors.New("the server closed the connection")
		}
		return errors.New("the server didn't answer in time")
	}
	if r.pos > 0 {
		r.buf = append(r.buf[:0], r.buf[r.pos:]...)
		r.pos = 0
	}
	r.buf = append(r.buf, data...)
	return nil
}

func (r *reader) peek() (byte, error) {
	for r.pos >= len(r.buf) {
		if err := r.more(); err != nil {
			return 0, err
		}
	}
	return r.buf[r.pos], nil
}

func (r *reader) next() (byte, error) {
	b, err := r.peek()
	if err == nil {
		r.pos++
	}
	return b, err
}

func (r *reader) take(n int) ([]byte, error) {
	for len(r.buf)-r.pos < n {
		if err := r.more(); err != nil {
			return nil, err
		}
	}
	out := append([]byte(nil), r.buf[r.pos:r.pos+n]...)
	r.pos += n
	return out, nil
}

// line is the rest of the line, without its CRLF.
func (r *reader) line() (string, error) {
	out := []byte{}
	for {
		b, err := r.next()
		if err != nil {
			return "", err
		}
		if b == '\n' {
			return strings.TrimSuffix(string(out), "\r"), nil
		}
		out = append(out, b)
	}
}

// smtpReply reads one reply, every line of it.
func smtpReply(r *reader) (int, string, error) {
	text := []string{}
	for {
		line, err := r.line()
		if err != nil {
			return 0, "", err
		}
		if len(line) < 3 {
			return 0, "", errServer
		}
		code, err := strconv.Atoi(line[:3])
		if err != nil {
			return 0, "", errServer
		}
		if len(line) > 4 {
			text = append(text, line[4:])
		}
		if len(line) == 3 || line[3] == ' ' {
			return code, strings.Join(text, "\n"), nil
		}
	}
}
