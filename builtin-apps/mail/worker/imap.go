package main

import (
	"fmt"
	"strconv"
	"strings"
)

// A small IMAP4rev1 client (RFC 3501): what the app needs to list, read and
// mark the person's inbox. Leafen fills the login from the person's mail
// account; the worker never sees it.

type imapClient struct {
	r        *reader
	tag      int
	exists   int
	validity int64
	uidNext  int64
}

// value is one item of a server response: an atom, a string (quoted or a
// literal), a parenthesized list, or NIL.
type value struct {
	kind  byte // 'a', 's', 'l', 'n'
	text  string
	items []value
}

type response struct {
	tag    string
	status string // OK, NO, BAD, BYE, PREAUTH, or "" for data
	text   string
	values []value
}

// openInbox signs in and opens the inbox, to read (examine) or to mark it.
func openInbox(server mailServer, write bool) (*imapClient, error) {
	connection, err := dial(server.imap)
	if err != nil {
		return nil, err
	}
	client := &imapClient{r: &reader{w: connection}}
	greeting, err := client.read()
	if err != nil || greeting.status != "OK" && greeting.status != "PREAUTH" {
		_ = connection.Close()
		return nil, errServer
	}
	if greeting.status == "OK" {
		if _, err := client.command("LOGIN " + server.field(server.user, "quoted") + " " + server.field("password", "quoted")); err != nil {
			_ = connection.Close()
			if refused, ok := err.(*imapRefusal); ok && refused.status == "NO" {
				return nil, errLogin
			}
			return nil, failure(err)
		}
	}
	verb := "EXAMINE"
	if write {
		verb = "SELECT"
	}
	answers, err := client.command(verb + " INBOX")
	if err != nil {
		_ = connection.Close()
		return nil, errServer
	}
	for _, answer := range answers {
		if len(answer.values) >= 2 && strings.EqualFold(answer.values[1].text, "EXISTS") {
			client.exists, _ = strconv.Atoi(answer.values[0].text)
		}
		if code, argument := responseCode(answer.text); code == "UIDVALIDITY" {
			client.validity, _ = strconv.ParseInt(argument, 10, 64)
		} else if code == "UIDNEXT" {
			client.uidNext, _ = strconv.ParseInt(argument, 10, 64)
		}
	}
	return client, nil
}

func (c *imapClient) logout() {
	_, _ = c.command("LOGOUT")
	_ = c.r.w.Close()
}

type imapRefusal struct{ status, text string }

func (refusal *imapRefusal) Error() string { return refusal.status + " " + refusal.text }

// command sends one command and gathers the untagged responses until its
// own tagged one.
func (c *imapClient) command(command string) ([]response, error) {
	c.tag++
	tag := fmt.Sprintf("a%d", c.tag)
	if err := c.r.w.Write([]byte(tag + " " + command + "\r\n")); err != nil {
		return nil, failure(err)
	}
	untagged := []response{}
	for {
		answer, err := c.read()
		if err != nil {
			return untagged, err
		}
		if answer.tag == tag {
			if answer.status != "OK" {
				return untagged, &imapRefusal{status: answer.status, text: answer.text}
			}
			return untagged, nil
		}
		untagged = append(untagged, answer)
	}
}

// fetch runs a FETCH (or UID FETCH) and returns each message's items by name.
func (c *imapClient) fetch(command string) ([]map[string]value, error) {
	answers, err := c.command(command)
	if err != nil {
		return nil, err
	}
	found := []map[string]value{}
	for _, answer := range answers {
		if len(answer.values) < 3 || !strings.EqualFold(answer.values[1].text, "FETCH") || answer.values[2].kind != 'l' {
			continue
		}
		items := map[string]value{}
		list := answer.values[2].items
		for index := 0; index+1 < len(list); index += 2 {
			name := strings.ToUpper(list[index].text)
			if strings.HasPrefix(name, "BODY[") {
				name = "BODY"
			}
			items[name] = list[index+1]
		}
		found = append(found, items)
	}
	return found, nil
}

