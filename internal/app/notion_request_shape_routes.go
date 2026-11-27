package app

import (
	"net/http"
	"strings"
)

// notionRouteShape is what one public API endpoint checks before it looks
// anything up, as Notion checks it (each line measured against Notion's API
// on 2026-11-07, 11-10 and 11-21): its query, its body, and the refusal, in
// Leafen's words (notion_request_fault_words.go), of what the shape lets
// through but the endpoint refuses. A nil query or body isn't checked here;
// what Notion checks once it found the target its handler checks.
type notionRouteShape struct {
	method string
	path   string // segments after /v1, "*" for an ID
	scope  string // without it refuseNotionRequest answers 403 before any check
	query  *notionShape
	body   *notionShape
	after  func(body, query *notionValue) string
	// multipart is a body sent as a form with a file, Notion's "body.file".
	multipart bool
	// patRefused is an endpoint Notion keeps from personal access tokens,
	// refused as one the token's capabilities don't reach.
	patRefused bool
}

var (
	notionNoQuery       = notionObjectOf(true)
	notionPageSizeQuery = shapeOf(notionNumber).atLeast(1).atMost(100).opt()
	notionCursorQuery   = shapeOf(notionUUID).opt()
	notionAnyShape      = shapeOf(notionAny)
	notionMaybeBool     = shapeOf(notionBoolean).opt()
	notionMaybeString   = shapeOf(notionString).opt()
	notionMaybeObject   = shapeOf(notionObject).opt()
	notionMaybeUUID     = shapeOf(notionUUID).opt()
	notionMaybeArray    = notionArrayOf(nil).opt()
	// notionMarkdownVersionShape is the page Markdown's version a request
	// names (notionMarkdownVersion2).
	notionMarkdownVersionShape = notionEnumOf("v1", notionMarkdownVersion2).opt()
	// notionStartCursor is a list's start_cursor where Notion checks it is a
	// UUID: written with its dashes, in either case (seen 2026-11-12: one
	// without them refused as no valid uuid, an upper-case one let through to
	// the cursor refusal, notionStartCursorInvalid), or Leafen's own ID.
	notionStartCursor = (&notionShape{kind: notionString, accepts: notionStartCursorText, said: "a valid uuid"}).opt()
)

func notionParentBranch(key string, value *notionShape) *notionShape {
	return notionObjectOf(true, notionField(key, value), notionField("type", notionEnumOf(key).opt()))
}

func notionOpenParentBranch(key string) *notionShape {
	return notionObjectOf(false, notionField(key, shapeOf(notionUUID)), notionField("type", notionEnumOf(key).opt()))
}

func notionTagBranch(tag string, fields ...notionShapeField) *notionShape {
	return notionObjectOf(false, append([]notionShapeField{notionField("type", notionEnumOf(tag))}, fields...)...)
}

// notionDataSourceFilterShape is a filter's form as Notion's API checks it
// before it reads anything (seen 2026-11-12): a compound of others, "or"
// before "and", each taking other keys beside it, which the filter's reading
// leaves; a property's condition, named by its kind's key beside the
// property and nothing else; or a page timestamp's beside "timestamp"
// naming it. Each alternative is checked key by key in that order, and the
// keys it doesn't take are named all together. A filter within a compound
// lists the compounds last, and one within two takes none.
var notionDataSourceFilterShape = func() *notionShape {
	conditions := []*notionShape{}
	for _, kind := range notionFilterShapeKinds {
		conditions = append(conditions, notionObjectOf(true,
			notionField(kind, shapeOf(notionObject)), notionField("property", shapeOf(notionString))))
	}
	for _, stamp := range []string{"created_time", "last_edited_time"} {
		conditions = append(conditions, notionObjectOf(true,
			notionField(stamp, shapeOf(notionObject)), notionField("timestamp", notionEnumOf(stamp))))
	}
	compounds := func(items *notionShape) []*notionShape {
		return []*notionShape{
			notionObjectOf(false, notionField("or", notionArrayOf(items))),
			notionObjectOf(false, notionField("and", notionArrayOf(items))),
		}
	}
	inner := notionUnionOf(conditions...)
	nested := notionUnionOf(append(append([]*notionShape{}, conditions...), compounds(inner)...)...)
	return notionUnionOf(append(compounds(nested), conditions...)...)
}()

