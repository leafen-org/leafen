package main

import (
	"bytes"
	"encoding/base64"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
)

// Reading a message (RFC 5322, MIME): its headers in any character set,
// the text of its body — the plain part, or the HTML part as text — and its
// attachments by name and size.

const textMax = 200_000

type person struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type attachment struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

type opened struct {
	Subject     string       `json:"subject"`
	From        person       `json:"from"`
	To          []person     `json:"to"`
	Cc          []person     `json:"cc"`
	Date        string       `json:"date"`
	MessageID   string       `json:"messageId"`
	Text        string       `json:"text"`
	Attachments []attachment `json:"attachments"`
}

// decoder reads encoded words (=?euc-kr?B?…?=) in any character set the web
// knows by name.
var decoder = &mime.WordDecoder{CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
	encoding, err := htmlindex.Get(charset)
	if err != nil {
		return nil, err
	}
	return encoding.NewDecoder().Reader(input), nil
}}

func headerText(value string) string {
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		decoded = value
	}
	return strings.ToValidUTF8(strings.TrimSpace(decoded), "�")
}

func people(header mail.Header, name string) []person {
	parser := mail.AddressParser{WordDecoder: decoder}
	list, err := parser.ParseList(header.Get(name))
	if err != nil {
		if raw := headerText(header.Get(name)); raw != "" {
			return []person{{Name: raw}}
		}
		return []person{}
	}
	out := []person{}
	for _, address := range list {
		out = append(out, person{Name: strings.ToValidUTF8(address.Name, "�"), Address: address.Address})
	}
	return out
}

func first(list []person) person {
	if len(list) == 0 {
		return person{}
	}
	return list[0]
}

func dateOf(header mail.Header, fallback time.Time) string {
	if parsed, err := mail.ParseDate(header.Get("Date")); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	if fallback.IsZero() {
		return ""
	}
	return fallback.UTC().Format(time.RFC3339)
}

// readHeader parses headers alone (a listing's HEADER.FIELDS).
func readHeader(raw string) mail.Header {
	message, err := mail.ReadMessage(strings.NewReader(strings.TrimRight(raw, "\r\n") + "\r\n\r\n"))
	if err != nil {
		return mail.Header{}
	}
	return message.Header
}

// openMessage reads a whole message.
func openMessage(raw []byte) opened {
	out := opened{To: []person{}, Cc: []person{}, Attachments: []attachment{}}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		out.Text = strings.ToValidUTF8(string(raw), "�")
		return out
	}
	header := message.Header
	out.Subject = headerText(header.Get("Subject"))
	out.From = first(people(header, "From"))
	out.To = people(header, "To")
	out.Cc = people(header, "Cc")
	out.Date = dateOf(header, time.Time{})
	out.MessageID = strings.TrimSpace(header.Get("Message-Id"))
	parts := &body{attachments: []attachment{}}
	parts.walk(header.Get("Content-Type"), header.Get("Content-Transfer-Encoding"), header.Get("Content-Disposition"), message.Body, 0)
	out.Text, out.Attachments = parts.plain, parts.attachments
	if out.Text == "" && parts.html != "" {
		out.Text = htmlText(parts.html)
	}
	if len(out.Text) > textMax {
		out.Text = strings.ToValidUTF8(out.Text[:textMax], "") + "…"
	}
	return out
}

type body struct {
	plain, html string
	attachments []attachment
}