func (c *imapClient) read() (response, error) {
	tag, err := c.word()
	if err != nil {
		return response{}, err
	}
	if tag == "+" {
		text, err := c.r.line()
		return response{tag: tag, text: strings.TrimSpace(text)}, err
	}
	first, err := c.atom()
	if err != nil {
		return response{}, err
	}
	switch upper := strings.ToUpper(first); upper {
	case "OK", "NO", "BAD", "BYE", "PREAUTH":
		text, err := c.r.line()
		return response{tag: tag, status: upper, text: strings.TrimSpace(text)}, err
	}
	answer := response{tag: tag, values: []value{{kind: 'a', text: first}}}
	for {
		b, err := c.r.peek()
		if err != nil {
			return answer, err
		}
		switch b {
		case '\r', '\n':
			_, err := c.r.line()
			return answer, err
		case ' ':
			c.r.pos++
			continue
		}
		item, err := c.value()
		if err != nil {
			return answer, err
		}
		answer.values = append(answer.values, item)
	}
}

// word is the response's tag, up to the space after it.
func (c *imapClient) word() (string, error) {
	out := []byte{}
	for {
		b, err := c.r.next()
		if err != nil {
			return "", err
		}
		if b == ' ' {
			return string(out), nil
		}
		if b == '\r' || b == '\n' {
			return "", errServer
		}
		out = append(out, b)
	}
}

// atom reads up to a space, a parenthesis or the line's end; a bracketed
// part (BODY[HEADER.FIELDS (FROM)]) is kept whole.
func (c *imapClient) atom() (string, error) {
	out := []byte{}
	depth := 0
	for {
		b, err := c.r.peek()
		if err != nil {
			return "", err
		}
		if depth == 0 && (b == ' ' || b == '(' || b == ')' || b == '\r' || b == '\n') {
			if len(out) == 0 {
				return "", errServer
			}
			return string(out), nil
		}
		if b == '[' {
			depth++
		} else if b == ']' && depth > 0 {
			depth--
		} else if b == '\r' || b == '\n' {
			return "", errServer
		}
		c.r.pos++
		out = append(out, b)
	}
}

func (c *imapClient) value() (value, error) {
	b, err := c.r.peek()
	if err != nil {
		return value{}, err
	}
	switch b {
	case '(':
		c.r.pos++
		list := value{kind: 'l'}
		for {
			b, err := c.r.peek()
			if err != nil {
				return list, err
			}
			switch b {
			case ')':
				c.r.pos++
				return list, nil
			case ' ':
				c.r.pos++
				continue
			case '\r', '\n':
				return list, errServer
			}
			item, err := c.value()
			if err != nil {
				return list, err
			}
			list.items = append(list.items, item)
		}
	case '"':
		c.r.pos++
		out := []byte{}
		for {
			b, err := c.r.next()
			if err != nil {
				return value{}, err
			}
			if b == '\\' {
				if b, err = c.r.next(); err != nil {
					return value{}, err
				}
			} else if b == '"' {
				return value{kind: 's', text: string(out)}, nil
			} else if b == '\r' || b == '\n' {
				return value{}, errServer
			}
			out = append(out, b)
		}
	case '{':
		header, err := c.r.line()
		if err != nil {
			return value{}, err
		}
		size, err := strconv.Atoi(strings.TrimSuffix(strings.Trim(header, "{}"), "+"))
		if err != nil || size < 0 || size > 4<<20 {
			return value{}, errServer
		}
		data, err := c.r.take(size)
		return value{kind: 's', text: string(data)}, err
	}
	atom, err := c.atom()
	if err != nil {
		return value{}, err
	}
	if strings.EqualFold(atom, "NIL") {
		return value{kind: 'n'}, nil
	}
	return value{kind: 'a', text: atom}, nil
}

// responseCode reads "[UIDNEXT 15] …" as UIDNEXT and 15.
func responseCode(text string) (string, string) {
	if !strings.HasPrefix(text, "[") {
		return "", ""
	}
	end := strings.IndexByte(text, ']')
	if end < 0 {
		return "", ""
	}
	parts := strings.SplitN(text[1:end], " ", 2)
	argument := ""
	if len(parts) == 2 {
		argument = parts[1]
	}
	return strings.ToUpper(parts[0]), argument
}

func flagsOf(item value) []string {
	flags := []string{}
	for _, flag := range item.items {
		flags = append(flags, flag.text)
	}
	return flags
}

func hasFlag(flags []string, wanted string) bool {
	for _, flag := range flags {
		if strings.EqualFold(flag, wanted) {
			return true
		}
	}
	return false
}