// notionQuickFilterShape is a quick filter's form, as Notion's API checks
// it (seen 2026-11-12): null, an empty condition, or a condition named by
// its kind's key alone.
var notionQuickFilterShape = func() *notionShape {
	branches := []*notionShape{notionObjectOf(true)}
	for _, kind := range notionFilterShapeKinds {
		branches = append(branches, notionObjectOf(true, notionField(kind, shapeOf(notionObject))))
	}
	return notionUnionOf(branches...).orNull()
}()

// notionFilterShapeKinds are the keys a filter names its kind by, in the
// order Notion's API checks them.
var notionFilterShapeKinds = append(append([]string{}, notionViewFilterKeys...), "verification")

var notionPageParentShape = notionUnionOf(
	notionParentBranch("page_id", shapeOf(notionUUID)),
	notionParentBranch("database_id", shapeOf(notionUUID)),
	notionParentBranch("data_source_id", shapeOf(notionUUID)),
	notionParentBranch("workspace", shapeOf(notionTrue)),
)

// An icon or a cover is one of its kinds, each its key holding what that kind
// holds and a type naming that kind or left out; Notion checks it before it
// looks anything up, each kind in its own order (seen 2026-11-10: a page's
// icon emoji, external, custom_emoji, file_upload, icon, a data source's
// file_upload first, a cover external and file_upload; 2026-11-14: a
// database's, made or changed, as a data source's but never null).
func notionIconKind(kind string, value *notionShape) *notionShape {
	return notionObjectOf(false, notionField(kind, value), notionField("type", notionEnumOf(kind).opt()))
}

var (
	notionIconEmoji       = notionIconKind("emoji", shapeOf(notionEmoji))
	notionIconExternal    = notionIconKind("external", notionObjectOf(false, notionField("url", shapeOf(notionString))))
	notionIconCustomEmoji = notionIconKind("custom_emoji", notionObjectOf(false, notionField("id", shapeOf(notionUUID))))
	notionIconFileUpload  = notionIconKind("file_upload", notionObjectOf(false, notionField("id", shapeOf(notionUUID))))
	notionIconBuiltIn     = notionIconKind("icon", notionObjectOf(false, notionField("name", shapeOf(notionString))))

	notionPageIconShape = notionUnionOf(
		notionIconEmoji, notionIconExternal, notionIconCustomEmoji, notionIconFileUpload, notionIconBuiltIn,
	).orNull().opt()
	notionDataSourceIconShape = notionUnionOf(
		notionIconFileUpload, notionIconEmoji, notionIconExternal, notionIconCustomEmoji, notionIconBuiltIn,
	).orNull().opt()
	// A database's is a data source's, but never null (seen 2026-11-14: made
	// or changed, "body.icon should be an object or `undefined`").
	notionDatabaseIconShape = notionUnionOf(
		notionIconFileUpload, notionIconEmoji, notionIconExternal, notionIconCustomEmoji, notionIconBuiltIn,
	).opt()
	notionPageCoverShape = notionUnionOf(notionIconExternal, notionIconFileUpload).orNull().opt()
)

var notionPageTemplateShape = notionTaggedOf("type",
	notionTagBranch("none"),
	notionTagBranch("default"),
	notionTagBranch("template_id", notionField("template_id", shapeOf(notionUUID))),
)

// A page update applies a default or a named template; "none" is a page
// create's alone (seen 2026-11-14: an update's "none" is refused by the
// types it takes).
var notionPageUpdateTemplateShape = notionTaggedOf("type",
	notionTagBranch("default"),
	notionTagBranch("template_id", notionField("template_id", shapeOf(notionUUID))),
)

