package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The public API's insert_content places its content by an anchor in "v2",
// as Notion's API does (seen 2026-11-22 on a page of its own): position
// {type: after_content or before_content, anchor} places it and refuses as
// notion-update-page's does (notion_page_markdown_anchor.go), each refusal a
// validation_error with its data; in "v1" it is refused, the positions taking
// Notion-flavored Markdown. An anchor is read against where the page's
// blocks stand in the Markdown the update is made from, read again with
// their places marked; a page that changed in between is refused as changed.

// notionMarkdownInsertPosition is where insert_content puts its content.
type notionMarkdownInsertPosition struct {
	Type   string `json:"type"`
	Anchor string `json:"anchor,omitempty"`
	// blocks reads where the page's blocks stand in the Markdown the update
	// is made from (notionMarkdownAnchorBlocksFor); nil where nothing does.
	blocks func(current string) ([]notionMarkdownBlockSpan, error)
}

// notionInsertContentShape is insert_content's request, as Notion's API
// checks it: its content, and a position of one of its kinds, an anchor
// with after_content and before_content.
var notionInsertContentShape = notionObjectOf(false,
	notionField("content", shapeOf(notionString)),
	notionField("position", notionTaggedOf("type",
		notionTagBranch("start"),
		notionTagBranch("end"),
		notionTagBranch("after_content", notionField("anchor", shapeOf(notionString))),
		notionTagBranch("before_content", notionField("anchor", shapeOf(notionString))),
	).opt()))

// notionMarkdownAfterWithPosition refuses insert_content given both a
// selection and a position, as Notion's API does (seen 2026-11-22: "cannot
// both be provided"), in Leafen's words.
const notionMarkdownAfterWithPosition = "insert_content takes after or position, not both: after places the content " +
	"by a selection, position at the start, the end or an anchor."

// notionMarkdownAnchorVersion1 refuses an anchor's position in "v1".
const notionMarkdownAnchorVersion1 = "The `after_content` and `before_content` positions take Notion-flavored Markdown: " +
	"send `markdown_version: \"v2\"`, or use a `start` or `end` position."

// notionMarkdownInsertAnchored is insert_content placed by its position's
// anchor in the page's current Markdown.
func notionMarkdownInsertAnchored(
	current string, input notionPageMarkdownUpdateRequest, value *notionMarkdownInsertContentRequest,
) (string, bool, []notionMarkdownWarning, error) {
	position := value.Position
	if input.MarkdownVersion != notionMarkdownVersion2 {
		return "", false, nil, pageMarkdownMatchError{message: notionMarkdownAnchorVersion1, template: notionMarkdownAnchorVersion1}
	}
	if position.blocks == nil {
		return "", false, nil, pageMarkdownRequestError{message: "insert_content.position.type is invalid."}
	}
	spans, err := position.blocks(current)
	if err != nil {
		return "", false, nil, err
	}
	before := position.Type == "before_content"
	target, err := notionMarkdownAnchorTarget(current, spans, before, position.Anchor)
	if err != nil {
		return "", false, nil, err
	}
	return notionMarkdownPlacedBeside(current, target, before, value.Content), false, nil, nil
}

// notionMarkdownAnchorBlocksFor gives a public API update placed by an anchor
// what reads where the page's blocks stand in its current Markdown.
func notionMarkdownAnchorBlocksFor(
	event *core.RequestEvent, authorization notionPageAuthorization, publicPageID string, input *notionPageMarkdownUpdateRequest,
) {
	if input.InsertContent == nil || input.InsertContent.Position == nil || !notionMarkdownAnchored(input.InsertContent.Position.Type) {
		return
	}
	version := input.MarkdownVersion
	input.InsertContent.Position.blocks = func(current string) ([]notionMarkdownBlockSpan, error) {
		reader, err := notionIdentityPageMarkdownReader(event, authorization.Identity, version, authorization.Page.Page.Id, publicPageID)
		if err != nil {
			return nil, err
		}
		marks := &notionMarkdownBlockMarks{}
		projection := notionPageMarkdownProjection{}
		err = runNotionReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
			var err error
			projection, err = projectNotionPageMarkdownMarked(
				txApp, authorization.Page.Workspace.OwnerID, authorization.Page.Page.Id, publicPageID, reader, "", marks)
			return err
		})
		if err != nil {
			return nil, err
		}
		markdown, spans, read := notionMarkdownBlockSpansOf(projection.Markdown, marks)
		if !read || markdown != current {
			return nil, errNotionPageMarkdownChanged
		}
		return spans, nil
	}
}

// notionMarkdownJoinBlocks puts one Markdown text's blocks after another's,
// on a line of their own.
func notionMarkdownJoinBlocks(first, second string) string {
	if first == "" || second == "" || strings.HasSuffix(first, "\n") || strings.HasPrefix(second, "\n") {
		return first + second
	}
	return first + "\n" + second
}
