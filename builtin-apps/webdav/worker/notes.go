package main

import (
	"encoding/json"
	"regexp"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Obsidian vaults kept in the drive's own folders (Remotely Save syncs one
// there over WebDAV): an agent lists the vaults, finds notes by name or by
// words in them, and reads a note as written, with its front matter, links
// and tags. Notes stay files; moving them into pages is another tool's.

const (
	// A run has 200 host calls; a search stops short of them.
	notesCallBudget = 170
	noteMaxBytes    = 400 << 10
)

var (
	wikiLink = regexp.MustCompile(`\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|[^\]]*)?\]\]`)
	tagWord  = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}_/-]+)`)
)

// noteDav is the drive as the person sees it, for an action run as them.
func noteDav(call leafen.ActionCall) *dav {
	return &dav{request: leafen.EndpointRequest{Tenancy: call.Tenancy, Person: call.Person, Headers: map[string]string{}}, names: english, action: true}
}

// vault is a folder that holds an Obsidian vault and its path.
type vault struct {
	folder node
	path   []string
}

// vaultFolders are the folders in the drive's own space that hold an
// Obsidian vault (a ".obsidian" folder).
func (d *dav) vaultFolders() ([]vault, error) {
	spaces, err := d.spaces()
	if err != nil {
		return nil, err
	}
	found := []vault{}
	for _, space := range spaces {
		folders, err := d.children(space)
		if err != nil {
			return nil, err
		}
		for _, folder := range folders {
			if !folder.folder() || d.calls > notesCallBudget {
				continue
			}
			inside, err := d.children(folder)
			if err != nil {
				return nil, err
			}
			for _, item := range inside {
				if item.folder() && item.name == ".obsidian" {
					found = append(found, vault{folder: folder, path: []string{d.names.files, space.name, folder.name}})
				}
			}
		}
	}
	return found, nil
}

func fail(message string) int32 { return leafen.Fail(message) }

// vaults lists the person's Obsidian vaults in the drive.
//
//go:wasmexport vaults
func vaults() int32 {
	call, err := leafen.Action(nil)
	if err != nil {
		return fail("the input isn't JSON")
	}
	d := noteDav(call)
	found, err := d.vaultFolders()
	if err != nil {
		return fail(err.Error())
	}
	// The folder's page and workspace too, where the drive's screen reads the
	// vault to move it into pages.
	listed := []map[string]string{}
	for _, found := range found {
		listed = append(listed, map[string]string{"name": found.folder.name, "path": strings.Join(found.path, "/"),
			"pageId": found.folder.pageID, "workspaceId": found.folder.workspaceID})
	}
	_ = leafen.OutputJSON(map[string]any{"vaults": listed})
	return 0
}

type noteHit struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// notes finds notes in a vault, or all of them: by words in their names
// and, when asked, in their text.
//
//go:wasmexport notes
func notes() int32 {
	input := struct {
		Vault string `json:"vault"`
		Query string `json:"query"`
		Text  string `json:"text"`
		Limit int    `json:"limit"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return fail("the input isn't JSON")
	}
	if input.Limit < 1 || input.Limit > 100 {
		input.Limit = 30
	}
	d := noteDav(call)
	roots, err := d.vaultFolders()
	if err != nil {
		return fail(err.Error())
	}
	query, text := strings.ToLower(strings.TrimSpace(input.Query)), strings.ToLower(strings.TrimSpace(input.Text))
	hits := []noteHit{}
	partial := false
	var walk func(folder node, path []string) error
	walk = func(folder node, path []string) error {
		if len(hits) >= input.Limit {
			return nil
		}
		if d.calls > notesCallBudget {
			partial = true
			return nil
		}
		children, err := d.children(folder)
		if err != nil {
			return err
		}
		for _, child := range children {
			if len(hits) >= input.Limit {
				return nil
			}
			childPath := append(append([]string{}, path...), child.name)
			if child.folder() {
				if !strings.HasPrefix(child.name, ".") {
					if err := walk(child, childPath); err != nil {
						return err
					}
				}
				continue
			}
			if child.kind != kindFile || !strings.HasSuffix(strings.ToLower(child.name), ".md") {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(child.name), query) {
				continue
			}
			if text != "" {
				if d.calls > notesCallBudget {
					partial = true
					continue
				}
				body, err := d.fileText(child)
				if err != nil || !strings.Contains(strings.ToLower(body), text) {
					continue
				}
			}
			hits = append(hits, noteHit{Path: strings.Join(childPath, "/"), Name: strings.TrimSuffix(child.name, ".md"), Size: child.file.size()})
		}
		return nil
	}
	for _, root := range roots {
		if input.Vault != "" && !same(root.folder.name, input.Vault) {
			continue
		}
		if err := walk(root.folder, root.path); err != nil {
			return fail(err.Error())
		}
	}
	_ = leafen.OutputJSON(map[string]any{"notes": hits, "partial": partial})
	return 0
}

// fileText reads a file's text.
func (d *dav) fileText(n node) (string, error) {
	d.calls++
	answer, err := leafen.API(leafen.APIRequest{Tenancy: d.request.Tenancy, Method: "GET", Path: "/files/" + n.file.text("fileId")})
	if err != nil {
		return "", err
	}
	if answer.Status >= 300 {
		return "", &refusal{status: answer.Status, text: answer.Text}
	}
	if answer.Text != "" {
		return answer.Text, nil
	}
	var text string
	if json.Unmarshal(answer.Body, &text) == nil {
		return text, nil
	}
	return string(answer.Body), nil
}

// note reads one note: its markdown, front matter, the notes it links to
// and its tags.
//
//go:wasmexport note
func note() int32 {
	input := struct {
		Path string `json:"path"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return fail("the input isn't JSON")
	}
	d := noteDav(call)
	path := segments(input.Path)
	if len(path) < 3 || !(same(path[0], english.files) || same(path[0], korean.files)) {
		return fail("give a note's path as notes lists it, under Files")
	}
	n, err := d.resolve(path)
	if err != nil || n.kind != kindFile {
		return fail("no such note")
	}
	body, err := d.fileText(n)
	if err != nil {
		return fail(err.Error())
	}
	truncated := len(body) > noteMaxBytes
	if truncated {
		body = body[:noteMaxBytes]
	}
	front, rest := frontMatter(body)
	links, tags := []string{}, []string{}
	seen := map[string]bool{}
	for _, match := range wikiLink.FindAllStringSubmatch(rest, -1) {
		if target := strings.TrimSpace(match[1]); !seen["l"+target] {
			seen["l"+target] = true
			links = append(links, target)
		}
	}
	for _, match := range tagWord.FindAllStringSubmatch(rest, -1) {
		if !seen["t"+match[1]] {
			seen["t"+match[1]] = true
			tags = append(tags, match[1])
		}
	}
	_ = leafen.OutputJSON(map[string]any{"path": strings.Join(path, "/"), "markdown": body, "frontMatter": front, "links": links,
		"tags": tags, "truncated": truncated})
	return 0
}

// frontMatter reads a note's leading YAML block of plain "key: value"
// lines.
func frontMatter(text string) (map[string]string, string) {
	fields := map[string]string{}
	body, found := strings.CutPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "---\n")
	if !found {
		return fields, text
	}
	head, rest, closed := strings.Cut(body, "\n---")
	if !closed {
		return fields, text
	}
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && !strings.HasPrefix(line, " ") && strings.TrimSpace(key) != "" {
			fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	_, rest, _ = strings.Cut(rest, "\n")
	return fields, rest
}
