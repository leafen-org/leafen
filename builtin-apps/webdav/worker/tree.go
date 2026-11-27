package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
	"leafen.org/leafen/sdk/app/go/leafen"
)

// The tree a WebDAV client sees: "Pages" holds the person's workspaces and,
// under each, its pages as folders, a page's files as files; "Files" holds
// the app's own space — the person's folder and the organization's shared
// one, pages in the app's workspace. Names come from titles; two alike in a
// folder get " (2)".

type kind int

const (
	kindRoot kind = iota
	kindPagesRoot
	kindFilesRoot
	kindWorkspace
	kindPage
	kindFile
	// kindEmpty is a file a client made with nothing in it yet, kept in the
	// app's temporary store until its content comes.
	kindEmpty
	// kindSide is an office app's lock or working file, kept in the app's
	// temporary store (side.go).
	kindSide
)

type node struct {
	kind        kind
	name        string
	workspaceID string
	// pageID is the folder's page, or the page that holds a file.
	pageID           string
	file             fileBlock
	created, updated string
	// aside is the document's block a side file holds aside (aside.go).
	aside *fileBlock
}

func (n node) folder() bool { return n.kind != kindFile && n.kind != kindEmpty && n.kind != kindSide }

// fileBlock is a block that holds a file, as the page's content keeps it,
// with when the block was made and last changed, and when the file's sender
// last changed it, if it said.
type fileBlock struct {
	ID, Type, AfterID, Position string
	ParentID                    *string
	Content                     map[string]any
	Created, Updated, Modified  string
}

func (b fileBlock) text(key string) string {
	value, _ := b.Content[key].(string)
	return value
}

func (b fileBlock) size() int64 {
	value, _ := b.Content["size"].(float64)
	return int64(value)
}

var errNotFound = errors.New("not found")

// names are the top folders' names, by the client's language; either is
// understood.
type names struct{ pages, files, mine, shared, untitled string }

var (
	korean  = names{pages: "페이지", files: "파일", mine: "내 파일", shared: "공유 파일", untitled: "제목 없음"}
	english = names{pages: "Pages", files: "Files", mine: "My files", shared: "Shared files", untitled: "Untitled"}
)

// same compares names as file systems do: case and Unicode form aside.
func same(a, b string) bool { return strings.EqualFold(norm.NFC.String(a), norm.NFC.String(b)) }

// cleanName is a title as a file name: one line, no slash.
func cleanName(title, untitled string) string {
	name := strings.Join(strings.Fields(norm.NFC.String(title)), " ")
	name = strings.NewReplacer("/", "／", "\\", "＼").Replace(name)
	if name == "" || name == "." || name == ".." {
		return untitled
	}
	return name
}

// numbered gives the second and later of a name " (2)", " (3)", before a
// file's extension.
func numbered(name string, index int, file bool) string {
	if index < 2 {
		return name
	}
	stem, ext := name, ""
	if file {
		if dot := strings.LastIndex(name, "."); dot > 0 {
			stem, ext = name[:dot], name[dot:]
		}
	}
	return fmt.Sprintf("%s (%d)%s", stem, index, ext)
}

// cache keeps a folder's listing a few seconds: a client asks for the same
// folder many times in a row. Requests reach several worker processes at
// once, so a change counts up the folder's version in the app's temporary
// store, and a listing kept from before it isn't used.
type cached struct {
	at       time.Time
	version  string
	children []node
}

var listings = map[string]cached{}

const listingFresh = 3 * time.Second

// cacheKey names a folder's listing in the client's language: the names in
// it (the top folders', "제목 없음") are that language's, and a client asking
// in the other must not get them (2026-11-10).
func (d *dav) cacheKey(n node) string { return cacheKeyIn(d.request.Person, n, d.names) }

func cacheKeyIn(person string, n node, language names) string {
	return person + "\x00" + language.pages + "\x00" + fmt.Sprint(n.kind) + "\x00" + n.workspaceID + "\x00" + n.pageID
}

func versionKey(n node) string { return "version:" + n.workspaceID + ":" + n.pageID }

// version says the folder's version; known is false when the store didn't
// answer, and a listing then can't be told from one kept from before a change.
func (d *dav) version(n node) (version string, known bool) {
	if d.action || (n.kind != kindWorkspace && n.kind != kindPage) {
		return "", true
	}
	answer := d.kv(leafen.KVRequest{Command: "GET", Key: versionKey(n)})
	return string(answer), answer != nil
}

func (d *dav) forget(n node) {
	for _, language := range []names{korean, english} {
		delete(listings, cacheKeyIn(d.request.Person, n, language))
	}
	if n.kind == kindWorkspace || n.kind == kindPage {
		d.kv(leafen.KVRequest{Command: "INCR", Key: versionKey(n), TTL: keptFor})
	}
}

