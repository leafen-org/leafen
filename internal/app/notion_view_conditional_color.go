package app

// A view's conditional colors are its Leafen settings (conditionalColors),
// made in its "조건부 색상" menu; the official view API doesn't give them.
// A timeline's color_by is a switch of its own, as Notion's (seen
// 2026-11-12): given true or false it is kept and read back as given, null
// takes it away, and it leaves the timeline's conditional colors and how
// the screen draws its bars as they are — a timeline given true without a
// color setting draws them uncolored, one given false keeps its settings'
// colors, and a setting made on the screen doesn't give color_by. Another
// type's color_by is taken and left out (notionViewTakesField).

func applyOfficialTimelineColorBy(value any, config map[string]any) error {
	switch value {
	case nil:
		delete(config, "timelineColorBy")
		return nil
	case true, false:
		config["timelineColorBy"] = value
		return nil
	}
	return notionViewFieldCheck(false, "color_by")
}
