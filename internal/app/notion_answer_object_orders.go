package app

// notionJSONRole is one kind of object's order in Notion's answers: keys
// names its keys in order ("*" for the key its type names) and members the
// role of what a key holds ("*" for the type's key, where a role ending in
// ":" is followed by the type), each that of any other key. An array's
// items take the role of the key that holds it. One of a type that Notion
// writes in an order of its own takes byType's role for that type.
type notionJSONRole struct {
	keys    []string
	members map[string]string
	each    string
	byType  map[string]*notionJSONRole
}

// notionJSONObjectRoles are the roles of Notion's objects, by "object".
var notionJSONObjectRoles = map[string]string{
	"page": "page", "block": "block", "database": "database", "data_source": "data source",
	"list": "list", "user": "user", "comment": "comment", "error": "error",
	"property_item": "property item", "view": "view", "file_upload": "file upload",
	"page_markdown": "page markdown", "view_query": "view query", "space": "space",
}

// The orders, as Notion's API answered them on 2026-11-11. A few it gave
// none of then are as their kind's siblings are (a heading_4 as a heading,
// a status value as a select's) or as its reference lists them (a hosted
// file, a verification, a custom emoji, a link mention).
var notionJSONRoles = map[string]*notionJSONRole{
	"page": {
		keys: []string{"object", "id", "created_time", "last_edited_time", "created_by", "last_edited_by",
			"cover", "icon", "parent", "in_trash", "is_archived", "is_locked", "properties", "url", "public_url"},
		members: map[string]string{"cover": "typed", "icon": "typed", "parent": "typed", "properties": "page properties"},
	},
	"block": {
		keys: []string{"object", "id", "parent", "created_time", "last_edited_time", "created_by", "last_edited_by",
			"has_children", "in_trash", "type", "*"},
		members: map[string]string{"parent": "typed", "*": "block:"},
	},
	"database": {
		keys: []string{"object", "id", "title", "description", "parent", "is_inline", "database_type", "in_trash",
			"is_locked", "show_data_source_title", "created_time", "last_edited_time", "data_sources", "icon", "cover",
			"url", "public_url"},
		members: map[string]string{"parent": "typed", "icon": "typed", "cover": "typed", "data_sources": "data source reference"},
	},
	"data source": {
		keys: []string{"object", "id", "cover", "icon", "created_time", "created_by", "last_edited_by", "last_edited_time",
			"title", "description", "is_inline", "database_type", "properties", "parent", "database_parent", "url",
			"public_url", "in_trash"},
		members: map[string]string{"cover": "typed", "icon": "typed", "parent": "typed", "database_parent": "typed",
			"properties": "schema properties"},
	},
	"list": {
		keys:    []string{"object", "results", "next_cursor", "has_more", "type", "*"},
		members: map[string]string{"*": "list:", "request_status": "request status"},
		// Custom emojis are listed in an order of their own (seen 2026-11-12).
		byType: map[string]*notionJSONRole{"custom_emoji": {keys: []string{"object", "type", "results", "has_more", "next_cursor"}}},
	},
	"list:property_item": {keys: []string{"id", "next_url", "type", "*"}, members: map[string]string{"*": "value:"}},
	"user": {
		keys:    []string{"object", "id", "name", "avatar_url", "type", "person", "bot"},
		members: map[string]string{"person": "person", "bot": "bot"},
	},
	"person": {keys: []string{"email", "email_verified"}},
	"bot":    {keys: []string{"owner", "workspace_name", "workspace_id", "workspace_limits"}, members: map[string]string{"owner": "typed"}},
	"comment": {
		keys: []string{"object", "id", "parent", "discussion_id", "created_time", "last_edited_time", "created_by",
			"rich_text", "display_name", "original_content_deleted", "has_pending_attachments", "attachments"},
		members: map[string]string{"parent": "typed", "display_name": "typed"},
	},
	"error": {
		keys: []string{"object", "status", "code", "message", "additional_data"}, members: map[string]string{"additional_data": "error data"},
	},
	// A tool's refusal names its code, class, whether to try again and its
	// words' template (seen 2026-11-12); a rate limit its reason and wait.
	"error data": {keys: []string{"tool_error_code", "tool_error_class", "tool_error_retryable", "tool_error_message_template",
		"rate_limit_reason", "retry_after"}},
	"property item": {keys: []string{"object", "type", "id", "*"}, members: map[string]string{"*": "value:"}},
	"view": {
		keys: []string{"object", "id", "parent", "data_source_id", "name", "type", "created_time", "created_by",
			"last_edited_time", "last_edited_by", "url", "filter", "sorts", "quick_filters", "configuration", "dashboard_view_id"},
		members: map[string]string{"parent": "typed", "filter": "filter", "sorts": "sort", "configuration": "view configuration"},
	},
	// A view's configuration in its type's order, whatever order a request
	// gave it in (seen 2026-11-12), and the objects in it.
	"view configuration": {keys: []string{"type"}, byType: notionJSONViewConfigurations},
	"view property":      {keys: []string{"property_id", "property_name", "visible", "width", "wrap", "status_show_as"}},
	// A group, as a formula's group of its result, in the same order.
	"view group": {
		keys: []string{"type", "property_id", "group_by", "sort", "property_name", "hide_empty_groups", "start_day_of_week",
			"range_start", "range_end", "range_size"},
		members: map[string]string{"group_by": "view group"},
	},
	"view subtasks":       {keys: []string{"property_id", "display_mode", "filter_scope", "toggle_column_id"}},
	"view cover":          {keys: []string{"type", "property_id"}},
	"timeline preference": {keys: []string{"zoom_level", "center_timestamp"}},
	"chart axis":          {keys: []string{"aggregator", "property_id"}},
	"dashboard row":       {keys: []string{"id", "widgets", "height"}, members: map[string]string{"widgets": "dashboard widget"}},
	"dashboard widget":    {keys: []string{"id", "view_id", "row_index", "width"}},
	"file upload": {
		keys: []string{"object", "id", "created_time", "created_by", "last_edited_time", "expiry_time", "upload_url",
			"in_trash", "status", "filename", "content_type", "content_length"},
		members: map[string]string{"created_by": "upload creator"},
	},
	"upload creator": {keys: []string{"id", "type"}},
	// An Admin API workspace, as its reference lists it.
	"space":         {keys: []string{"object", "id", "name"}},
	"page markdown": {keys: []string{"object", "id", "markdown", "truncated", "unknown_block_ids"}},
	// A view's query as it is made, and as it is deleted (seen 2026-11-12).
	"view query": {keys: []string{"object", "id", "view_id", "expires_at", "total_count", "results", "next_cursor", "has_more", "deleted"},
		members: map[string]string{"request_status": "request status"}},
	// A list or view query cut short at its result limit says so in this
	// order, as Notion's reference for a data source's query writes it
	// (read 2026-11-18; no answer of Notion's API was seen cut short).
	"request status": {keys: []string{"type", "incomplete_reason"}},

	// An object named by its type: a parent, an icon, a cover, a formula's
	// value, a link to a page, a comment's display name.
	"typed": {
		keys: []string{"type", "*"},
		members: map[string]string{"external": "external", "file": "hosted file", "file_upload": "reference",
			"custom_emoji": "custom emoji", "date": "date"},
	},
	// A chart's reference line, its id first (seen 2026-11-12).
	"reference line":        {keys: []string{"id", "value", "label", "color", "dash_style"}},
	"external":              {keys: []string{"url"}},
	"hosted file":           {keys: []string{"url", "expiry_time"}},
	"reference":             {keys: []string{"id"}},
	"custom emoji":          {keys: []string{"id", "name", "url"}},
	"date":                  {keys: []string{"start", "end", "time_zone"}},
	"data source reference": {keys: []string{"id", "name"}},
	"filter": {
		keys:    []string{"property", "timestamp"},
		members: map[string]string{"and": "filter", "or": "filter"},
	},
	"sort": {keys: []string{"property", "timestamp", "direction"}},

	// A page's properties, and a data source's, are in the order their
	// objects are written (notionPropertyObjects).
	"page properties":    {each: "property value"},
	"property value":     {keys: []string{"id", "type", "*", "has_more"}, members: map[string]string{"*": "value:"}},
	"value:select":       {keys: []string{"id", "name", "color"}},
	"value:status":       {keys: []string{"id", "name", "color"}},
	"value:multi_select": {keys: []string{"id", "name", "color"}},
	"value:date":         {keys: []string{"start", "end", "time_zone"}},
	"value:formula":      {keys: []string{"type", "*"}, members: map[string]string{"date": "date"}},
	"value:rollup": {
		keys:    []string{"type", "*", "function"},
		members: map[string]string{"date": "date", "array": "rollup item"},
		// A rollup's property item before its last page.
		byType: map[string]*notionJSONRole{"incomplete": {keys: []string{"function", "type", "incomplete"}}},
	},
	// A related page's value in a rollup that shows them, as its property's.
	"rollup item": {keys: []string{"type", "*"}, members: map[string]string{"*": "value:"}},
	"value:files": {
		keys:    []string{"name", "type", "*"},
		members: map[string]string{"external": "external", "file": "hosted file", "file_upload": "reference"},
	},
	"value:unique_id":    {keys: []string{"prefix", "number"}},
	"value:relation":     {keys: []string{"id"}},
	"value:verification": {keys: []string{"state", "verified_by", "date"}, members: map[string]string{"date": "date"}},
	"schema properties":  {each: "schema property"},
	"schema property": {
		keys:    []string{"id", "name", "description", "type", "*"},
		members: map[string]string{"*": "schema:"},
	},
	"schema:number":       {keys: []string{"format"}},
	"schema:formula":      {keys: []string{"expression"}},
	"schema:select":       {keys: []string{"options"}, members: map[string]string{"options": "option"}},
	"schema:multi_select": {keys: []string{"options"}, members: map[string]string{"options": "option"}},
	"schema:status": {
		keys:    []string{"options", "groups", "default_option"},
		members: map[string]string{"options": "option", "groups": "status group"},
	},
	"option":       {keys: []string{"id", "name", "color", "description"}},
	"status group": {keys: []string{"id", "name", "color", "option_ids"}},
	"schema:relation": {
		keys: []string{"database_id", "data_source_id", "limit", "type", "*"},
	},
	"schema:rollup": {
		keys: []string{"rollup_property_name", "relation_property_name", "rollup_property_id", "relation_property_id", "function"},
	},
	"schema:unique_id": {keys: []string{"prefix"}},

	// A block's own object, by its type.
	"block:paragraph":          {keys: []string{"rich_text", "icon", "color"}, members: map[string]string{"icon": "typed"}},
	"block:heading_1":          {keys: []string{"rich_text", "is_toggleable", "color"}},
	"block:heading_2":          {keys: []string{"rich_text", "is_toggleable", "color"}},
	"block:heading_3":          {keys: []string{"rich_text", "is_toggleable", "color"}},
	"block:heading_4":          {keys: []string{"rich_text", "is_toggleable", "color"}},
	"block:bulleted_list_item": {keys: []string{"rich_text", "color"}},
	"block:numbered_list_item": {keys: []string{"rich_text", "color"}},
	"block:quote":              {keys: []string{"rich_text", "color"}},
	"block:toggle":             {keys: []string{"rich_text", "color"}},
	"block:to_do":              {keys: []string{"rich_text", "checked", "color"}},
	"block:callout":            {keys: []string{"rich_text", "icon", "color"}, members: map[string]string{"icon": "typed"}},
	"block:code":               {keys: []string{"caption", "rich_text", "language"}},
	"block:bookmark":           {keys: []string{"caption", "url"}},
	"block:embed":              {keys: []string{"caption", "url"}},
	"block:image":              notionJSONMediaRole,
	"block:video":              notionJSONMediaRole,
	"block:pdf":                notionJSONMediaRole,
	"block:audio":              notionJSONMediaRole,
	"block:file":               notionJSONMediaRole,
	"block:table":              {keys: []string{"table_width", "has_column_header", "has_row_header"}},
	"block:table_row":          {keys: []string{"cells"}},
	"block:table_of_contents":  {keys: []string{"color"}},
	"block:link_to_page":       {keys: []string{"type", "*"}},
	"block:equation":           {keys: []string{"expression"}},
	"block:child_page":         {keys: []string{"title"}},
	"block:child_database":     {keys: []string{"title"}},
	"block:synced_block":       {keys: []string{"synced_from"}, members: map[string]string{"synced_from": "typed"}},

	// A rich text item and the objects in it.
	"rich text": {
		keys: []string{"type", "*", "annotations", "plain_text", "href"},
		members: map[string]string{"text": "text", "annotations": "annotations", "equation": "equation",
			"mention": "mention"},
	},
	"text":        {keys: []string{"content", "link"}, members: map[string]string{"link": "external"}},
	"annotations": {keys: []string{"bold", "italic", "strikethrough", "underline", "code", "color"}},
	"equation":    {keys: []string{"expression"}},
	"mention": {
		keys: []string{"type", "*"},
		members: map[string]string{"date": "date", "page": "reference", "database": "reference",
			"template_mention": "template mention", "custom_emoji": "custom emoji", "link_mention": "link mention",
			"link_preview": "external"},
	},
	"template mention": {keys: []string{"type", "*"}},
	// Not seen on Notion, whose API makes none: as its reference lists them.
	"link mention": {keys: []string{"href", "title", "description", "link_author", "link_provider", "thumbnail_url", "icon_url"}},
}

