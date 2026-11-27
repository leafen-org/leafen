// The built-in WebDAV app (docs/app-platform.md 16): Finder, Windows
// Explorer, Office and sync tools reach Leafen as a drive at the app's
// endpoint, signed in with the person's app password. Pages are folders and
// the files on a page are its files: opening one opens the file, saving it
// puts the new file in the same block — the page's history keeps the old —
// and a new file becomes a file block at the page's end. Everything runs as
// the person, so they see and change only what they may in Leafen.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"
	"leafen.org/leafen/sdk/app/go/leafen"
)

type dav struct {
	request leafen.EndpointRequest
	names   names
	// prefix is the endpoint's own path on the server.
	prefix string
	// action: a run of the app's tools, which starts anew each time, keeps
	// no cache and makes no empty files.
	action bool
	// calls counts the run's host calls, of the 200 it has.
	calls int
	// keptBody: the PUT's body is a block's file or a side file's now.
	keptBody bool
}

// refusal is an answer of Leafen's that isn't a success.
type refusal struct {
	status int
	text   string
}

func (r *refusal) Error() string { return fmt.Sprintf("%d %s", r.status, r.text) }

//go:wasmexport dav
func davCall() int32 {
	request := leafen.EndpointRequest{}
	if err := leafen.InputJSON(&request); err != nil {
		return leafen.Fail("the request isn't JSON")
	}
	d := &dav{request: request, names: english}
	if strings.HasPrefix(strings.ToLower(request.Headers["accept-language"]), "ko") {
		d.names = korean
	}
	if address, err := url.Parse(request.Address); err == nil {
		d.prefix = strings.TrimSuffix(address.Path, "/")
	}
	answer, err := d.serve()
	if err != nil {
		var refused *refusal
		switch {
		case errors.Is(err, errNotFound):
			answer = plain(404, "Not found.")
		case errors.As(err, &refused) && (refused.status == 403 || refused.status == 423):
			answer = plain(403, "Leafen doesn't let you change that.")
		default:
			leafen.Log(request.Method + ": " + err.Error())
			answer = plain(502, "Leafen couldn't do that just now; try again.")
		}
	}
	_ = leafen.OutputJSON(answer)
	return 0
}

func (d *dav) api(method, path, query string, body any) (json.RawMessage, error) {
	d.calls++
	answer, err := leafen.API(leafen.APIRequest{Tenancy: d.request.Tenancy, Method: method, Path: path, Query: query, Body: body})
	if err != nil {
		return nil, err
	}
	if answer.Status == 404 {
		// Drive changes only what it has just found, and Leafen answers a change
		// someone may not make as it answers a page they can't see.
		if method != "GET" {
			return nil, &refusal{status: 403, text: string(answer.Body)}
		}
		return nil, errNotFound
	}
	if answer.Status >= 300 {
		return nil, &refusal{status: answer.Status, text: string(answer.Body)}
	}
	return answer.Body, nil
}

func plain(status int, text string) leafen.EndpointAnswer {
	return leafen.EndpointAnswer{Status: status, Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"}, Body: text}
}

const allowed = "OPTIONS, PROPFIND, PROPPATCH, GET, HEAD, PUT, DELETE, MKCOL, COPY, MOVE, LOCK, UNLOCK"

// segments splits a path below the endpoint into names, in Unicode's
// composed form (macOS sends Korean decomposed).
func segments(path string) []string {
	parts := []string{}
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, norm.NFC.String(part))
		}
	}
	return parts
}

func (d *dav) serve() (leafen.EndpointAnswer, error) {
	path := segments(d.request.Path)
	switch d.request.Method {
	case "OPTIONS":
		return leafen.EndpointAnswer{Status: 200, Headers: map[string]string{"DAV": "1, 2", "Allow": allowed, "MS-Author-Via": "DAV"}}, nil
	case "PROPFIND":
		return d.propfind(path)
	case "PROPPATCH":
		return d.proppatch(path)
	case "GET", "HEAD":
		return d.get(path)
	case "PUT":
		return d.put(path)
	case "DELETE":
		return d.remove(path)
	case "MKCOL":
		return d.mkcol(path)
	case "MOVE", "COPY":
		return d.transfer(path, d.request.Method == "MOVE")
	case "LOCK":
		return d.lock(path)
	case "UNLOCK":
		return leafen.EndpointAnswer{Status: 204}, d.unlock(path)
	}
	return leafen.EndpointAnswer{Status: 405, Headers: map[string]string{"Allow": allowed}}, nil
}

