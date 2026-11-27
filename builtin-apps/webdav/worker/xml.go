package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// WebDAV's XML (RFC 4918): what a folder holds and each thing's properties,
// and the locks Office and Finder take before they write.

func escape(text string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(text))
	return out.String()
}

// href is a path below the endpoint as the client addresses it.
func (d *dav) href(path []string, folder bool) string {
	parts := []string{d.prefix}
	for _, name := range path {
		parts = append(parts, url.PathEscape(name))
	}
	href := strings.Join(parts, "/")
	if folder {
		href += "/"
	}
	return href
}

func multistatus(responses string) leafen.EndpointAnswer {
	return leafen.EndpointAnswer{Status: 207, Headers: map[string]string{"Content-Type": `application/xml; charset="utf-8"`},
		Body: `<?xml version="1.0" encoding="utf-8"?>` + "\n" + `<D:multistatus xmlns:D="DAV:">` + responses + `</D:multistatus>`}
}

// leafenTime reads Leafen's times ("2006-01-02 15:04:05.000Z" or RFC 3339).
func leafenTime(value string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05.000Z", "2006-01-02 15:04:05Z", time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Unix(0, 0)
}

const supportedLock = `<D:supportedlock><D:lockentry><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry>` +
	`<D:lockentry><D:lockscope><D:shared/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry></D:supportedlock>`

func (d *dav) properties(path []string, n node) string {
	var props strings.Builder
	fmt.Fprintf(&props, "<D:displayname>%s</D:displayname>", escape(n.name))
	modified, created := leafenTime(n.updated), leafenTime(n.created)
	if n.folder() {
		props.WriteString("<D:resourcetype><D:collection/></D:resourcetype>")
	} else {
		props.WriteString("<D:resourcetype/>")
		contentType := n.file.text("contentType")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		fmt.Fprintf(&props, "<D:getcontentlength>%d</D:getcontentlength><D:getcontenttype>%s</D:getcontenttype>", n.file.size(), escape(contentType))
		etag := n.file.text("sha256")
		if etag == "" {
			etag = "empty"
		}
		fmt.Fprintf(&props, `<D:getetag>"%s"</D:getetag>`, escape(etag))
	}
	if n.updated != "" {
		fmt.Fprintf(&props, "<D:getlastmodified>%s</D:getlastmodified>", modified.UTC().Format(time.RFC1123))
	}
	if n.created != "" {
		fmt.Fprintf(&props, "<D:creationdate>%s</D:creationdate>", created.UTC().Format(time.RFC3339))
	}
	props.WriteString(supportedLock)
	return fmt.Sprintf("<D:response><D:href>%s</D:href><D:propstat><D:prop>%s</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>",
		escape(d.href(path, n.folder())), props.String())
}

func (d *dav) propfind(path []string) (leafen.EndpointAnswer, error) {
	if len(path) > 0 && junk(path[len(path)-1]) {
		return plain(404, "Not found."), nil
	}
	n, err := d.resolve(path)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if n.name == "" {
		n.name = "Leafen"
	}
	// A whole tree at once is refused as RFC 4918 lets a server; clients
	// then walk it a level at a time.
	depth := strings.ToLower(strings.TrimSpace(d.request.Headers["depth"]))
	if n.folder() && (depth == "" || depth == "infinity") {
		return leafen.EndpointAnswer{Status: 403, Headers: map[string]string{"Content-Type": `application/xml; charset="utf-8"`},
			Body: `<?xml version="1.0" encoding="utf-8"?>` + "\n" + `<D:error xmlns:D="DAV:"><D:propfind-finite-depth/></D:error>`}, nil
	}
	// One buffer for the answer: adding each response to a string copied
	// the whole answer again for every child.
	var responses strings.Builder
	responses.WriteString(d.properties(path, n))
	if n.folder() && depth != "0" {
		children, err := d.children(n)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		for _, child := range children {
			if !junk(child.name) {
				responses.WriteString(d.properties(append(append([]string{}, path...), child.name), child))
			}
		}
	}
	return multistatus(responses.String()), nil
}

// propNames are the properties a PROPPATCH names, as "namespace name".
func propNames(body []byte) [][2]string {
	names := [][2]string{}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth, inProp := 0, -1
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if element.Name.Local == "prop" && element.Name.Space == "DAV:" {
				inProp = depth
			} else if inProp >= 0 && depth == inProp+1 {
				names = append(names, [2]string{element.Name.Space, element.Name.Local})
			}
		case xml.EndElement:
			if depth == inProp {
				inProp = -1
			}
			depth--
		}
	}
	return names
}