// A position names its place by its type alone: Notion reads a branch's own
// key, and takes any other beside it, only for "after_block", whose block it
// names by a UUID (seen 2026-11-14: "page_start" with a page_start of null, a
// string or none, or an after_block beside it, all go first).
var notionAfterBlockShape = notionObjectOf(false, notionField("id", shapeOf(notionUUID)))

var notionPagePositionShape = notionTaggedOf("type",
	notionTagBranch("after_block", notionField("after_block", notionAfterBlockShape)),
	notionTagBranch("page_start"),
	notionTagBranch("page_end"),
)

var notionAppendPositionShape = notionTaggedOf("type",
	notionTagBranch("after_block", notionField("after_block", notionAfterBlockShape)),
	notionTagBranch("start"),
	notionTagBranch("end"),
)

var notionSearchBodyShape = notionObjectOf(true,
	notionField("query", notionMaybeString),
	notionField("sort", notionUnionOf(
		notionObjectOf(true,
			notionField("timestamp", notionMaybeString),
			notionField("direction", notionEnumOf("ascending", "descending").opt())),
		notionObjectOf(true,
			notionField("property", shapeOf(notionString)),
			notionField("direction", notionEnumOf("ascending", "descending").opt())),
	).opt()),
	notionField("filter", notionUnionOf(
		notionObjectOf(true,
			notionField("property", notionEnumOf("object")),
			notionField("value", notionEnumOf("page", "data_source")),
			notionField("in_trash", notionMaybeBool)),
		notionObjectOf(true, notionField("in_trash", shapeOf(notionBoolean))),
	).opt()),
	notionField("start_cursor", notionStartCursor),
	notionField("page_size", shapeOf(notionNumber).opt()),
)

// notionSearchSortSaid is what Notion says of a sort its shape lets through.
func notionSearchSortSaid(body, _ *notionValue) string {
	sort := body.field("sort")
	if sort == nil {
		return ""
	}
	if property := sort.field("property"); property != nil {
		if property.text != "relevance" {
			return notionFaultLine("body.sort.property", []string{`"relevance"`}, property.text)
		}
		return ""
	}
	timestamp, direction := sort.field("timestamp"), sort.field("direction")
	switch {
	case timestamp == nil && direction == nil:
		return "body.sort is `{ \"property\": \"relevance\" }` or `{ \"timestamp\": \"last_edited_time\", \"direction\": \"ascending\" | \"descending\" }`."
	case timestamp == nil || timestamp.text != "last_edited_time":
		return "A sort by timestamp takes body.sort.timestamp \"last_edited_time\"."
	case direction == nil:
		return "body.sort.direction is needed with body.sort.timestamp \"last_edited_time\"."
	}
	return ""
}

const notionMarkdownVersionWithoutMarkdown = "`markdown_version` goes only with `markdown`: send `markdown`, or leave `markdown_version` out."

// notionPageCreateSaid is what Notion says of a page's parameters that go
// only with its Markdown, given without it, before it reads the rest
// (seen 2026-11-10).
func notionPageCreateSaid(body, _ *notionValue) string {
	if body.field("markdown") != nil {
		return ""
	}
	if body.field("allow_async") != nil {
		return "`allow_async` goes only with `markdown`."
	}
	if body.field("markdown_version") != nil {
		return notionMarkdownVersionWithoutMarkdown
	}
	return ""
}

// notionPageUpdateSaid is what Notion says of an update applying a template
// beside an icon or a cover, which it takes only in a request of their own
// (seen 2026-11-14: any template — a default one on a page, one it can't
// find — and an icon of null too; the icon is said first). A page made from
// a template takes both.
func notionPageUpdateSaid(body, _ *notionValue) string {
	if template := body.field("template"); template == nil || template.kind != 'o' {
		return ""
	}
	for _, name := range []string{"icon", "cover"} {
		if body.field(name) != nil {
			return "'" + name + "' doesn't go with a template: set the " + name + " in a request of its own."
		}
	}
	return ""
}

