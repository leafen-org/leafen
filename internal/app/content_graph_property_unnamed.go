package app

import "encoding/json"

// graphPropertyNamed says whether a property of valueType must have a name:
// every one but a database's title property, which may have none, as
// Notion's may (seen 2026-11-23: a database made through Notion's API with
// its title property named "" keeps it so in its database and data source,
// and Notion's table heads that column with its icon alone; renaming another
// property to "" Notion refuses). A Notion move brings such a title over
// unnamed (2026-11-23: one database's unnamed title stopped a move of
// 15,747 things).
func graphPropertyNamed(valueType string) bool {
	return valueType != graphValueTypeTitle
}

// notionDataSourceTitleRequest says whether a property a request makes is
// the title: a new source's title may be keyed "" and so unnamed, as
// Notion's API takes it.
func notionDataSourceTitleRequest(raw json.RawMessage) bool {
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	_, title := object["title"]
	return title
}