func (b *body) walk(contentType, transfer, disposition string, data io.Reader, depth int) {
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		media, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(media, "multipart/") && depth < 8 {
		parts := multipart.NewReader(data, params["boundary"])
		for {
			part, err := parts.NextRawPart()
			if err != nil {
				return
			}
			b.walk(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"),
				part.Header.Get("Content-Disposition"), part, depth+1)
		}
	}
	decoded := decodeTransfer(transfer, data)
	content, _ := io.ReadAll(io.LimitReader(decoded, 8<<20))
	kind, dispositionParams, _ := mime.ParseMediaType(disposition)
	name := headerText(dispositionParams["filename"])
	if name == "" {
		name = headerText(params["name"])
	}
	if kind == "attachment" || name != "" && kind != "inline" || !strings.HasPrefix(media, "text/") {
		if name == "" {
			name = media
		}
		b.attachments = append(b.attachments, attachment{Name: name, Size: len(content)})
		return
	}
	text := charsetText(params["charset"], content)
	switch {
	case media == "text/plain" && b.plain == "":
		b.plain = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	case media == "text/html" && b.html == "":
		b.html = text
	}
}

func decodeTransfer(transfer string, data io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(transfer)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &spaceless{data})
	case "quoted-printable":
		return quotedprintable.NewReader(data)
	}
	return data
}

// spaceless drops the spaces and tabs some senders leave in base64 lines.
type spaceless struct{ io.Reader }

func (s *spaceless) Read(p []byte) (int, error) {
	n, err := s.Reader.Read(p)
	kept := 0
	for _, b := range p[:n] {
		if b != ' ' && b != '\t' {
			p[kept] = b
			kept++
		}
	}
	return kept, err
}

func charsetText(charset string, content []byte) string {
	charset = strings.ToLower(strings.Trim(strings.TrimSpace(charset), `"`))
	if charset != "" && charset != "utf-8" && charset != "us-ascii" {
		if encoding, err := htmlindex.Get(charset); err == nil {
			if decoded, err := encoding.NewDecoder().Bytes(content); err == nil {
				return string(decoded)
			}
		}
	}
	if utf8.Valid(content) {
		return string(content)
	}
	return strings.ToValidUTF8(string(content), "�")
}

var (
	spaces    = regexp.MustCompile(`[ \t\r\n\f]+`)
	lineGaps  = regexp.MustCompile(`\n[ \t]+|[ \t]+\n`)
	manyLines = regexp.MustCompile(`\n{3,}`)
	blocks    = map[string]bool{"p": true, "div": true, "tr": true, "li": true, "table": true, "blockquote": true, "h1": true,
		"h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "hr": true, "section": true, "article": true}
)

// htmlText turns an HTML body into its text: blocks on lines of their own,
// scripts, styles and comments left out.
func htmlText(source string) string {
	var out strings.Builder
	skip := ""
	for index := 0; index < len(source); {
		if strings.HasPrefix(source[index:], "<!--") {
			end := strings.Index(source[index+4:], "-->")
			if end < 0 {
				break
			}
			index += end + 7
			continue
		}
		if source[index] == '<' {
			end := strings.IndexByte(source[index:], '>')
			if end < 0 {
				break
			}
			tag := strings.ToLower(strings.TrimSpace(source[index+1 : index+end]))
			closing := strings.HasPrefix(tag, "/")
			name := strings.TrimLeft(tag, "/")
			if cut := strings.IndexAny(name, " \t\r\n/"); cut >= 0 {
				name = name[:cut]
			}
			index += end + 1
			if skip != "" {
				if closing && name == skip {
					skip = ""
				}
				continue
			}
			switch {
			case !closing && (name == "script" || name == "style" || name == "head" || name == "title"):
				skip = name
			case name == "br":
				out.WriteString("\n")
			case blocks[name]:
				out.WriteString("\n")
			}
			continue
		}
		next := strings.IndexByte(source[index:], '<')
		if next < 0 {
			next = len(source) - index
		}
		if skip == "" {
			out.WriteString(spaces.ReplaceAllString(source[index:index+next], " "))
		}
		index += next
	}
	text := html.UnescapeString(out.String())
	text = strings.ReplaceAll(text, " ", " ")
	text = lineGaps.ReplaceAllString(text, "\n")
	return strings.TrimSpace(manyLines.ReplaceAllString(text, "\n\n"))
}
