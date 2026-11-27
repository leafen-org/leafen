package app

// What a block type's object holds, as Notion's request check has it (each
// type measured against Notion's API on 2026-11-10 at Notion-Version
// 2026-03-11): a closed object, so a key Notion doesn't take "should be not
// present", its keys checked in the order below. A new block's children sit
// where Notion checks them; an update takes none. An update of a paragraph,
// a to-do or a callout takes other keys, as Notion's does. A file Leafen
// hosts is kept as a file source so a block Leafen reads out can be written
// back. A numbered list's start and format, which a read gives, are no
// request keys, as Notion's (seen 2026-11-10: "should be not present").

var (
	notionBlockColorShape   = notionEnumOf(notionTextColors...).opt()
	notionBlockCaptionShape = notionRichTextShape.opt()
	notionBlockBooleanShape = shapeOf(notionBoolean).opt()
	notionBlockUploadShape  = notionObjectOf(true, notionField("id", notionAnyUUID))
	// A picture's, a video's, a PDF's, a file's or a sound's address.
	notionBlockAddressShape = notionObjectOf(true, notionField("url", shapeOf(notionString).notEmpty().longAtMost(2000)))
	// A file Leafen hosts, as a block reads it out (notion_hosted_file_request.go).
	notionBlockHostedFileShape = notionObjectOf(true,
		notionField("url", &notionShape{kind: notionString, accepts: notionHostedFileURL, said: "a file address Leafen gave"}),
		notionField("expiry_time", notionISODateOf("a valid ISO 8601 date-time string").opt()))
	// notionCodeLanguages are the languages a code block takes, as Notion
	// lists them (validNotionCodeLanguage).
	notionCodeLanguages = []string{
		"abap", "abc", "agda", "arduino", "ascii art", "assembly", "bash", "basic", "bnf", "c", "c#", "c++",
		"clojure", "coffeescript", "coq", "css", "dart", "dhall", "diff", "docker", "ebnf", "elixir", "elm",
		"erlang", "f#", "flow", "fortran", "gherkin", "glsl", "go", "graphql", "groovy", "haskell", "hcl", "html",
		"idris", "java", "javascript", "json", "julia", "kotlin", "latex", "less", "lisp", "livescript", "llvm ir",
		"lua", "makefile", "markdown", "markup", "matlab", "mathematica", "mermaid", "nix", "notion formula",
		"objective-c", "ocaml", "pascal", "perl", "php", "plain text", "powershell", "prolog", "protobuf",
		"purescript", "python", "r", "racket", "reason", "ruby", "rust", "sass", "scala", "scheme", "scss", "shell",
		"smalltalk", "solidity", "sql", "swift", "toml", "typescript", "vb.net", "verilog", "vhdl", "visual basic",
		"webassembly", "xml", "yaml", "java/c/c++/c#",
	}
	// A paragraph's or a callout's icon: an emoji, an address, a custom emoji,
	// an upload or a built-in icon, each its key holding what that kind
	// holds and a type naming that kind or left out.
	notionBlockIconShape = func() *notionShape {
		kind := func(name string, inner *notionShape) *notionShape {
			return notionObjectOf(true, notionField(name, inner), notionField("type", notionEnumOf(name).opt()))
		}
		return notionUnionOf(
			kind("emoji", shapeOf(notionEmoji)),
			kind("external", notionObjectOf(true, notionField("url", shapeOf(notionString)))),
			kind("custom_emoji", notionObjectOf(true,
				notionField("id", notionAnyUUID), notionField("name", notionMaybeText), notionField("url", notionMaybeText))),
			kind("file_upload", notionBlockUploadShape),
			kind("icon", notionObjectOf(true,
				notionField("name", shapeOf(notionString)),
				notionField("color", notionEnumOf("gray", "lightgray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red").opt()))),
		).opt()
	}()
	// A synced block's source: none for an original, or the block it copies.
	notionSyncedFromShape = notionObjectOf(true,
		notionField("block_id", notionAnyUUID),
		notionField("type", notionEnumOf("block_id").opt())).orNull()
)

