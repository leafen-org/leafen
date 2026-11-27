package app

// The rich text and page property value shapes Notion checks a request
// against before it looks anything up (each measured against Notion's API on
// 2026-11-10). A rich text item is text, a mention or an equation, wherever
// it is written: a block's text, a title, a database's name. A page's
// properties are values of the kinds below (notionPagePropertiesShape).

var (
	notionOptionColors = []string{"default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"}
	notionTextColors   = append(append([]string{}, notionOptionColors...),
		"default_background", "gray_background", "brown_background", "orange_background", "yellow_background",
		"green_background", "blue_background", "purple_background", "pink_background", "red_background")
)

var (
	notionMaybeText = shapeOf(notionString).opt()
	notionAnyUUID   = shapeOf(notionUUID)
)

func notionISODateOf(said string) *notionShape {
	return &notionShape{kind: notionISODate, said: said}
}

// notionDateValueShape is a date: its start, an end and a time zone of
// notionTimeZones. Leafen's own deadline and reminder ride along on a
// property's date (deadline_values.go).
func notionDateValueShape(leafenOwn bool) *notionShape {
	day := notionISODateOf("a valid ISO 8601 date string")
	fields := []notionShapeField{
		notionField("start", day),
		notionField("end", day.orNull().opt()),
		notionField("time_zone", notionEnumOf(notionTimeZones...).orNull().opt()),
	}
	if leafenOwn {
		fields = append(fields, notionField("deadline", notionAnyShape), notionField("reminder", notionAnyShape))
	}
	return notionObjectOf(true, fields...)
}

// notionRichTextItem is a rich text item as Notion checks one in a block, a
// title or a name; inComment is one in a comment, which Notion checks less
// (seen 2026-11-11 with a test integration allowed to insert comments): an
// annotation it doesn't know is said after the check ("Unrecognized annotation
// type: …"), a link's address may be any length, and an equation's
// expression any string.
func notionRichTextItem(inComment bool) *notionShape {
	annotations := notionObjectOf(!inComment,
		notionField("bold", notionMaybeBool),
		notionField("italic", notionMaybeBool),
		notionField("strikethrough", notionMaybeBool),
		notionField("underline", notionMaybeBool),
		notionField("code", notionMaybeBool),
		notionField("color", notionEnumOf(notionTextColors...).opt()),
	)
	item := func(kind string, inner *notionShape) *notionShape {
		return notionObjectOf(true,
			notionField(kind, inner),
			notionField("type", notionEnumOf(kind).opt()),
			notionField("annotations", annotations.opt()),
			notionField("plain_text", notionMaybeText),
			notionField("href", shapeOf(notionString).orNull().opt()),
		)
	}
	address := shapeOf(notionString).longAtMost(2000)
	if inComment {
		address = shapeOf(notionString)
	}
	text := notionObjectOf(true,
		notionField("content", shapeOf(notionString).longAtMost(2000)),
		notionField("link", notionObjectOf(false, notionField("url", address)).orNull().opt()),
	)
	named := func(fields ...notionShapeField) *notionShape { return notionObjectOf(true, fields...) }
	mention := notionUnionOf(
		notionObjectOf(false, notionField("user", named(
			notionField("id", notionAnyUUID), notionField("object", notionEnumOf("user").opt())))),
		notionObjectOf(false, notionField("date", notionDateValueShape(false))),
		notionObjectOf(false, notionField("page", named(notionField("id", notionAnyUUID)))),
		notionObjectOf(false, notionField("database", named(notionField("id", notionAnyUUID)))),
		notionObjectOf(false, notionField("template_mention", notionUnionOf(
			named(notionField("template_mention_date", notionEnumOf("today", "now")), notionField("type", notionAnyShape)),
			named(notionField("template_mention_user", notionEnumOf("me")), notionField("type", notionAnyShape)),
		))),
		notionObjectOf(false, notionField("custom_emoji", named(
			notionField("id", notionAnyUUID), notionField("name", notionMaybeText), notionField("url", notionMaybeText)))),
	)
	// An equation's expression can't be empty (seen 2026-11-11, an empty
	// equation.expression refused).
	equation := notionObjectOf(true, notionField("expression", shapeOf(notionString).notEmpty().longAtMost(2000)))
	if inComment {
		equation = notionObjectOf(true, notionField("expression", shapeOf(notionString)))
	}
	return notionUnionOf(item("text", text), item("mention", mention), item("equation", equation))
}

// An option is named by its ID or its name.
var notionOptionShape = notionUnionOf(
	notionObjectOf(true,
		notionField("id", shapeOf(notionString).notEmpty()),
		notionField("name", notionMaybeText),
		notionField("color", notionEnumOf(notionOptionColors...).opt())),
	notionObjectOf(true,
		notionField("name", shapeOf(notionString).notEmpty()),
		notionField("id", notionMaybeText),
		notionField("color", notionEnumOf(notionOptionColors...).opt())),
)

