package app

// A data source property in a request is one of Notion's property kinds,
// named by its kind's key, as its API checks it before it looks anything up
// (seen 2026-11-12, API 2026-03-11). Each kind's settings are checked as
// Notion checks them (notionDataSourceKindSettingsShape); a kind takes a
// description, and its type, beside them, and other keys, which the
// request's reading leaves. Changing a data source also takes a property
// with only a string "name" beside whatever else it holds, which is how a
// name is changed; such a property is checked again when it is read, as a
// whole (notionDataSourcePropertyRenameShape). One that fits none is refused
// with each kind's first fault, in this order:
//
//	body isn't valid; any one of these fixes it:
//	body.properties.B.number is missing.
//	…
//	body.properties.B.place is missing.
//	body.properties.B.name is missing.
//
// A kind whose own key is there but whose settings don't fit is the only
// one named, at the deepest fault (notionMergeFaults). Changing a source, a
// kind whose type names another leaves only "name" named. Making a data
// source takes button, location, verification and last visited time too,
// and no property by its name alone; changing one doesn't take those four.
// Leafen also takes a verification when changing one, as any database may
// have one (user decision 2026-10-30), without naming it among the choices.

var notionDataSourcePropertyKinds = []string{
	"number", "formula", "select", "multi_select", "status", "relation", "rollup", "unique_id", "title",
	"rich_text", "url", "people", "files", "email", "phone_number", "date", "checkbox", "created_by",
	"created_time", "last_edited_by", "last_edited_time",
}

// notionDataSourcePropertyMadeOnly are the kinds a data source takes only
// when it is made, in the order Notion names them (before "place").
var notionDataSourcePropertyMadeOnly = []string{"button", "location", "verification", "last_visited_time"}

// notionDataSourceKindOrder is every kind a request may name, in Notion's
// order; a property naming several is the first of them (seen 2026-11-12:
// rich_text and number make a number, date and formula a formula).
var notionDataSourceKindOrder = append(append(append([]string{}, notionDataSourcePropertyKinds...),
	notionDataSourcePropertyMadeOnly...), "place")

// notionNumberFormats are the number formats Notion's API takes, in the
// order it names them (seen 2026-11-12).
var notionNumberFormats = []string{
	"number", "number_with_commas", "percent", "dollar", "australian_dollar", "canadian_dollar",
	"singapore_dollar", "euro", "pound", "yen", "ruble", "rupee", "won", "yuan", "real", "lira", "rupiah",
	"franc", "hong_kong_dollar", "new_zealand_dollar", "krona", "norwegian_krone", "mexican_peso", "rand",
	"new_taiwan_dollar", "danish_krone", "zloty", "baht", "forint", "koruna", "shekel", "chilean_peso",
	"philippine_peso", "dirham", "colombian_peso", "riyal", "ringgit", "leu", "argentine_peso",
	"uruguayan_peso", "peruvian_sol", "vietnamese_dong", "pakistani_rupee", "nigerian_naira", "bitcoin",
}

// notionRollupSettingsShape is a rollup's settings: a calculation, then the
// relation and the property it rolls up, each by name or ID. Each pairing
// is an alternative, so a missing one is named once for each way to give it
// (seen 2026-11-12: a relation by name without the property names
// rollup_property_name, relation_property_id and rollup_property_id).
var notionRollupSettingsShape = func() *notionShape {
	function := notionEnumOf(notionRollupFunctionOrder...)
	text := shapeOf(notionString)
	pairing := func(relation, rolled string) *notionShape {
		return notionObjectOf(false,
			notionField("function", function), notionField(relation, text), notionField(rolled, text))
	}
	return notionUnionOf(
		pairing("relation_property_name", "rollup_property_name"),
		pairing("relation_property_id", "rollup_property_name"),
		pairing("relation_property_name", "rollup_property_id"),
		pairing("relation_property_id", "rollup_property_id"),
	)
}()

// notionRelationSettingsShape is a relation's settings: the data source it
// points to, one way or two. A type naming the other way picks it (seen
// 2026-11-12: single_property with type "dual_property" names only
// dual_property); one-way settings take no keys, two-way a synced name and
// ID; other keys are left.
var notionRelationSettingsShape = func() *notionShape {
	way := func(kind string, settings *notionShape) *notionShape {
		return notionObjectOf(false,
			notionField("data_source_id", notionAnyUUID), notionField(kind, settings),
			notionField("type", notionPicking(notionEnumOf(kind).opt())))
	}
	synced := notionMaybeText
	return notionUnionOf(
		way("single_property", notionObjectOf(true)),
		way("dual_property", notionObjectOf(false,
			notionField("synced_property_name", synced), notionField("synced_property_id", synced))),
	)
}()

