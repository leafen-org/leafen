package app

import "encoding/json"

// The fields of each type of view configuration, in the order Notion's API
// checks them, each in its form (seen 2026-11-12, every field given out of
// form at once and taken away one by one as refused): a chart's type and
// axes first, then its look; a board's groups before its properties; a
// calendar's and a timeline's date first; a map's height before its place;
// a list's, gallery's and timeline's group_by, as a table's, after their
// properties (seen 2026-11-27).
// An enum's choices are listed in Notion's order and words: a chart's color
// themes are its eleven (brown, which charts made on the screen before
// kept, is none of them), and a field that may be null takes it as unset,
// the view's own default.

// notionViewChartAggregatorNames are the aggregators a chart's y_axis and value
// take, in the order Notion's refusal lists them.
var notionViewChartAggregatorNames = []string{"count", "count_values", "sum", "average", "median", "min", "max", "range", "unique",
	"empty", "not_empty", "percent_empty", "percent_not_empty", "checked", "unchecked", "percent_checked", "percent_unchecked",
	"earliest_date", "latest_date", "date_range"}

// notionViewReferenceLineForm is a chart's reference line as Notion checks
// it (seen 2026-11-12): its value, label, color (one of its ten) and dash
// style, then the id it may keep.
var notionViewReferenceLineForm = notionViewFormObject(notionViewFormRequired,
	notionViewFormField{"value", notionViewFormOf("number", notionViewFormRequired)},
	notionViewFormField{"label", notionViewFormOf("string", notionViewFormRequired)},
	notionViewFormField{"color", notionViewFormEnum(notionViewFormRequired, notionViewChartLineColors...)},
	notionViewFormField{"dash_style", notionViewFormEnum(notionViewFormRequired, notionViewChartLineDashStyles...)},
	notionViewFormField{"id", notionViewFormOf("string", notionViewFormOptional)})

