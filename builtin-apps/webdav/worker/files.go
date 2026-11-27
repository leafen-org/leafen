package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Files: a PUT's body comes as a stored file (the manifest's fileBodies),
// copied into the page's workspace when it lives elsewhere, and becomes the
// file of a block — the same block when the name is already there.

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func decodeBase64(text string) ([]byte, error) { return base64.StdEncoding.DecodeString(text) }

// contentTypeOf is what a file is: as the client said, or by its name.
func contentTypeOf(given, name string) string {
	media, _, _ := mime.ParseMediaType(given)
	if media != "" && media != "application/octet-stream" {
		return given
	}
	if guessed := mime.TypeByExtension(strings.ToLower(path.Ext(name))); guessed != "" {
		return guessed
	}
	return "application/octet-stream"
}

// into is a stored file in a workspace: the file itself when it is there,
// else a copy made there.
func into(workspaceID string, file leafen.StoredFile, name, contentType string) (leafen.StoredFile, error) {
	if file.WorkspaceID == workspaceID && file.Name == name {
		return file, nil
	}
	return leafen.JoinFilesInto(workspaceID, []string{file.ID}, name, contentType)
}

func fileContent(base map[string]any, file leafen.StoredFile, name string) map[string]any {
	content := map[string]any{}
	for key, value := range base {
		content[key] = value
	}
	content["fileId"], content["name"], content["size"], content["contentType"], content["sha256"] = file.ID, name, file.Size, file.ContentType, file.SHA256
	return content
}

func blockOperation(block fileBlock, content map[string]any) map[string]any {
	value := map[string]any{"id": block.ID, "parentId": block.ParentID, "position": block.Position, "type": block.Type, "content": content}
	if block.AfterID != "" {
		value["afterId"] = block.AfterID
	} else {
		value["afterId"] = nil
	}
	return map[string]any{"kind": "upsertBlock", "block": value}
}

// nextPosition comes after a block's place in the page's order.
func nextPosition(last *fileBlock) string {
	if last == nil {
		return "p0000000000"
	}
	if number, err := strconv.ParseInt(strings.TrimPrefix(last.Position, "p"), 10, 64); err == nil && strings.HasPrefix(last.Position, "p") {
		return fmt.Sprintf("p%010d", number+1)
	}
	return last.Position + "z"
}

// appendFile puts a new file block at the page's end.
func (d *dav) appendFile(pageID string, content map[string]any) error {
	_, last, err := d.content(pageID)
	if err != nil {
		return err
	}
	block := fileBlock{ID: newID(), Type: "file", Position: nextPosition(last)}
	if last != nil {
		block.AfterID = last.ID
	}
	return d.mutate(pageID, blockOperation(block, content))
}

// upload is the file a PUT sent, or nothing for an empty body.
func (d *dav) upload() *leafen.StoredFile {
	return d.request.BodyFile
}

// release lets go of a stored file the drive was given and keeps no more,
// so no one's storage counts it — Leafen would clean it away only a day
// later (2026-11-14: the bodies of hidden and side files, and the one a
// document's copy was made from, counted twice what the drive keeps).
func (d *dav) release(file string) {
	if file == "" {
		return
	}
	if _, err := d.api("DELETE", "/files/"+file, "", map[string]any{"requestId": "webdav-" + newID()}); err != nil {
		leafen.Log("release: " + err.Error())
	}
}

// put keeps a PUT's body where its name goes, and lets go of it when it
// becomes nothing — a system's own file, a refusal — or a copy of it does.
func (d *dav) put(path []string) (leafen.EndpointAnswer, error) {
	answer, err := d.putFile(path)
	if upload := d.upload(); upload != nil && !d.keptBody {
		d.release(upload.ID)
	}
	return answer, err
}