// children lists a folder.
func (d *dav) children(n node) ([]node, error) {
	key := d.cacheKey(n)
	version, known := d.version(n)
	if hit, ok := listings[key]; known && ok && time.Since(hit.at) < listingFresh && hit.version == version {
		return hit.children, nil
	}
	var found []node
	var err error
	switch n.kind {
	case kindRoot:
		found = []node{{kind: kindPagesRoot, name: d.names.pages}, {kind: kindFilesRoot, name: d.names.files}}
	case kindPagesRoot:
		found, err = d.workspaces()
	case kindFilesRoot:
		found, err = d.spaces()
	case kindWorkspace, kindPage:
		found, err = d.pageFolder(n)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Two alike get numbers, in the order the page lists them. Each name is
	// normalized once and compared only with those before it that fold
	// alike: normalizing both names of every pair took a folder of a
	// thousand pages seconds (2026-11-24).
	alike := map[string][]string{}
	for index := range found {
		name := norm.NFC.String(found[index].name)
		key := strings.ToLower(strings.ToUpper(name))
		count := 1
		for _, before := range alike[key] {
			if strings.EqualFold(before, name) {
				count++
			}
		}
		alike[key] = append(alike[key], name)
		found[index].name = numbered(found[index].name, count, !found[index].folder())
	}
	if known {
		listings[key] = cached{at: time.Now(), version: version, children: found}
	}
	return found, nil
}

// resolve finds the node a path names.
func (d *dav) resolve(segments []string) (node, error) {
	current := node{kind: kindRoot}
	for index, segment := range segments {
		if index == 0 {
			switch {
			case same(segment, korean.pages) || same(segment, english.pages):
				current = node{kind: kindPagesRoot, name: d.names.pages}
				continue
			case same(segment, korean.files) || same(segment, english.files):
				current = node{kind: kindFilesRoot, name: d.names.files}
				continue
			}
			return node{}, errNotFound
		}
		if index == 1 && current.kind == kindFilesRoot {
			spaces, err := d.spaces()
			if err != nil {
				return node{}, err
			}
			matched := false
			for _, space := range spaces {
				if same(segment, space.name) || same(segment, korean.mine) && space.name == d.names.mine ||
					same(segment, english.mine) && space.name == d.names.mine || same(segment, korean.shared) && space.name == d.names.shared ||
					same(segment, english.shared) && space.name == d.names.shared {
					current, matched = space, true
				}
			}
			if !matched {
				return node{}, errNotFound
			}
			continue
		}
		children, err := d.children(current)
		if err != nil {
			return node{}, err
		}
		matched := false
		for _, child := range children {
			if same(child.name, segment) {
				current, matched = child, true
				break
			}
		}
		if !matched || (!current.folder() && index < len(segments)-1) {
			return node{}, errNotFound
		}
	}
	return current, nil
}

// workspaces are the person's workspaces in the organization, not apps'.
func (d *dav) workspaces() ([]node, error) {
	answer, err := d.api("GET", "/app-workspaces", "", nil)
	if err != nil {
		return nil, err
	}
	listed := struct {
		Workspaces []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			App      bool   `json:"app"`
			OtherApp bool   `json:"otherApp"`
		} `json:"workspaces"`
	}{}
	if err := json.Unmarshal(answer, &listed); err != nil {
		return nil, err
	}
	found := []node{}
	for _, workspace := range listed.Workspaces {
		if !workspace.App && !workspace.OtherApp {
			found = append(found, node{kind: kindWorkspace, name: cleanName(workspace.Name, d.names.untitled), workspaceID: workspace.ID})
		}
	}
	return found, nil
}

// spaces are the app's own folders: the person's and the shared one.
func (d *dav) spaces() ([]node, error) {
	answer, err := d.api("GET", "/app-data", "", nil)
	if err != nil {
		return nil, err
	}
	data := struct {
		WorkspaceID string `json:"workspaceId"`
		Items       []struct {
			Key    string `json:"key"`
			PageID string `json:"pageId"`
		} `json:"items"`
	}{}
	if err := json.Unmarshal(answer, &data); err != nil {
		return nil, err
	}
	found := []node{}
	for _, item := range data.Items {
		switch {
		case item.Key == "mine" && item.PageID != "":
			found = append(found, node{kind: kindPage, name: d.names.mine, workspaceID: data.WorkspaceID, pageID: item.PageID})
		case item.Key == "shared" && item.PageID != "":
			found = append(found, node{kind: kindPage, name: d.names.shared, workspaceID: data.WorkspaceID, pageID: item.PageID})
		}
	}
	return found, nil
}