func notionViewListSaid(_, query *notionValue) string {
	if query.field("database_id") == nil && query.field("data_source_id") == nil {
		return notionViewListNeedsID
	}
	return ""
}

// notionViewCreateShape is a new view's body as Notion checks it (seen
// 2026-11-12), in its order: the IDs it names, each a UUID, the data
// source's first; its name and type; then its settings and where it goes, a
// tab after another naming that view.
var notionViewCreateShape = notionObjectOf(false,
	notionField("data_source_id", shapeOf(notionUUID)),
	notionField("database_id", notionMaybeUUID),
	notionField("view_id", notionMaybeUUID),
	notionField("name", shapeOf(notionString)),
	notionField("type", notionEnumOf("table", "board", "list", "calendar", "timeline", "gallery", "form", "chart", "map", "dashboard")),
	notionField("filter", notionMaybeObject),
	notionField("sorts", notionMaybeArray),
	notionField("quick_filters", notionMaybeObject),
	notionField("configuration", notionMaybeObject),
	notionField("create_database", notionMaybeObject),
	notionField("position", notionTaggedOf("type",
		notionTagBranch("start"),
		notionTagBranch("end"),
		notionTagBranch("after_view", notionField("view_id", shapeOf(notionUUID))),
	).opt()))

// notionViewCreateSaid is what Notion says of a new view's body before it
// looks anything up (seen 2026-11-12): it goes into one database, dashboard
// or new database, and only a dashboard's widget takes a placement.
func notionViewCreateSaid(body, _ *notionValue) string {
	given := 0
	for _, key := range []string{"database_id", "view_id", "create_database"} {
		if body.field(key) != nil {
			given++
		}
	}
	if given != 1 {
		return "Give one of database_id, view_id and create_database, and only one."
	}
	if body.field("placement") != nil && body.field("database_id") != nil {
		return "placement goes only with view_id (for a dashboard's widgets), not with database_id."
	}
	return ""
}

// A comment's rich text is a hundred items at most, as a block's, each
// checked as a comment's (notionRichTextItem); none at all is a comment of
// no words (seen 2026-11-11).
var notionCommentRichTextShape = notionArrayOf(notionRichTextItem(true)).atMostItems(100)

// notionCommentCreateShape is a new comment's body as Notion checks it: a
// page or block parent, or a discussion, each with rich_text or markdown; a
// body with neither target is told it needs one (body.parent missing and
// body.discussion_id missing, as choices). Beside
// them, up to three attachments, each a file upload's ID, and the name it
// shows: the integration's, its person's or one of its own of a character
// or more. Other keys are left (seen 2026-11-11).
var notionCommentCreateShape = func() *notionShape {
	parent := notionUnionOf(
		notionParentBranch("page_id", shapeOf(notionUUID)),
		notionParentBranch("block_id", shapeOf(notionUUID)),
	)
	discussion := shapeOf(notionUUID)
	markdown := shapeOf(notionString)
	attachments := notionField("attachments", notionArrayOf(notionObjectOf(false,
		notionField("file_upload_id", shapeOf(notionString)),
		notionField("type", notionEnumOf("file_upload").opt()),
	)).atMostItems(3).opt())
	displayName := notionField("display_name", notionTaggedOf("type",
		notionTagBranch("integration"),
		notionTagBranch("user"),
		notionTagBranch("custom", notionField("custom", notionObjectOf(false,
			notionField("name", shapeOf(notionString).longAtLeast(1))))),
	).opt())
	version := notionField("markdown_version", notionMarkdownVersionShape)
	branch := func(target notionShapeField, content ...notionShapeField) *notionShape {
		return notionObjectOf(false, append([]notionShapeField{target}, append(content, attachments, displayName)...)...)
	}
	return notionUnionOf(
		branch(notionField("parent", parent), notionField("rich_text", notionCommentRichTextShape)),
		branch(notionField("parent", parent), notionField("markdown", markdown), version),
		branch(notionField("discussion_id", discussion), notionField("rich_text", notionCommentRichTextShape)),
		branch(notionField("discussion_id", discussion), notionField("markdown", markdown), version),
	)
}()