// junk are files systems make for themselves, which Leafen takes and lets
// go: macOS's AppleDouble and folder files, Windows' thumbnails.
func junk(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(name, "._") || lower == ".ds_store" || lower == "thumbs.db" || lower == "desktop.ini" ||
		lower == ".trashes" || lower == ".spotlight-v100" || lower == ".fseventsd" || lower == ".temporaryitems" || lower == ".localized"
}

func (d *dav) get(path []string) (leafen.EndpointAnswer, error) {
	if len(path) > 0 && junk(path[len(path)-1]) {
		return plain(404, "Not found."), nil
	}
	n, err := d.resolve(path)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	switch n.kind {
	case kindFile:
		headers := map[string]string{"Content-Type": n.file.text("contentType")}
		if sha := n.file.text("sha256"); sha != "" {
			headers["ETag"] = `"` + sha + `"`
		}
		return leafen.EndpointAnswer{File: n.file.text("fileId"), Headers: headers}, nil
	case kindEmpty:
		return leafen.EndpointAnswer{Status: 200, Headers: map[string]string{"Content-Type": "application/octet-stream"}}, nil
	case kindSide:
		if n.file.text("fileId") == "" {
			return leafen.EndpointAnswer{Status: 200, Headers: map[string]string{"Content-Type": "application/octet-stream"}}, nil
		}
		return leafen.EndpointAnswer{File: n.file.text("fileId"), Headers: map[string]string{"Content-Type": n.file.text("contentType")}}, nil
	}
	// A folder, for a browser: its names.
	children, err := d.children(n)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	var page strings.Builder
	page.WriteString("<!doctype html><meta charset=utf-8><ul>")
	for _, child := range children {
		suffix := ""
		if child.folder() {
			suffix = "/"
		}
		fmt.Fprintf(&page, "<li><a href=\"%s\">%s%s</a></li>", d.href(append(append([]string{}, path...), child.name), child.folder()), escape(child.name), suffix)
	}
	page.WriteString("</ul>")
	return leafen.EndpointAnswer{Status: 200, Headers: map[string]string{"Content-Type": "text/html; charset=utf-8"}, Body: page.String()}, nil
}

// parentOf resolves the folder a path's last name goes in, and that name.
func (d *dav) parentOf(path []string) (node, string, error) {
	if len(path) == 0 {
		return node{}, "", errNotFound
	}
	parent, err := d.resolve(path[:len(path)-1])
	if err != nil {
		return node{}, "", err
	}
	if !parent.folder() {
		return node{}, "", errNotFound
	}
	return parent, path[len(path)-1], nil
}

// child finds a folder's child by name.
func (d *dav) child(parent node, name string) (node, bool, error) {
	children, err := d.children(parent)
	if err != nil {
		return node{}, false, err
	}
	for _, candidate := range children {
		if same(candidate.name, name) {
			return candidate, true, nil
		}
	}
	return node{}, false, nil
}

