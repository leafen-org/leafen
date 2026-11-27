package main

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Side files are the files office apps keep beside a document they have
// open: LibreOffice's lock ".~lock.<name>#" and the "lu….tmp" it writes a
// save to first, Microsoft Office's owner file "~$<name>" and its working
// files — Word's "~….tmp", PowerPoint's "ppt….tmp", Excel's "<8 hex>.tmp"
// it renames a document to, and the save Excel writes under eight hex digits
// with no extension ("Cedd4100", Microsoft's KB 814068) — that one only
// while an owner file says Office has a document open on the page, so a
// file of the person's own with such a name stays a file. They live in the
// app's temporary store, an hour at most, not as the page's file blocks: the
// app lists them, reads its lock back as it wrote it and removes them as it
// does on a disk, while the page and its history hold only the document
// (2026-11-12: LibreOffice's lock stood on the page while the document was
// open, and the history kept it; 2026-11-14: Excel's and PowerPoint's saves
// moved the document to a new block at the page's end).
// A block that has such a name from before stays a block. The body a PUT
// gave a side file is the drive's to hold while the side file is there: it
// goes when the app removes the side file, writes it anew or renames it over
// a document (2026-11-14: hidden and side files' bodies stayed stored a day).

var (
	libreOfficeTemporary = regexp.MustCompile(`^lu[0-9]+[0-9a-z]*\.tmp$`)
	officeTemporary      = regexp.MustCompile(`^(ppt[0-9a-f]{1,8}|[0-9a-f]{8})\.tmp$`)
	excelSave            = regexp.MustCompile(`^[0-9a-f]{8}$`)
)

func side(name string) bool {
	lower := strings.ToLower(name)
	return owner(name) || strings.HasPrefix(name, "~") && strings.HasSuffix(lower, ".tmp") ||
		libreOfficeTemporary.MatchString(lower) || officeTemporary.MatchString(lower)
}

// owner is an office app's lock or owner file: who has a document open.
func owner(name string) bool {
	return strings.HasPrefix(name, ".~lock.") && strings.HasSuffix(name, "#") || strings.HasPrefix(name, "~$")
}

// sideIn says a name in a page folder is a side file's: by its name, or as
// Excel's save while Office has a document open there.
func (d *dav) sideIn(parent node, name string) bool {
	if side(name) {
		return true
	}
	if parent.kind != kindPage || !excelSave.MatchString(strings.ToLower(name)) {
		return false
	}
	for _, kept := range d.sideFiles(parent) {
		if strings.HasPrefix(kept.name, "~$") {
			return true
		}
	}
	return false
}

