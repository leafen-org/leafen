package app

import (
	"bytes"
	"io"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// A page metadata change changes what it sends and leaves the rest as the
// page has it, as Notion's PATCH /v1/pages/{id} changes only the properties,
// icon and cover sent (2026-11-13: an agent's change of small text alone
// emptied the page's title, took it out of its parent to the top of the
// workspace, and brought a page in the trash back). Its title, parent and
// trash state, which the change writes every time, are kept when the
// request leaves them out or sends them null, as a teamspace, an icon or a
// style left out is; the screen sends all three. Title spans sent alone are
// the title they spell. Teamspace sent without a parent names the top of it,
// as it did. The request hash tells a field left out from one sent empty, so
// a retry of the same change after another rename is still that change.

// rereadableRequestBody puts a request's body back as the server's own body
// that reads itself again, so it binds twice by the same rules: into a change
// and into which of its fields the request sent. A key's checks may have read
// it before and put back a body that reads only once (readUserKeyBody;
// 2026-11-13: every agent change was refused for a body read twice).
func rereadableRequestBody(event *core.RequestEvent) error {
	if event.Request.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(event.Request.Body)
	if err != nil {
		return err
	}
	event.Request.Body = &router.RereadableReadCloser{ReadCloser: io.NopCloser(bytes.NewReader(raw))}
	return nil
}

// pageMetadataSentFields is which of a change's always-written fields its
// request sent, read from the body by the same rules as the change.
type pageMetadataSentFields struct {
	Title    *string `json:"title"`
	ParentID *string `json:"parentId"`
	InTrash  *bool   `json:"inTrash"`
}

// pageMetadataInputSpellingTitle is a change whose title spans were sent
// without words: the words they spell are its title.
func pageMetadataInputSpellingTitle(input pageMetadataInput, sent pageMetadataSentFields) (pageMetadataInput, pageMetadataSentFields) {
	if sent.Title == nil && input.TitleSpans != nil {
		input.Title = spelledName(*input.TitleSpans)
		sent.Title = &input.Title
	}
	return input, sent
}

// leftOut is "" for a request that sent every always-written field, and
// otherwise names those it left out, for its request hash.
func (sent pageMetadataSentFields) leftOut() string {
	if sent.Title != nil && sent.ParentID != nil && sent.InTrash != nil {
		return ""
	}
	marks := "\x00left-out-v1\x00"
	for _, field := range []struct {
		name string
		left bool
	}{{"title", sent.Title == nil}, {"parent", sent.ParentID == nil}, {"trash", sent.InTrash == nil}} {
		if field.left {
			marks += field.name + ";"
		}
	}
	return marks
}

// pageMetadataInputKeepingUnsent is the change with the page's own title,
// parent and trash state where its request left them out, read in the
// change's transaction; a page not found is left for the change to answer.
func pageMetadataInputKeepingUnsent(
	app core.App, pageID string, input pageMetadataInput, sent pageMetadataSentFields,
) pageMetadataInput {
	if sent.leftOut() == "" {
		return input
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return input
	}
	if sent.Title == nil {
		input.Title = page.GetString("title")
	}
	if sent.ParentID == nil && input.TeamspaceID == nil {
		input.ParentID = page.GetString("parent_id")
	}
	if sent.InTrash == nil {
		input.InTrash = page.GetBool("in_trash")
	}
	return input
}