// notionSchemaOptionShape is an option a select, multi-select or status
// names: by its name, or, changing a source, by its ID instead, with a
// color and a description, and a status's with its group (seen
// 2026-11-12). Other keys are left.
func notionSchemaOptionShape(making, status bool) *notionShape {
	text := shapeOf(notionString)
	option := func(first ...notionShapeField) *notionShape {
		fields := append(first,
			notionField("color", notionEnumOf(notionOptionColors...).opt()),
			notionField("description", text.orNull().opt()))
		if status {
			fields = append(fields, notionField("group", notionEnumOf(notionStatusGroupNames...).opt()))
		}
		return notionObjectOf(false, fields...)
	}
	if making {
		return option(notionField("name", text))
	}
	return notionUnionOf(
		option(notionField("name", text), notionField("id", text.opt())),
		option(notionField("id", text), notionField("name", text.opt())),
	)
}

// notionDataSourceKindSettingsShape is what a kind's settings hold (seen
// 2026-11-12): a number's format of notionNumberFormats, a formula's
// expression, options, a unique ID's prefix, a relation's or a rollup's
// settings; the kinds without settings take no keys. Other kinds' settings
// are an object.
func notionDataSourceKindSettingsShape(kind string, making bool) *notionShape {
	options := func(status bool) *notionShape {
		return notionObjectOf(false, notionField("options", notionArrayOf(notionSchemaOptionShape(making, status)).opt()))
	}
	switch kind {
	case "number":
		return notionObjectOf(false, notionField("format", notionEnumOf(notionNumberFormats...).opt()))
	case "formula":
		return notionObjectOf(false, notionField("expression", notionMaybeText))
	case "select", "multi_select":
		return options(false)
	case "status":
		return options(true)
	case "relation":
		return notionRelationSettingsShape
	case "rollup":
		return notionRollupSettingsShape
	case "unique_id":
		return notionObjectOf(false, notionField("prefix", shapeOf(notionString).orNull().opt()))
	case "title", "rich_text", "url", "people", "files", "email", "phone_number", "date", "checkbox",
		"created_by", "created_time", "last_edited_by", "last_edited_time":
		return notionObjectOf(true)
	}
	return shapeOf(notionObject)
}

// notionDataSourcePropertyKindsShape is a property as one of kinds: its
// settings, a string name when changing a source (a new source's property
// is named by its key, whatever its name), a description and its type. A
// changed property's type naming another kind hushes them all (seen
// 2026-11-12: {type: "select", number: {}} names only "name"); a new
// source's is said. Renaming takes any rollup settings, as a renamed
// rollup's are read as they are (notionDataSourceRollupRequest).
func notionDataSourcePropertyKindsShape(kinds []string, making, renaming bool) *notionShape {
	branches := make([]*notionShape, 0, len(kinds))
	for _, kind := range kinds {
		settings := notionDataSourceKindSettingsShape(kind, making)
		if renaming && kind == "rollup" {
			settings = shapeOf(notionObject)
		}
		fields := []notionShapeField{notionField(kind, settings)}
		if !making {
			fields = append(fields, notionField("name", notionMaybeText))
		}
		kindType := notionEnumOf(kind).opt()
		kindType.hushes = !making && !renaming
		fields = append(fields,
			notionField("description", shapeOf(notionString).orNull().opt()), notionField("type", kindType))
		branches = append(branches, notionObjectOf(false, fields...))
	}
	return notionUnionOf(branches...)
}

func notionDataSourcePropertyShape(making bool) *notionShape {
	if making {
		return notionDataSourcePropertyKindsShape(notionDataSourceKindOrder, true, false)
	}
	kinds := append(append([]string{}, notionDataSourcePropertyKinds...), "place")
	verification := notionObjectOf(false, notionField("verification", shapeOf(notionObject)))
	verification.own = true
	return notionUnionOf(
		notionDataSourcePropertyKindsShape(kinds, false, false),
		notionObjectOf(false, notionField("name", shapeOf(notionString))),
		verification,
	)
}

var (
	// notionDataSourcePropertyMakeShape is a property of a data source
	// being made: a new database's initial_data_source, or a new source.
	notionDataSourcePropertyMakeShape = notionDataSourcePropertyShape(true)
	// notionDataSourcePropertyChangeShape is a property a data source's
	// update writes, or null to remove it.
	notionDataSourcePropertyChangeShape = notionDataSourcePropertyShape(false).orNull()
	// notionDataSourcePropertyRenameShape is a changed property with a name
	// and more beside it, as its reading checks it: of any kind a new source
	// takes, its settings as they are checked; one that doesn't fit is "not
	// a valid property schema" (seen 2026-11-12: a name beside a description
	// alone, a number's unknown format or a relation without its way).
	notionDataSourcePropertyRenameShape = notionDataSourcePropertyKindsShape(notionDataSourceKindOrder, false, true)
)