func (d *dav) mkcol(path []string) (leafen.EndpointAnswer, error) {
	parent, name, err := d.parentOf(path)
	if errors.Is(err, errNotFound) {
		return plain(409, "The folder above isn't there."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if junk(name) {
		return plain(403, "Leafen keeps no system folders."), nil
	}
	if parent.kind != kindWorkspace && parent.kind != kindPage {
		return plain(403, "Make folders inside a workspace or a page."), nil
	}
	if _, exists, err := d.child(parent, name); err != nil || exists {
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return plain(405, "That name is taken."), nil
	}
	if _, err := d.api("POST", "/pages", "", map[string]any{"requestId": "webdav-" + newID(), "workspaceId": parent.workspaceID,
		"parentId": parent.pageID, "title": name}); err != nil {
		// Leafen answers a page it won't make under this parent with a conflict.
		var refused *refusal
		if errors.As(err, &refused) && refused.status == 409 {
			return plain(403, "Leafen doesn't let you change that."), nil
		}
		return leafen.EndpointAnswer{}, err
	}
	d.forget(parent)
	return leafen.EndpointAnswer{Status: 201}, nil
}

func (d *dav) remove(path []string) (leafen.EndpointAnswer, error) {
	if len(path) > 0 && junk(path[len(path)-1]) {
		return leafen.EndpointAnswer{Status: 204}, nil
	}
	parent, name, err := d.parentOf(path)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	n, found, err := d.child(parent, name)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if !found {
		return plain(404, "Not found."), nil
	}
	switch n.kind {
	case kindFile:
		// An office app's save waiting beside the document takes its place (aside.go).
		if save, waiting := d.saving(parent); waiting {
			d.holdDeleted(save, n)
		} else {
			err = d.mutate(n.pageID, map[string]any{"kind": "deleteBlock", "deletion": map[string]any{"blockId": n.file.ID}})
		}
	case kindEmpty:
		d.dropEmpty(n.pageID, n.name)
	case kindSide:
		// A side file holding a document aside takes it along, as on a disk.
		if n.aside != nil {
			err = d.mutate(n.pageID, map[string]any{"kind": "deleteBlock", "deletion": map[string]any{"blockId": n.aside.ID}})
		}
		if err == nil {
			d.dropSide(n)
		}
	case kindPage:
		if parent.kind == kindFilesRoot {
			return plain(403, "This folder stays."), nil
		}
		err = d.trashPage(n.pageID)
	default:
		return plain(403, "This folder stays."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	d.forget(parent)
	return leafen.EndpointAnswer{Status: 204}, nil
}

// trashPage puts a page in Leafen's trash, where it can be restored.
func (d *dav) trashPage(pageID string) error {
	return d.changePage(pageID, func(change map[string]any) { change["inTrash"] = true })
}

// changePage changes a page's title, place or trash state as Leafen's
// sidebar does.
func (d *dav) changePage(pageID string, edit func(map[string]any)) error {
	raw, err := d.api("GET", "/pages/"+pageID, "", nil)
	if err != nil {
		return err
	}
	page := struct {
		Title       string  `json:"title"`
		ParentID    string  `json:"parentId"`
		Cursor      float64 `json:"cursor"`
		TeamspaceID string  `json:"teamspaceId"`
	}{}
	if err := json.Unmarshal(raw, &page); err != nil {
		return err
	}
	change := map[string]any{"title": page.Title, "parentId": page.ParentID, "inTrash": false, "mutationId": "webdav-" + newID(), "baseCursor": page.Cursor}
	if page.TeamspaceID != "" {
		change["teamspaceId"] = page.TeamspaceID
	}
	edit(change)
	_, err = d.api("PATCH", "/pages/"+pageID+"/metadata", "", change)
	return err
}

// mutate changes a page's content.
func (d *dav) mutate(pageID string, operations ...any) error {
	_, err := d.api("POST", "/pages/"+pageID+"/content-graph/mutations", "", map[string]any{"mutationId": newID(), "operations": operations})
	return err
}

func (d *dav) proppatch(path []string) (leafen.EndpointAnswer, error) {
	if _, err := d.resolve(path); err != nil {
		if len(path) > 0 && junk(path[len(path)-1]) {
			return plain(404, "Not found."), nil
		}
		return leafen.EndpointAnswer{}, err
	}
	// Leafen keeps no client properties; it says each was set, as clients
	// that set times on a copy expect.
	return multistatus(proppatchAnswer(d.href(path, false), propNames(d.body()))), nil
}

func (d *dav) body() []byte {
	raw, _ := decodeBase64(d.request.Body)
	return raw
}

func main() {}