// sideFile is a side file as the temporary store keeps it: the stored file a
// PUT sent, or none for an empty one, and when it came. Body says the file is
// a PUT's body the drive holds for this side file alone.
type sideFile struct {
	File        string `json:"file,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Body        bool   `json:"body,omitempty"`
	// Block is the document's block an app renamed to this name (aside.go).
	Block string `json:"block,omitempty"`
	At    string `json:"at"`
}

func sideKey(pageID string) string { return "side:" + pageID }

func sideOf(file *leafen.StoredFile, name, contentType string, body bool) sideFile {
	kept := sideFile{At: time.Now().UTC().Format(time.RFC3339Nano)}
	if file != nil {
		kept.File, kept.WorkspaceID, kept.Size, kept.SHA256, kept.Body = file.ID, file.WorkspaceID, file.Size, file.SHA256, body
		kept.ContentType = contentTypeOf(contentType, name)
		if kept.ContentType == "application/octet-stream" && file.ContentType != "" {
			kept.ContentType = file.ContentType
		}
	}
	return kept
}

func (d *dav) keepSide(pageID, name string, kept sideFile) {
	raw, _ := json.Marshal(kept)
	d.kv(leafen.KVRequest{Command: "HSET", Key: sideKey(pageID), Field: name, Value: string(raw), TTL: keptFor})
}

// dropSide removes a side file, and lets go of the body it held.
func (d *dav) dropSide(n node) {
	d.kv(leafen.KVRequest{Command: "HDEL", Key: sideKey(n.pageID), Field: n.name})
	d.letGo(n)
}

// letGo lets go of the body a side file held, if it held one.
func (d *dav) letGo(n node) {
	if n.file.Content["body"] == true {
		d.release(n.file.text("fileId"))
	}
}

// sideFiles are a page's side files as its folder lists them; a run of the
// app's tools sees none.
func (d *dav) sideFiles(n node) []node {
	if d.action {
		return nil
	}
	found := []node{}
	for name, value := range keptFields(d.kv(leafen.KVRequest{Command: "HGETALL", Key: sideKey(n.pageID)})) {
		kept := sideFile{}
		if json.Unmarshal([]byte(value), &kept) != nil {
			continue
		}
		content := map[string]any{"name": name}
		if kept.File != "" {
			content["fileId"], content["size"], content["contentType"], content["sha256"] = kept.File, float64(kept.Size), kept.ContentType, kept.SHA256
			content["body"], content["workspaceId"], content["block"] = kept.Body, kept.WorkspaceID, kept.Block
		}
		found = append(found, node{kind: kindSide, name: name, workspaceID: n.workspaceID, pageID: n.pageID,
			file: fileBlock{Content: content}, created: kept.At, updated: kept.At})
	}
	sort.Slice(found, func(a, b int) bool { return found[a].name < found[b].name })
	return found
}

// putSide keeps what a PUT sent to a side file's name, in place of the side
// file there was, if any.
func (d *dav) putSide(parent node, name string, existing node) leafen.EndpointAnswer {
	d.keepSide(parent.pageID, name, sideOf(d.upload(), name, d.request.Headers["content-type"], true))
	d.keptBody = true
	d.dropEmpty(parent.pageID, name)
	d.forget(parent)
	if existing.kind == kindSide {
		d.letGo(existing)
		return leafen.EndpointAnswer{Status: 204}
	}
	return leafen.EndpointAnswer{Status: 201}
}

// storedOf is the stored file a side file holds, if any.
func storedOf(n node) *leafen.StoredFile {
	id := n.file.text("fileId")
	if id == "" {
		return nil
	}
	return &leafen.StoredFile{ID: id, Name: n.name, Size: n.file.size(), ContentType: n.file.text("contentType"), SHA256: n.file.text("sha256")}
}

// transferSide moves or copies a side file: to another side file's name it
// stays aside, a move taking its body along and a copy with one of its own;
// to a document's name — an app that saves by writing aside and renaming —
// its content becomes that file, in the same block when the name is already
// there, and a move lets the body go.
func (d *dav) transferSide(source, targetParent node, targetName string, existing node, replace, move bool, status int) (leafen.EndpointAnswer, error) {
	if targetParent.kind != kindPage {
		return plain(403, "Put files inside a page."), nil
	}
	if replace && existing.folder() {
		return plain(409, "A folder has that name."), nil
	}
	stored := storedOf(source)
	// along: the source's body went with it.
	along := false
	switch {
	case d.sideIn(targetParent, targetName) && !(replace && existing.kind == kindFile):
		kept := sideFileOf(source)
		kept.At = time.Now().UTC().Format(time.RFC3339Nano)
		if !move && stored != nil {
			copied, err := leafen.JoinFilesInto("", []string{stored.ID}, targetName, stored.ContentType)
			if err != nil {
				return leafen.EndpointAnswer{}, err
			}
			kept = sideOf(&copied, targetName, source.file.text("contentType"), true)
		}
		if targetParent.pageID != source.pageID {
			// A document stays held on its own page only.
			kept.Block = ""
		}
		along = move
		d.keepSide(targetParent.pageID, targetName, kept)
	case stored == nil:
		if !(replace && existing.kind == kindFile) {
			d.keepEmpty(targetParent.pageID, targetName)
		}
	case replace && existing.kind == kindFile && stored.ID == existing.file.text("fileId"):
		// The document held aside comes back as it was.
	default:
		contentType := contentTypeOf(stored.ContentType, targetName)
		file, err := leafen.JoinFilesInto(targetParent.workspaceID, []string{stored.ID}, targetName, contentType)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if replace && existing.kind == kindFile {
			err = d.mutate(targetParent.pageID, blockOperation(existing.file, fileContent(existing.file.Content, file, targetName)))
		} else {
			if replace && existing.kind == kindEmpty {
				d.dropEmpty(existing.pageID, existing.name)
			}
			err = d.appendFile(targetParent.pageID, fileContent(nil, file, targetName))
		}
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	// The side file there before goes, with its body.
	if replace && existing.kind == kindSide {
		if existing.name == targetName {
			d.letGo(existing)
		} else {
			d.dropSide(existing)
		}
	}
	if move {
		d.kv(leafen.KVRequest{Command: "HDEL", Key: sideKey(source.pageID), Field: source.name})
		if !along {
			d.letGo(source)
		}
	}
	return leafen.EndpointAnswer{Status: status}, nil
}

// keptFields reads the temporary store's answer to HGETALL: an object, or
// names and values one after another.
func keptFields(raw json.RawMessage) map[string]string {
	fields := map[string]string{}
	if json.Unmarshal(raw, &fields) == nil {
		return fields
	}
	pairs := []string{}
	if json.Unmarshal(raw, &pairs) == nil {
		for index := 0; index+1 < len(pairs); index += 2 {
			fields[pairs[index]] = pairs[index+1]
		}
	}
	return fields
}