// notionCommentUpdateShape is a comment's new words: rich_text or markdown,
// the Markdown's version with it, other keys left (seen 2026-11-11; the
// version from Notion's docs, read 2026-11-13).
var notionCommentUpdateShape = notionUnionOf(
	notionObjectOf(false, notionField("rich_text", notionCommentRichTextShape)),
	notionObjectOf(false, notionField("markdown", shapeOf(notionString)),
		notionField("markdown_version", notionMarkdownVersionShape)),
)

const notionCommentBothContents = "Give 'rich_text' or 'markdown', not both: one content format per request."

func notionCommentCreateSaid(body, _ *notionValue) string {
	if body.field("parent") != nil && body.field("discussion_id") != nil {
		return notionFaultOnlyOne("parent", "discussion_id")
	}
	return notionCommentUpdateSaid(body, nil)
}

// notionCommentUpdateSaid is what a comment's words are refused for beyond
// their shape: rich_text and markdown together, or a Markdown version with
// rich_text, which Notion's docs say is a validation error (read
// 2026-11-13), said as a page's is.
func notionCommentUpdateSaid(body, _ *notionValue) string {
	if body.field("rich_text") != nil && body.field("markdown") != nil {
		return notionCommentBothContents
	}
	if body.field("markdown") == nil && body.field("markdown_version") != nil {
		return notionMarkdownVersionWithoutMarkdown
	}
	return ""
}

func notionFileUploadCreateSaid(body, _ *notionValue) string {
	mode := "single_part"
	if given := body.field("mode"); given != nil {
		mode = given.text
	}
	parts, filename := body.field("number_of_parts"), body.field("filename")
	switch {
	case mode == "single_part" && parts != nil:
		return "A `single_part` upload takes no `number_of_parts`."
	case mode == "multi_part" && parts == nil:
		return "A `multi_part` upload needs `number_of_parts`."
	case mode == "multi_part" && (filename == nil || filename.kind != 's' || filename.text == ""):
		// A content type is found from the name's extension when not given.
		return "A `multi_part` upload needs a filename and a content type."
	}
	return ""
}

// notionUpdateContentShape is update_content's replacements, as Notion
// checks them (seen 2026-11-10): at most 100, each a non-empty old_str and a
// new_str; none is a change that changes nothing.
var notionUpdateContentShape = func() *notionShape {
	updates := notionArrayOf(notionObjectOf(false,
		notionField("old_str", shapeOf(notionString).longAtLeast(1)),
		notionField("new_str", shapeOf(notionString))))
	updates.maxItems = 100
	return updates
}()

