package main

import (
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// A document an office app renames to a side file's name on its page is held
// aside: its block stays where it is, out of the folder, and the side file
// reads as the document did; a file that then takes the document's name —
// moved there or written there — is the document's new content, in the same
// block, and the side file stays with the old until the app removes it.
// Windows Word saves so (2026-11-14): the save written to ~WRD….tmp, the
// document renamed to ~WRL….tmp, ~WRD….tmp renamed to the document,
// ~WRL….tmp removed — before, the document left its place for a new block
// at the page's end and the history kept three changes. Removing the side
// file while it holds the document removes the document, as on a disk; an
// hour on, the hold ends and the document is back under its name.
//
// Excel and PowerPoint save the other way Microsoft documents (KB 814068, KB
// 102888): the save written to a temporary file beside the document, the
// document deleted, the temporary renamed to its name. A document deleted
// while such a save waits on its page, written in the last two minutes, is
// held by that save as a rename holds it: out of the folder, its block where
// it is, the save then taking its name as its new content. Removing the save
// removes the document, as the delete asked; a save never renamed lets the
// document come back after the hour, where a disk would have lost it.

// holdAside renames a document to a side file's name on its page.
func (d *dav) holdAside(document node, name string) {
	d.keepSide(document.pageID, name, sideFile{File: document.file.text("fileId"), WorkspaceID: document.workspaceID,
		Size: document.file.size(), ContentType: document.file.text("contentType"), SHA256: document.file.text("sha256"),
		Block: document.file.ID, At: time.Now().UTC().Format(time.RFC3339Nano)})
}

// savingFor is how recent a save beside a document is for its delete to wait.
const savingFor = 2 * time.Minute

// saving is the save an office app wrote last on the page, within savingFor:
// a side file with content, not an owner or lock file, holding no document.
func (d *dav) saving(parent node) (node, bool) {
	latest, latestAt, found := node{}, time.Time{}, false
	for _, kept := range d.sideFiles(parent) {
		at, err := time.Parse(time.RFC3339Nano, kept.created)
		if err != nil || owner(kept.name) || kept.file.text("fileId") == "" || kept.file.text("block") != "" ||
			time.Since(at) > savingFor || found && !at.After(latestAt) {
			continue
		}
		latest, latestAt, found = kept, at, true
	}
	return latest, found
}

// holdDeleted holds a document a client deleted by the save waiting beside it.
func (d *dav) holdDeleted(save, document node) {
	kept := sideFileOf(save)
	kept.Block = document.file.ID
	d.keepSide(save.pageID, save.name, kept)
}

// held finds the document a side file in the folder holds aside under name,
// and that side file.
func (d *dav) held(parent node, name string) (document, holder node, found bool, err error) {
	children, err := d.children(parent)
	if err != nil {
		return node{}, node{}, false, err
	}
	for _, child := range children {
		if child.kind == kindSide && child.aside != nil && same(cleanName(child.aside.text("name"), "file"), name) {
			document = node{kind: kindFile, name: name, workspaceID: child.workspaceID, pageID: child.pageID, file: *child.aside,
				created: child.aside.Created, updated: child.aside.Updated}
			return document, child, true, nil
		}
	}
	return node{}, node{}, false, nil
}

// unhold gives a held document its place in the folder again.
func (d *dav) unhold(holder node) {
	kept := sideFileOf(holder)
	kept.Block = ""
	d.keepSide(holder.pageID, holder.name, kept)
}

// sideFileOf is a listed side file as the temporary store keeps it.
func sideFileOf(n node) sideFile {
	return sideFile{File: n.file.text("fileId"), WorkspaceID: n.file.text("workspaceId"), Size: n.file.size(), ContentType: n.file.text("contentType"),
		SHA256: n.file.text("sha256"), Body: n.file.Content["body"] == true, Block: n.file.text("block"), At: n.created}
}

// asideTo moves or copies a document to a side file's name: on its page a
// move holds it aside, elsewhere or as a copy the side file has a copy of
// its content.
func (d *dav) asideTo(source, targetParent node, targetName string, move bool, status int) (leafen.EndpointAnswer, error) {
	if move && targetParent.pageID == source.pageID {
		d.holdAside(source, targetName)
		return leafen.EndpointAnswer{Status: status}, nil
	}
	copied, err := leafen.JoinFilesInto("", []string{source.file.text("fileId")}, targetName, source.file.text("contentType"))
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	d.keepSide(targetParent.pageID, targetName, sideOf(&copied, targetName, source.file.text("contentType"), true))
	if move {
		if err := d.mutate(source.pageID, map[string]any{"kind": "deleteBlock", "deletion": map[string]any{"blockId": source.file.ID}}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	return leafen.EndpointAnswer{Status: status}, nil
}