// notionViewConfigurationFields are the fields of a view configuration of
// the type, in their forms.
func notionViewConfigurationFields(viewType string) []notionViewFormField {
	nullable, optional, required := notionViewFormNullable, notionViewFormOptional, notionViewFormRequired
	field := func(name string, form notionViewForm) notionViewFormField { return notionViewFormField{name, form} }
	enum := func(name string, words ...string) notionViewFormField {
		return field(name, notionViewFormEnum(nullable, words...))
	}
	flag := func(name string) notionViewFormField { return field(name, notionViewFormOf("boolean", nullable)) }
	item := notionViewFormObject(required,
		field("property_id", notionViewFormOf("string", required)), field("visible", notionViewFormOf("boolean", optional)),
		field("width", notionViewFormOf("number", optional)), field("wrap", notionViewFormOf("boolean", optional)),
		field("status_show_as", notionViewFormEnum(optional, "select", "checkbox")))
	list := func(name string) notionViewFormField {
		return field(name, notionViewForm{kind: "array", presence: nullable, items: &item})
	}
	group := func(name string, presence notionViewFormPresence) notionViewFormField {
		return field(name, notionViewGroupForm(presence))
	}
	cards := []notionViewFormField{
		field("cover", notionViewFormObject(nullable, field("type", notionViewFormEnum(required, "page_cover", "page_content", "property")),
			field("property_id", notionViewFormOf("string", optional)))),
		enum("cover_size", "small", "medium", "large"), enum("cover_aspect", "contain", "cover"), enum("card_layout", "list", "compact"),
	}
	heights := enum("height", notionViewHeights...)
	switch viewType {
	case graphViewTypeTable:
		return []notionViewFormField{list("properties"), group("group_by", nullable),
			field("subtasks", notionViewForm{kind: "check", presence: nullable, check: func(_ string, raw json.RawMessage) string {
				return notionViewSubtasksRefusal(raw, notionViewFormShouldBe)
			}}),
			field("wrap_cells", notionViewFormOf("boolean", optional)), field("frozen_column_index", notionViewFormOf("integer", optional)),
			field("show_vertical_lines", notionViewFormOf("boolean", optional))}
	case graphViewTypeBoard:
		return append([]notionViewFormField{group("group_by", required), group("sub_group_by", nullable), list("properties")}, cards...)
	case graphViewTypeGallery:
		return append([]notionViewFormField{list("properties"), group("group_by", nullable)}, cards...)
	case graphViewTypeList:
		return []notionViewFormField{list("properties"), group("group_by", nullable)}
	case graphViewTypeCalendar:
		return []notionViewFormField{field("date_property_id", notionViewFormOf("string", required)), list("properties"),
			enum("view_range", "week", "month"), flag("show_weekends")}
	case graphViewTypeTimeline:
		return []notionViewFormField{field("date_property_id", notionViewFormOf("string", required)),
			field("end_date_property_id", notionViewFormOf("string", nullable)), list("properties"), group("group_by", nullable),
			flag("show_table"), list("table_properties"),
			field("preference", notionViewFormObject(nullable, field("zoom_level", notionViewFormEnum(required, notionViewTimelineZooms...)),
				field("center_timestamp", notionViewFormOf("whole", optional)))),
			field("arrows_by", notionViewFormObject(nullable, field("property_id", notionViewFormOf("string", nullable)))), flag("color_by")}
	case graphViewTypeMap:
		return []notionViewFormField{heights, field("map_by", notionViewFormOf("string", nullable)), list("properties")}
	case graphViewTypeForm:
		return []notionViewFormField{flag("is_form_closed"), flag("anonymous_submissions"),
			enum("submission_permissions", "none", "comment_only", "reader", "read_and_write", "editor")}
	case graphViewTypeChart:
		axis := notionViewFormObject(nullable, field("aggregator", notionViewFormEnum(required, notionViewChartAggregatorNames...)),
			field("property_id", notionViewFormOf("string", optional)))
		return []notionViewFormField{field("chart_type", notionViewFormEnum(required, "column", "bar", "line", "donut", "number")),
			group("x_axis", nullable), field("y_axis", axis), field("value", axis), enum("sort", notionViewChartSorts...),
			enum("color_theme", "gray", "blue", "yellow", "green", "purple", "teal", "orange", "pink", "red", "auto", "colorful"),
			heights, flag("hide_empty_groups"), enum("legend_position", notionViewChartLegends...), flag("show_data_labels"),
			enum("axis_labels", notionViewChartAxisLabels...), enum("grid_lines", notionViewChartGridLines...),
			flag("cumulative"), flag("smooth_line"), flag("hide_line_fill_area"), enum("group_style", notionViewChartGroupStyles...),
			field("y_axis_min", notionViewFormOf("number", nullable)), field("y_axis_max", notionViewFormOf("number", nullable)),
			enum("donut_labels", notionViewChartDonutLabels...), flag("hide_title"), group("stack_by", nullable),
			field("reference_lines", notionViewForm{kind: "array", presence: nullable, items: &notionViewReferenceLineForm,
				most: notionViewChartLineMostLines}),
			field("caption", notionViewFormOf("string", nullable)),
			flag("color_by_value")}
	}
	return nil
}

// applyOfficialViewNull sets a field given null back to the view's own
// default, as Notion's API takes it (seen 2026-11-12: each read back
// without it), reporting whether the field is one it does so for here; the
// fields that read null themselves (a chart's choices and switches, its
// axes, a group, a list) are not. A gallery's cover goes back to none, as
// one made through the API starts (notionAPICreatedViewConfig).
func applyOfficialViewNull(viewType, field string, config map[string]any) bool {
	keys := map[string][]string{
		"color_theme": {"chartColorTheme"}, "show_data_labels": {"chartShowDataLabels"}, "color_by_value": {"chartColorByValue"},
		"view_range": {"calendarLayout"}, "show_weekends": {"showWeekends"}, "show_table": {"timelineShowTable"},
		"preference": {"timelineZoom", "timelineCenter"}, "end_date_property_id": {"timelineEndBy"}, "map_by": {"mapBy"},
		"cover_size": {"cardSize"}, "cover_aspect": {"fitImage"}, "card_layout": {"cardLayout"}, "cover": {"coverProperty"},
		"height": {map[bool]string{true: "chartHeight", false: "mapHeight"}[viewType == graphViewTypeChart]},
	}[field]
	for _, key := range keys {
		delete(config, key)
	}
	if field == "cover" && viewType == graphViewTypeGallery {
		config["coverProperty"] = "__none"
	}
	return keys != nil
}