// notionBlockContent is what a blockType's object holds: at creation with
// children where Notion checks them (nil where it takes none), or in an
// update.
func notionBlockContent(blockType string, children *notionShape, update bool) *notionShape {
	open := false
	with := func(fields ...notionShapeField) *notionShape {
		kept := make([]notionShapeField, 0, len(fields))
		for _, field := range fields {
			if field.name == "children" {
				if children == nil || update {
					continue
				}
				field.shape = children
			}
			kept = append(kept, field)
		}
		return notionObjectOf(!open, kept...)
	}
	required := func(shape *notionShape) *notionShape {
		if update {
			return shape.opt()
		}
		return shape
	}
	text := notionField("rich_text", notionRichTextShape)
	if update && !notionUpdateNeedsRichText[blockType] {
		text.shape = notionRichTextShape.opt()
	}
	place := notionField("children", nil)
	color := notionField("color", notionBlockColorShape)
	icon := notionField("icon", notionBlockIconShape)
	switch blockType {
	case "paragraph":
		if update {
			open = true
			return with(text, icon, color)
		}
		return with(text, color, icon, place)
	case "heading_1", "heading_2", "heading_3", "heading_4":
		return with(text, color, notionField("is_toggleable", notionBlockBooleanShape), place)
	case "bulleted_list_item", "numbered_list_item", "quote", "toggle":
		return with(text, color, place)
	case "to_do":
		checked := notionField("checked", notionBlockBooleanShape)
		if update {
			open = true
			return with(text, checked, color)
		}
		return with(text, color, place, checked)
	case "template":
		return with(text, place)
	case "callout":
		if update {
			open = true
			return with(text, icon, color)
		}
		return with(text, color, place, icon)
	case "code":
		return with(text, notionField("language", required(notionEnumOf(notionCodeLanguages...))),
			notionField("caption", notionBlockCaptionShape))
	case "equation":
		return with(notionField("expression", shapeOf(notionString)))
	case "divider", "breadcrumb", "meeting_notes", "custom_block":
		return with()
	case "ai_block":
		return with(notionField("custom_content", notionObjectOf(true, notionField("rich_text", notionRichTextShape))))
	case "table_of_contents":
		return with(color)
	case "tab", "column_list":
		return with(place)
	case "column":
		return with(place, notionField("width_ratio", shapeOf(notionNumber).between(0, 1).opt()))
	case "table":
		headers := []notionShapeField{
			notionField("has_column_header", notionBlockBooleanShape),
			notionField("has_row_header", notionBlockBooleanShape),
		}
		if update {
			return with(headers...)
		}
		return with(append([]notionShapeField{
			notionField("table_width", shapeOf(notionNumber).integer().atLeast(1)), place,
		}, headers...)...)
	case "table_row":
		return with(notionField("cells", notionArrayOf(notionRichTextShape).atMostItems(100)))
	case "synced_block":
		return with(notionField("synced_from", notionSyncedFromShape), place)
	case "bookmark":
		return with(notionField("url", required(shapeOf(notionString))), notionField("caption", notionBlockCaptionShape))
	case "embed":
		return notionEmbedContent(update)
	case "image", "video", "pdf", "file", "audio":
		return notionFileBlockContent(blockType, update)
	case "link_to_page":
		return notionLinkToPageContent()
	}
	return with(text, color, place)
}

// notionEmbedContent is an embed: an address, or an uploaded file. An
// address's alternative takes no type, and isn't named when a type is all
// that doesn't fit it; a new upload's type, when given, names it.
func notionEmbedContent(update bool) *notionShape {
	url := shapeOf(notionString)
	upload := []notionShapeField{notionField("file_upload", notionBlockUploadShape)}
	if update {
		url = url.opt()
	} else {
		upload = append(upload, notionField("type", notionPicking(notionEnumOf("file_upload").opt())))
	}
	return notionUnionOf(
		notionObjectOf(true,
			notionField("url", url),
			notionField("caption", notionBlockCaptionShape),
			notionField("type", notionPicking(&notionShape{kind: notionAbsent, optional: true}))),
		notionObjectOf(true, append(upload, notionField("caption", notionBlockCaptionShape))...),
	)
}

// notionFileBlockContent is a picture, video, PDF, file or sound: at
// creation an address or an uploaded file, its type, when given, naming
// which, or a file Leafen hosts; in an update any of them, or none. A file
// has its name.
func notionFileBlockContent(blockType string, update bool) *notionShape {
	name := notionField("name", shapeOf(notionString).longAtMost(100).notEmpty().opt())
	if update {
		fields := []notionShapeField{
			notionField("caption", notionBlockCaptionShape),
			notionField("external", notionBlockAddressShape.opt()),
			notionField("file_upload", notionBlockUploadShape.opt()),
		}
		if blockType == "file" {
			fields = append(fields, name)
		}
		return notionObjectOf(true, append(fields, notionField("file", notionOwn(notionBlockHostedFileShape.opt())))...)
	}
	source := func(kind string, inner *notionShape) *notionShape {
		fields := []notionShapeField{
			notionField(kind, inner),
			notionField("type", notionPicking(notionEnumOf(kind).opt())),
			notionField("caption", notionBlockCaptionShape),
		}
		if blockType == "file" {
			fields = append(fields, name)
		}
		return notionObjectOf(true, fields...)
	}
	return notionUnionOf(
		source("external", notionBlockAddressShape),
		source("file_upload", notionBlockUploadShape),
		notionOwn(source("file", notionBlockHostedFileShape)),
	)
}

// notionLinkToPageContent is a link to a page, a database or a comment, its
// type, when given, naming which.
func notionLinkToPageContent() *notionShape {
	link := func(kind string) *notionShape {
		return notionObjectOf(true,
			notionField(kind, notionAnyUUID),
			notionField("type", notionEnumOf(kind).opt()))
	}
	return notionUnionOf(link("page_id"), link("database_id"), link("comment_id"))
}

// notionOwn is what Leafen takes beside Notion's (notionShape.own).
func notionOwn(shape *notionShape) *notionShape {
	copied := *shape
	copied.own = true
	return &copied
}

// notionPicking is a key telling a union's alternatives apart: an
// alternative that fails there isn't named while another fails elsewhere.
func notionPicking(shape *notionShape) *notionShape {
	copied := *shape
	copied.picks = true
	return &copied
}