func proppatchAnswer(href string, names [][2]string) string {
	var props strings.Builder
	for index, name := range names {
		fmt.Fprintf(&props, `<x%d:%s xmlns:x%d="%s"/>`, index, escape(name[1]), index, escape(name[0]))
	}
	return fmt.Sprintf("<D:response><D:href>%s</D:href><D:propstat><D:prop>%s</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>",
		escape(href), props.String())
}

// Locks: Leafen keeps a token for the path a while and gives it back; it
// doesn't hold other writers off — a page's history keeps every version.

func lockKey(path []string) string { return "lock:" + strings.ToLower(strings.Join(path, "/")) }

func lockSeconds(header string) int64 {
	for _, part := range strings.Split(header, ",") {
		if seconds, ok := strings.CutPrefix(strings.TrimSpace(part), "Second-"); ok {
			if value, err := strconv.ParseInt(seconds, 10, 64); err == nil && value > 0 {
				return min(value, keptFor)
			}
		}
	}
	return keptFor
}

func (d *dav) lock(path []string) (leafen.EndpointAnswer, error) {
	seconds := lockSeconds(d.request.Headers["timeout"])
	key := lockKey(path)
	body := d.body()
	token := ""
	owner := ""
	if len(bytes.TrimSpace(body)) == 0 {
		// A refresh names the lock it renews.
		given := d.request.Headers["if"]
		if start := strings.Index(given, "<opaquelocktoken:"); start >= 0 {
			if end := strings.Index(given[start:], ">"); end > 0 {
				token = given[start+1 : start+end]
			}
		}
		if token == "" {
			return plain(400, "Name the lock to renew."), nil
		}
	} else {
		token, owner = "opaquelocktoken:"+newID(), ownerOf(body)
	}
	status := 200
	if len(path) > 0 && !junk(path[len(path)-1]) {
		if _, err := d.resolve(path); err != nil {
			// Locking a name nothing has yet makes it, empty.
			parent, name, err := d.parentOf(path)
			if err != nil || parent.kind != kindPage {
				return plain(409, "Lock files inside a page."), nil
			}
			_, _, held, err := d.held(parent, name)
			switch {
			case err != nil:
				return leafen.EndpointAnswer{}, err
			case held:
				// A document held aside keeps its name for what comes there.
			case d.sideIn(parent, name):
				d.keepSide(parent.pageID, name, sideOf(nil, name, "", false))
			default:
				d.keepEmpty(parent.pageID, name)
			}
			d.forget(parent)
			status = 201
		}
	}
	d.kv(leafen.KVRequest{Command: "SET", Key: key, Value: token, TTL: seconds})
	discovery := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>`+"\n"+`<D:prop xmlns:D="DAV:"><D:lockdiscovery><D:activelock>`+
		`<D:locktype><D:write/></D:locktype><D:lockscope><D:exclusive/></D:lockscope><D:depth>0</D:depth>%s`+
		`<D:timeout>Second-%d</D:timeout><D:locktoken><D:href>%s</D:href></D:locktoken>`+
		`<D:lockroot><D:href>%s</D:href></D:lockroot></D:activelock></D:lockdiscovery></D:prop>`,
		owner, seconds, escape(token), escape(d.href(path, false)))
	return leafen.EndpointAnswer{Status: status, Headers: map[string]string{"Content-Type": `application/xml; charset="utf-8"`, "Lock-Token": "<" + token + ">"},
		Body: discovery}, nil
}

// ownerOf gives back the owner a LOCK named, as it came.
func ownerOf(body []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "owner" {
			var inner struct {
				Text string `xml:",innerxml"`
			}
			if decoder.DecodeElement(&inner, &start) != nil {
				return ""
			}
			return "<D:owner>" + inner.Text + "</D:owner>"
		}
	}
}

func (d *dav) unlock(path []string) error {
	d.kv(leafen.KVRequest{Command: "DEL", Key: lockKey(path)})
	return nil
}

// keptNames reads the temporary store's answer to HGETALL: an object, or
// names and values one after another.
func keptNames(raw json.RawMessage) []string {
	names := []string{}
	asMap := map[string]string{}
	if json.Unmarshal(raw, &asMap) == nil {
		for name := range asMap {
			names = append(names, name)
		}
	} else {
		pairs := []string{}
		if json.Unmarshal(raw, &pairs) == nil {
			for index := 0; index+1 < len(pairs); index += 2 {
				names = append(names, pairs[index])
			}
		}
	}
	sort.Strings(names)
	return names
}