var notionRouteShapes = []notionRouteShape{
	{method: http.MethodGet, path: "users/me", query: notionNoQuery},
	// A personal access token lists no one (Notion's documentation, read
	// 2026-11-12: "Personal access tokens cannot list workspace users"); it
	// reads its own bot, the people it is told of by ID and itself.
	{method: http.MethodGet, path: "users", scope: "workspace:read", patRefused: true, query: notionObjectOf(true,
		notionField("start_cursor", notionMaybeString),
		notionField("page_size", shapeOf(notionNumber).opt()))},
	{method: http.MethodGet, path: "users/*", scope: "workspace:read", query: notionNoQuery},
	{method: http.MethodPost, path: "search", scope: "pages:read", query: notionNoQuery, body: notionSearchBodyShape, after: notionSearchSortSaid},
	{method: http.MethodGet, path: "async_tasks/*", scope: "pages:read", query: notionNoQuery},
	{method: http.MethodGet, path: "custom_emojis", scope: "workspace:read", query: notionObjectOf(false,
		notionField("page_size", notionPageSizeQuery),
		notionField("start_cursor", notionStartCursor))},

	{method: http.MethodPost, path: "pages", scope: "pages:write", query: notionNoQuery, after: notionPageCreateSaid, body: notionObjectOf(true,
		notionField("parent", notionPageParentShape.opt()),
		notionField("properties", notionPagePropertiesShape),
		notionField("icon", notionPageIconShape),
		notionField("cover", notionPageCoverShape),
		notionField("content", notionMaybeArray),
		notionField("children", notionBlocksShape().opt()),
		notionField("markdown", notionMaybeString),
		notionField("markdown_version", notionMarkdownVersionShape),
		notionField("template", notionPageTemplateShape.opt()),
		notionField("position", notionPagePositionShape.opt()),
		notionField("in_trash", notionMaybeBool),
		// Leafen's own: a locked new page, and an answer before a long write ends.
		notionField("is_locked", notionMaybeBool),
		notionField("allow_async", notionMaybeBool))},
	{method: http.MethodPatch, path: "pages/*", scope: "pages:write", query: notionNoQuery, after: notionPageUpdateSaid, body: notionObjectOf(true,
		notionField("properties", notionPagePropertiesShape),
		notionField("in_trash", notionMaybeBool),
		notionField("icon", notionPageIconShape),
		notionField("cover", notionPageCoverShape),
		notionField("is_locked", notionMaybeBool),
		notionField("template", notionPageUpdateTemplateShape.opt()),
		notionField("erase_content", notionMaybeBool),
		notionField("parent", notionMaybeObject),
		// Leafen's own archive state, apart from the trash.
		notionField("is_archived", notionMaybeBool))},
	{method: http.MethodPost, path: "pages/*/move", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(false,
		notionField("parent", notionUnionOf(notionOpenParentBranch("page_id"), notionOpenParentBranch("data_source_id"))))},
	// A page's Markdown names its version in a read's query, which takes
	// other keys too, and in a write's body, after what the write holds.
	{method: http.MethodGet, path: "pages/*/properties/*", scope: "pages:read", query: notionPagePropertyQueryShape},
	{method: http.MethodGet, path: "pages/*/markdown", scope: "pages:read", query: notionObjectOf(false,
		notionField("include_transcript", notionMaybeBool),
		notionField("markdown_version", notionMarkdownVersionShape))},
	{method: http.MethodPatch, path: "pages/*/markdown", scope: "pages:write", query: notionNoQuery, body: notionTaggedOf("type",
		notionTagBranch("insert_content", notionField("insert_content", notionInsertContentShape),
			notionField("markdown_version", notionMarkdownVersionShape)),
		notionTagBranch("replace_content_range", notionField("replace_content_range", notionObjectOf(false,
			notionField("content", shapeOf(notionString)),
			notionField("content_range", shapeOf(notionString)))),
			notionField("markdown_version", notionMarkdownVersionShape)),
		notionTagBranch("update_content", notionField("update_content", notionObjectOf(false,
			notionField("content_updates", notionUpdateContentShape))),
			notionField("markdown_version", notionMarkdownVersionShape)),
		notionTagBranch("replace_content", notionField("replace_content", notionObjectOf(false,
			notionField("new_str", shapeOf(notionString)))),
			notionField("markdown_version", notionMarkdownVersionShape)),
	)},

	{method: http.MethodPost, path: "databases", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(false,
		notionField("parent", notionTaggedOf("type",
			notionTagBranch("page_id", notionField("page_id", shapeOf(notionUUID))),
			notionTagBranch("workspace", notionField("workspace", shapeOf(notionTrue).opt())))),
		notionField("title", notionRichTextShape.opt()),
		notionField("description", notionRichTextShape.opt()),
		notionField("is_inline", notionMaybeBool),
		notionField("initial_data_source", notionObjectOf(false, notionField("properties",
			&notionShape{kind: notionObject, optional: true, each: notionDataSourcePropertyMakeShape})).opt()),
		notionField("icon", notionDatabaseIconShape),
		notionField("cover", notionMaybeObject))},
	{method: http.MethodGet, path: "databases/*", scope: "pages:read", query: notionNoQuery},
	{method: http.MethodPatch, path: "databases/*", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(false,
		notionField("parent", notionMaybeObject),
		notionField("title", notionRichTextShape.opt()),
		notionField("description", notionRichTextShape.opt()),
		notionField("is_inline", notionMaybeBool),
		notionField("icon", notionDatabaseIconShape),
		notionField("cover", notionMaybeObject),
		notionField("in_trash", notionMaybeBool),
		notionField("is_locked", notionMaybeBool))},

	{method: http.MethodPost, path: "data_sources", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(false,
		notionField("parent", notionUnionOf(notionOpenParentBranch("page_id"), notionOpenParentBranch("database_id"))),
		notionField("properties", &notionShape{kind: notionObject, each: notionDataSourcePropertyMakeShape}),
		notionField("title", notionRichTextShape.opt()),
		notionField("icon", notionDataSourceIconShape))},
	{method: http.MethodGet, path: "data_sources/*", scope: "pages:read", query: notionNoQuery},
	{method: http.MethodPatch, path: "data_sources/*", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(false,
		notionField("title", notionRichTextShape.opt()),
		notionField("icon", notionDataSourceIconShape),
		notionField("properties", &notionShape{kind: notionRecord, optional: true, each: notionDataSourcePropertyChangeShape}),
		notionField("in_trash", notionMaybeBool),
		notionField("parent", notionMaybeObject))},
	{method: http.MethodPost, path: "data_sources/*/query", scope: "pages:read",
		query: notionObjectOf(true, notionField("filter_properties", notionAnyShape)),
		body: notionObjectOf(true,
			notionField("filter", notionDataSourceFilterShape.opt()),
			notionField("sorts", notionArrayOf(notionUnionOf(
				notionObjectOf(false, notionField("property", shapeOf(notionString))),
				notionObjectOf(false, notionField("timestamp", shapeOf(notionString))),
			)).opt()),
			notionField("start_cursor", notionMaybeString),
			notionField("page_size", shapeOf(notionNumber).atLeast(1).opt()),
			notionField("result_type", notionEnumOf("page", "data_source").opt()),
			// Leafen's own: rows in the archive.
			notionField("is_archived", notionMaybeBool))},
	{method: http.MethodGet, path: "data_sources/*/templates", scope: "pages:read", query: notionObjectOf(false,
		notionField("page_size", notionPageSizeQuery))},

	{method: http.MethodGet, path: "blocks/*", scope: "pages:read", query: notionNoQuery},
	{method: http.MethodDelete, path: "blocks/*", scope: "pages:write", query: notionNoQuery},
	{method: http.MethodPatch, path: "blocks/*", scope: "pages:write", query: notionNoQuery, body: notionBlockUpdateShape},
	{method: http.MethodGet, path: "blocks/*/children", scope: "pages:read", query: notionObjectOf(true,
		notionField("start_cursor", notionStartCursor),
		notionField("page_size", shapeOf(notionNumber).opt()))},
	{method: http.MethodPatch, path: "blocks/*/children", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(true,
		notionField("children", notionBlocksShape()),
		notionField("position", notionAppendPositionShape.opt()))},

	{method: http.MethodGet, path: "views", scope: "pages:read", after: notionViewListSaid, query: notionObjectOf(true,
		notionField("database_id", notionCursorQuery),
		notionField("data_source_id", notionCursorQuery),
		notionField("start_cursor", notionStartCursor),
		notionField("page_size", notionPageSizeQuery))},
	{method: http.MethodPost, path: "views", scope: "pages:write", query: notionNoQuery, after: notionViewCreateSaid, body: notionViewCreateShape},
	{method: http.MethodGet, path: "views/*", scope: "pages:read", query: notionNoQuery},
	{method: http.MethodDelete, path: "views/*", scope: "pages:write", query: notionNoQuery},
	{method: http.MethodPatch, path: "views/*", scope: "pages:write", query: notionNoQuery, body: notionObjectOf(false,
		notionField("name", notionMaybeString),
		notionField("filter", shapeOf(notionObject).orNull().opt()),
		notionField("sorts", notionArrayOf(nil).orNull().opt()),
		notionField("quick_filters", shapeOf(notionObject).orNull().opt()),
		notionField("configuration", notionMaybeObject))},
	{method: http.MethodPost, path: "views/*/queries", scope: "pages:read", query: notionNoQuery, body: notionObjectOf(false,
		notionField("page_size", shapeOf(notionNumber).atLeast(1).opt()))},
	{method: http.MethodGet, path: "views/*/queries/*", scope: "pages:read", query: notionObjectOf(true,
		notionField("start_cursor", notionStartCursor),
		notionField("page_size", shapeOf(notionNumber).atLeast(1).opt()))},
	{method: http.MethodDelete, path: "views/*/queries/*", scope: "pages:read", query: notionNoQuery},

	{method: http.MethodGet, path: "comments", scope: "comments:read", query: notionObjectOf(false,
		notionField("block_id", shapeOf(notionUUID)),
		notionField("start_cursor", notionStartCursor),
		notionField("page_size", notionPageSizeQuery))},
	{method: http.MethodGet, path: "comments/*", scope: "comments:read", query: notionNoQuery},
	// Writing comments: a token that may not is refused before its body or
	// query is read (seen 2026-11-10). One made, changed or removed takes no
	// query (seen 11-10, 11-21); a new one's body is a parent or a discussion
	// with rich_text or markdown, the two never together (seen 2026-11-10 and
	// 11-11 with a test integration allowed to insert comments), as a
	// comment's new words are; what the shape can't say the handler checks.
	{method: http.MethodPost, path: "comments", scope: "comments:write", query: notionNoQuery,
		body: notionCommentCreateShape, after: notionCommentCreateSaid},
	{method: http.MethodPatch, path: "comments/*", scope: "comments:write", query: notionNoQuery,
		body: notionCommentUpdateShape, after: notionCommentUpdateSaid},
	{method: http.MethodDelete, path: "comments/*", scope: "comments:write", query: notionNoQuery},

	{method: http.MethodGet, path: "file_uploads", scope: "files:read", query: notionObjectOf(false,
		notionField("status", notionEnumOf("pending", "uploaded", "expired", "failed").opt()),
		notionField("start_cursor", notionStartCursor),
		notionField("page_size", notionPageSizeQuery))},
	{method: http.MethodPost, path: "file_uploads", scope: "files:write", query: notionNoQuery, after: notionFileUploadCreateSaid, body: notionObjectOf(false,
		notionField("mode", notionEnumOf("single_part", "multi_part", "external_url").opt()),
		notionField("filename", notionMaybeString),
		notionField("content_type", notionMaybeString),
		notionField("number_of_parts", shapeOf(notionNumber).opt()),
		notionField("external_url", notionMaybeString))},
	{method: http.MethodGet, path: "file_uploads/*", scope: "files:read", query: notionNoQuery},
	{method: http.MethodPost, path: "file_uploads/*/send", scope: "files:write", query: notionNoQuery, multipart: true},
	{method: http.MethodPost, path: "file_uploads/*/complete", scope: "files:write", query: notionNoQuery, body: notionObjectOf(true)},
}

// notionRouteShapeFor is the shape of the endpoint a request names, if
// Leafen checks one.
func notionRouteShapeFor(method, path string) *notionRouteShape {
	rest, found := strings.CutPrefix(path, notionAPIRoot+"/")
	if !found {
		return nil
	}
	segments := strings.Split(rest, "/")
	for index := range notionRouteShapes {
		route := &notionRouteShapes[index]
		if route.method != method {
			continue
		}
		pattern := strings.Split(route.path, "/")
		if len(pattern) != len(segments) {
			continue
		}
		matches := true
		for at, part := range pattern {
			if part == "*" && segments[at] == "" || part != "*" && part != segments[at] {
				matches = false
				break
			}
		}
		// users/me is its own endpoint, not a user's ID.
		if matches && !(route.path == "users/*" && segments[1] == "me") {
			return route
		}
	}
	return nil
}