// pageFolder is a workspace's top or a page: its child pages, then the
// files its content holds but those held aside, then files a client made
// empty, then office apps' side files.
func (d *dav) pageFolder(n node) ([]node, error) {
	found, err := d.childPages(n)
	if err != nil {
		return nil, err
	}
	if n.kind != kindPage {
		return found, nil
	}
	// A page's files carry its times; Leafen keeps none for a block.
	if n.updated == "" {
		if raw, err := d.api("GET", "/pages/"+n.pageID, "", nil); err == nil {
			page := struct {
				Created string `json:"created"`
				Updated string `json:"updated"`
			}{}
			if json.Unmarshal(raw, &page) == nil {
				n.created, n.updated = page.Created, page.Updated
			}
		}
	}
	blocks, _, err := d.content(n.pageID)
	if err != nil {
		return nil, err
	}
	sides := d.sideFiles(n)
	holders := map[string]int{}
	for index, side := range sides {
		if block := side.file.text("block"); block != "" {
			holders[block] = index
		}
	}
	for _, block := range blocks {
		if block.text("fileId") == "" {
			continue
		}
		// A document held aside is its side file's.
		if index, held := holders[block.ID]; held {
			kept := block
			sides[index].aside = &kept
			continue
		}
		// A file's times are its block's, so changing one file leaves the
		// others' as they were; a server too old to say keeps the page's. A
		// sync tool's own time for the file, given as it sent it, is the
		// file's: it reads the file back as unchanged.
		created, updated := block.Created, block.Updated
		if updated == "" {
			created, updated = n.created, n.updated
		}
		if block.Modified != "" {
			updated = block.Modified
		}
		found = append(found, node{kind: kindFile, name: cleanName(block.text("name"), "file"), workspaceID: n.workspaceID,
			pageID: n.pageID, file: block, created: created, updated: updated})
	}
	for _, name := range d.emptyFiles(n.pageID) {
		found = append(found, node{kind: kindEmpty, name: name, workspaceID: n.workspaceID, pageID: n.pageID})
	}
	return append(found, sides...), nil
}

// fileTypes are the blocks that hold a file.
var fileTypes = map[string]bool{"file": true, "image": true, "pdf": true, "video": true, "audio": true}

// content reads a page's file blocks, every window of them, and its last
// top-level block, after which a new file goes.
func (d *dav) content(pageID string) (files []fileBlock, last *fileBlock, err error) {
	type wireBlock struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		ParentID   *string        `json:"parentId"`
		Position   string         `json:"position"`
		Content    map[string]any `json:"content"`
		CreatedAt  string         `json:"createdAt"`
		UpdatedAt  string         `json:"updatedAt"`
		ModifiedAt string         `json:"modifiedAt"`
	}
	block := func(raw wireBlock) fileBlock {
		found := fileBlock{ID: raw.ID, Type: raw.Type, ParentID: raw.ParentID, Position: raw.Position, Content: raw.Content,
			Created: raw.CreatedAt, Updated: raw.UpdatedAt, Modified: raw.ModifiedAt}
		if found.Content == nil {
			found.Content = map[string]any{}
		}
		return found
	}
	cursor := ""
	for {
		query := ""
		if cursor != "" {
			query = "cursor=" + url.QueryEscape(cursor)
		}
		answer, err := d.api("GET", "/pages/"+pageID+"/content-graph/file-blocks", query, nil)
		if err != nil {
			return nil, nil, err
		}
		page := struct {
			Blocks     []wireBlock `json:"blocks"`
			Last       *wireBlock  `json:"last"`
			NextCursor string      `json:"nextCursor"`
		}{}
		if err := json.Unmarshal(answer, &page); err != nil {
			return nil, nil, err
		}
		for _, raw := range page.Blocks {
			if fileTypes[raw.Type] {
				files = append(files, block(raw))
			}
		}
		if page.Last != nil && cursor == "" {
			found := block(*page.Last)
			last = &found
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return files, last, nil
		}
		cursor = page.NextCursor
	}
}

// childPages are the pages right under a workspace's top or a page, a
// window at a time to the last: taking the first thousand for all left a
// folder's pages past them out of Finder and Explorer (2026-11-24).
func (d *dav) childPages(n node) ([]node, error) {
	found := []node{}
	cursor := ""
	for {
		query := url.Values{"limit": {"1000"}}
		if n.pageID != "" {
			query.Set("parent", n.pageID)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		answer, err := d.api("GET", "/workspaces/"+n.workspaceID+"/page-children", query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		listed := struct {
			Pages []struct {
				ID      string `json:"id"`
				Title   string `json:"title"`
				Created string `json:"created"`
				Updated string `json:"updated"`
			} `json:"pages"`
			NextCursor string `json:"nextCursor"`
		}{}
		if err := json.Unmarshal(answer, &listed); err != nil {
			return nil, err
		}
		for _, page := range listed.Pages {
			found = append(found, node{kind: kindPage, name: cleanName(page.Title, d.names.untitled), workspaceID: n.workspaceID,
				pageID: page.ID, created: page.Created, updated: page.Updated})
		}
		if listed.NextCursor == "" || listed.NextCursor == cursor {
			return found, nil
		}
		cursor = listed.NextCursor
	}
}