// notionJSONViewConfigurations are each type of view's configuration, its
// keys as Notion's API wrote them (seen 2026-11-12); a chart's are those of
// every chart type, each of which writes some of them.
var notionJSONViewConfigurations = map[string]*notionJSONRole{
	"table":    notionJSONViewConfiguration("properties", "group_by", "subtasks", "wrap_cells", "frozen_column_index", "show_vertical_lines"),
	"board":    notionJSONViewConfiguration("group_by", "sub_group_by", "properties", "cover", "cover_size", "cover_aspect", "card_layout"),
	"gallery":  notionJSONViewConfiguration("properties", "cover", "cover_size", "cover_aspect", "card_layout"),
	"list":     notionJSONViewConfiguration("properties"),
	"calendar": notionJSONViewConfiguration("date_property_id", "date_property_name", "properties", "view_range", "show_weekends"),
	"timeline": notionJSONViewConfiguration("date_property_id", "date_property_name", "end_date_property_id", "end_date_property_name",
		"properties", "show_table", "table_properties", "preference", "arrows_by", "color_by"),
	"map": notionJSONViewConfiguration("height", "map_by", "map_by_property_name"),
	"chart": notionJSONViewConfiguration("chart_type", "x_axis", "y_axis", "value", "stack_by", "sort", "color_theme", "height",
		"hide_empty_groups", "legend_position", "show_data_labels", "axis_labels", "grid_lines", "cumulative", "smooth_line",
		"hide_line_fill_area", "group_style", "y_axis_min", "y_axis_max", "donut_labels", "hide_title", "reference_lines", "caption",
		"color_by_value"),
	"form":      notionJSONViewConfiguration("is_form_closed", "anonymous_submissions", "submission_permissions"),
	"dashboard": notionJSONViewConfiguration("rows"),
}

// notionJSONViewConfiguration is one type of view's configuration: its type,
// then keys.
func notionJSONViewConfiguration(keys ...string) *notionJSONRole {
	return &notionJSONRole{keys: append([]string{"type"}, keys...), members: map[string]string{
		"properties": "view property", "table_properties": "view property", "group_by": "view group", "sub_group_by": "view group",
		"x_axis": "view group", "stack_by": "view group", "subtasks": "view subtasks", "cover": "view cover",
		"preference": "timeline preference", "y_axis": "chart axis", "value": "chart axis", "reference_lines": "reference line",
		"rows": "dashboard row",
	}}
}

// notionJSONMediaRole is an image's, a video's, a PDF's, an audio's or a
// file's own object.
var notionJSONMediaRole = &notionJSONRole{
	keys:    []string{"caption", "type", "*", "name"},
	members: map[string]string{"external": "external", "file": "hosted file", "file_upload": "reference"},
}