func (d *dav) putFile(path []string) (leafen.EndpointAnswer, error) {
	parent, name, err := d.parentOf(path)
	if errors.Is(err, errNotFound) {
		return plain(409, "The folder above isn't there."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if junk(name) {
		return leafen.EndpointAnswer{Status: 201}, nil
	}
	if parent.kind != kindPage {
		return plain(403, "Put files inside a page."), nil
	}
	existing, found, err := d.child(parent, name)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if found && existing.folder() {
		return plain(409, "A folder has that name."), nil
	}
	if d.sideIn(parent, name) && !(found && existing.kind == kindFile) {
		return d.putSide(parent, name, existing), nil
	}
	// A document held aside under the name takes what comes there (aside.go);
	// to the client the name was free.
	holder := node{}
	if !found || existing.kind == kindEmpty {
		document, by, held, err := d.held(parent, name)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if held {
			if found {
				d.dropEmpty(existing.pageID, existing.name)
			}
			existing, found, holder = document, true, by
		}
	}
	replaced := 204
	if holder.kind == kindSide {
		replaced = 201
	}
	upload := d.upload()
	if upload == nil {
		// A client makes a file empty before it writes it.
		if found && existing.kind == kindFile {
			return leafen.EndpointAnswer{Status: replaced}, nil
		}
		d.keepEmpty(parent.pageID, name)
		d.forget(parent)
		return leafen.EndpointAnswer{Status: 201}, nil
	}
	contentType := contentTypeOf(d.request.Headers["content-type"], name)
	stored, err := into(parent.workspaceID, *upload, name, contentType)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	d.keptBody = stored.ID == upload.ID
	status := 201
	if found && existing.kind == kindFile {
		// The new file goes in the same block; the page's history keeps the
		// old one.
		err, status = d.mutate(parent.pageID, blockOperation(existing.file, fileContent(existing.file.Content, stored, name))), replaced
	} else {
		err = d.appendFile(parent.pageID, fileContent(nil, stored, name))
	}
	if err != nil {
		// Neither the body nor its copy is a block's; Leafen refuses to let go
		// of one that is.
		if d.keptBody = false; stored.ID != upload.ID {
			d.release(stored.ID)
		}
		return leafen.EndpointAnswer{}, err
	}
	d.dropEmpty(parent.pageID, name)
	if holder.kind == kindSide {
		d.unhold(holder)
	}
	d.forget(parent)
	answer := leafen.EndpointAnswer{Status: status}
	// Leafen keeps the time a sync tool sends with the file (rclone,
	// ownCloud's and Nextcloud's clients), and says so as they ask.
	if seconds, err := strconv.ParseInt(strings.TrimSpace(d.request.Headers["x-oc-mtime"]), 10, 64); err == nil &&
		seconds > 0 && seconds <= time.Now().Add(24*time.Hour).Unix() {
		answer.Headers = map[string]string{"X-OC-Mtime": "accepted"}
	}
	return answer, nil
}

// destination reads a MOVE's or COPY's Destination as names below the
// endpoint.
func (d *dav) destination() ([]string, bool) {
	given, err := url.Parse(d.request.Headers["destination"])
	if err != nil {
		return nil, false
	}
	below, ok := strings.CutPrefix(given.Path, d.prefix)
	if !ok {
		return nil, false
	}
	return segments(below), true
}

func (d *dav) transfer(path []string, move bool) (leafen.EndpointAnswer, error) {
	target, ok := d.destination()
	if !ok || len(target) == 0 {
		return plain(502, "The destination is outside this drive."), nil
	}
	overwrite := !strings.EqualFold(strings.TrimSpace(d.request.Headers["overwrite"]), "F")
	if len(path) > 0 && junk(path[len(path)-1]) {
		return leafen.EndpointAnswer{Status: 201}, nil
	}
	sourceParent, sourceName, err := d.parentOf(path)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	source, found, err := d.child(sourceParent, sourceName)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if !found {
		return plain(404, "Not found."), nil
	}
	targetParent, targetName, err := d.parentOf(target)
	if errors.Is(err, errNotFound) {
		return plain(409, "The folder above the destination isn't there."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	existing, taken, err := d.child(targetParent, targetName)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	// Empty and side files are told apart by name; pages and files by id.
	samePlace := taken && existing.kind == source.kind && existing.pageID == source.pageID && existing.file.ID == source.file.ID &&
		(source.kind == kindFile || source.kind == kindPage || existing.name == source.name)
	if taken && !samePlace && !overwrite {
		return plain(412, "Something has that name."), nil
	}
	status := 201
	if taken && !samePlace {
		status = 204
	}
	defer d.forget(sourceParent)
	defer d.forget(targetParent)
	// A document held aside under the name takes what comes there (aside.go).
	holder := node{}
	if !taken || existing.kind == kindEmpty {
		document, by, found, err := d.held(targetParent, targetName)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if found {
			if taken {
				d.dropEmpty(existing.pageID, existing.name)
			}
			existing, taken, holder = document, true, by
		}
	}
	answer, err := d.transferTo(source, sourceParent, targetParent, targetName, existing, taken && !samePlace, move, status)
	if err == nil && answer.Status < 300 && holder.kind == kindSide && !(move && holder.pageID == source.pageID && holder.name == source.name) {
		d.unhold(holder)
	}
	return answer, err
}

func (d *dav) transferTo(source, sourceParent, targetParent node, targetName string, existing node, replace, move bool, status int) (leafen.EndpointAnswer, error) {
	if source.kind == kindSide {
		return d.transferSide(source, targetParent, targetName, existing, replace, move, status)
	}
	// A side file a document takes the name of goes.
	if replace && existing.kind == kindSide {
		d.dropSide(existing)
		existing, replace = node{}, false
	}
	// A document becomes a side file under a side file's name; a block that
	// has the name from before takes it as a block.
	if source.kind == kindFile && d.sideIn(targetParent, targetName) && !replace && existing.kind != kindFile && targetParent.kind == kindPage {
		return d.asideTo(source, targetParent, targetName, move, status)
	}
	switch source.kind {
	case kindEmpty:
		if targetParent.kind != kindPage {
			return plain(403, "Put files inside a page."), nil
		}
		if move {
			d.dropEmpty(source.pageID, source.name)
		}
		d.keepEmpty(targetParent.pageID, targetName)
		return leafen.EndpointAnswer{Status: status}, nil
	case kindPage:
		return d.transferPage(source, sourceParent, targetParent, targetName, existing, replace, move, status)
	case kindFile:
		return d.transferFile(source, targetParent, targetName, existing, replace, move, status)
	}
	return plain(403, "This folder stays."), nil
}

func (d *dav) transferFile(source, targetParent node, targetName string, existing node, replace, move bool, status int) (leafen.EndpointAnswer, error) {
	if targetParent.kind != kindPage {
		return plain(403, "Put files inside a page."), nil
	}
	if replace && existing.folder() {
		return plain(409, "A folder has that name."), nil
	}
	file := leafen.StoredFile{ID: source.file.text("fileId"), WorkspaceID: source.workspaceID, Name: source.file.text("name"),
		Size: source.file.size(), ContentType: source.file.text("contentType"), SHA256: source.file.text("sha256")}
	// A copy, or a move to another workspace, is a file of its own.
	if !move || source.workspaceID != targetParent.workspaceID {
		stored, err := leafen.JoinFilesInto(targetParent.workspaceID, []string{file.ID}, targetName, file.ContentType)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		file = stored
	}
	var err error
	switch {
	case replace && existing.kind == kindFile:
		err = d.mutate(targetParent.pageID, blockOperation(existing.file, fileContent(existing.file.Content, file, targetName)))
	case move && source.pageID == targetParent.pageID:
		// A rename where the file is.
		err = d.mutate(source.pageID, blockOperation(source.file, fileContent(source.file.Content, file, targetName)))
		move = false
	default:
		if replace && existing.kind == kindEmpty {
			d.dropEmpty(existing.pageID, existing.name)
		}
		err = d.appendFile(targetParent.pageID, fileContent(source.file.Content, file, targetName))
	}
	if err == nil && move {
		err = d.mutate(source.pageID, map[string]any{"kind": "deleteBlock", "deletion": map[string]any{"blockId": source.file.ID}})
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return leafen.EndpointAnswer{Status: status}, nil
}

func (d *dav) transferPage(source, sourceParent, targetParent node, targetName string, existing node, replace, move bool, status int) (leafen.EndpointAnswer, error) {
	switch {
	case !move:
		return plain(403, "Copy a page in Leafen, with its content."), nil
	case sourceParent.kind == kindFilesRoot:
		return plain(403, "This folder stays."), nil
	case replace:
		return plain(403, "Leafen won't replace a page; move or remove it first."), nil
	case (targetParent.kind != kindWorkspace && targetParent.kind != kindPage) || targetParent.workspaceID != source.workspaceID:
		return plain(403, "A page moves within its workspace."), nil
	}
	renamed := !same(targetName, source.name)
	err := d.changePage(source.pageID, func(change map[string]any) {
		change["parentId"] = targetParent.pageID
		if renamed {
			change["title"] = targetName
		}
	})
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return leafen.EndpointAnswer{Status: status}, nil
}

// Empty files and locks live in the app's temporary store, an hour at most.

const keptFor = 3600

func emptyKey(pageID string) string { return "empty:" + pageID }

func (d *dav) kv(request leafen.KVRequest) json.RawMessage {
	d.calls++
	request.Tenancy = d.request.Tenancy
	answer, err := leafen.KV(request)
	if err != nil {
		leafen.Log("temporary store: " + err.Error())
	}
	return answer
}

func (d *dav) keepEmpty(pageID, name string) {
	d.kv(leafen.KVRequest{Command: "HSET", Key: emptyKey(pageID), Field: name, Value: "1", TTL: keptFor})
}

func (d *dav) dropEmpty(pageID, name string) {
	d.kv(leafen.KVRequest{Command: "HDEL", Key: emptyKey(pageID), Field: name})
}

func (d *dav) emptyFiles(pageID string) []string {
	if d.action {
		return nil
	}
	return keptNames(d.kv(leafen.KVRequest{Command: "HGETALL", Key: emptyKey(pageID)}))
}