// A file is Notion's, an address's or an upload's, its type, when given,
// naming which; one whose type names another kind isn't named while another
// kind fails elsewhere.
var notionFileItemShape = func() *notionShape {
	kind := func(name string, inner *notionShape, fileName *notionShape) *notionShape {
		picks := notionEnumOf(name).opt()
		picks.picks = true
		return notionObjectOf(true, notionField(name, inner), notionField("name", fileName), notionField("type", picks))
	}
	// A name, and an address, that should be populated (seen 2026-11-10).
	fileName := shapeOf(notionString).longAtMost(100)
	return notionUnionOf(
		kind("file", notionObjectOf(true,
			notionField("url", shapeOf(notionString)),
			notionField("expiry_time", notionISODateOf("a valid ISO 8601 date-time string").opt())), fileName),
		kind("external", notionObjectOf(true, notionField("url", shapeOf(notionString).notEmpty().longAtMost(2000))), fileName.notEmpty()),
		kind("file_upload", notionObjectOf(true, notionField("id", notionAnyUUID)), fileName.notEmpty().opt()),
	)
}()

var (
	notionRelationItemShape = notionObjectOf(false, notionField("id", notionAnyUUID))
	notionPlaceShape        = notionObjectOf(false,
		notionField("lat", shapeOf(notionNumber)),
		notionField("lon", shapeOf(notionNumber)),
		notionField("name", shapeOf(notionString).orNull().opt()),
		notionField("address", shapeOf(notionString).orNull().opt()),
		notionField("google_place_id", shapeOf(notionString).orNull().opt()))
	notionVerificationShape = notionUnionOf(
		notionObjectOf(false,
			notionField("state", notionEnumOf("verified")),
			notionField("date", notionDateValueShape(false).opt())),
		notionObjectOf(false, notionField("state", notionEnumOf("unverified"))),
	)
)

// notionPropertyValueKinds are the kinds of value a page property takes and
// what each holds, as Notion's request check has them: {"kind": value} is
// one, and so is a bare value a database page's property of the kind reads
// as its own (notionRowRequestValues).
var notionPropertyValueKinds = func() []notionShapeField {
	richText := notionArrayOf(notionRichTextItemShape).atMostItems(100)
	people := notionArrayOf(notionUnionOf(
		notionObjectOf(true, notionField("id", notionAnyUUID), notionField("object", notionEnumOf("user").opt())),
		notionObjectOf(true, notionField("id", notionAnyUUID), notionField("object", notionEnumOf("group").opt())),
	)).atMostItems(100)
	contact := shapeOf(notionString).notEmpty().longAtMost(100).orNull()
	return []notionShapeField{
		notionField("title", richText),
		notionField("rich_text", richText),
		notionField("number", shapeOf(notionNumber).orNull()),
		notionField("url", shapeOf(notionString).notEmpty().longAtMost(2000).orNull()),
		notionField("select", notionOptionShape.orNull()),
		notionField("multi_select", notionArrayOf(notionOptionShape).atMostItems(100)),
		notionField("people", people),
		notionField("email", contact),
		notionField("phone_number", contact),
		notionField("date", notionDateValueShape(true).orNull()),
		notionField("checkbox", shapeOf(notionBoolean)),
		notionField("relation", notionArrayOf(notionRelationItemShape).atMostItems(100)),
		notionField("files", notionArrayOf(notionFileItemShape).atMostItems(100)),
		notionField("status", notionOptionShape.orNull()),
		notionField("place", notionPlaceShape.orNull()),
		notionField("verification", notionVerificationShape),
	}
}()

// notionPagePropertiesShape is a page's properties, read as Notion's does
// (seen 2026-11-10): any value but a falsy one, whose keys, as Object.keys
// finds them, all hold a kind's {"kind": value}, or all hold bare values: an
// option, a date, a place, a verification, a list of one kind's items, a
// number or null, a string that isn't empty, or a boolean. A request that
// mixes the two is refused for the first property of the other way.
var notionPagePropertiesShape = func() *notionShape {
	wrapped := make([]*notionShape, 0, len(notionPropertyValueKinds))
	for _, kind := range notionPropertyValueKinds {
		wrapped = append(wrapped, notionObjectOf(true,
			kind, notionField("type", notionEnumOf(kind.name).opt()), notionField("id", notionMaybeText)))
	}
	bare := append([]*notionShape{}, notionOptionShape.branches...)
	bare = append(bare, notionDateValueShape(false), notionPlaceShape)
	bare = append(bare, notionVerificationShape.branches...)
	bare = append(bare,
		notionArrayOf(notionRichTextItemShape).atMostItems(100), notionArrayOf(notionOptionShape).atMostItems(100),
		notionArrayOf(notionRelationItemShape).atMostItems(100), notionArrayOf(notionFileItemShape).atMostItems(100),
		shapeOf(notionNumber).orNull(), shapeOf(notionString).notEmpty(), shapeOf(notionBoolean))
	return notionUnionOf(
		&notionShape{kind: notionRecord, optional: true, truthy: true, each: notionUnionOf(wrapped...)},
		&notionShape{kind: notionRecord, optional: true, truthy: true, each: notionUnionOf(bare...)},
	).opt()
}()
