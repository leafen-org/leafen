package app

import "unicode/utf8"

// The content tab's own name, icon and description, a tabbed layout's
// "콘텐츠" as Notion's "설정 보기" keeps them, and its place among the
// tabs: after as many data tabs as Position says, first when none, as
// Notion's builder moves it among them (seen 2026-11-24).
type databasePageLayoutContentTab struct {
	Name        string `json:"name,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	Position    int    `json:"position,omitempty"`
}

// kept says why a layout's content tab can't be kept beside its tabs (how
// many data tabs it has), or nothing; a place past the last tab is after
// them all, as a tab taken out of a layout leaves it. A layout without one
// has nothing to refuse.
func (tab *databasePageLayoutContentTab) kept(tabs int) error {
	switch {
	case tab == nil:
		return nil
	case utf8.RuneCountInString(tab.Name) > databasePageLayoutMaxName:
		return databasePageLayoutRefused("contentTab.name is at most %d characters", databasePageLayoutMaxName)
	case utf8.RuneCountInString(tab.Description) > databasePageLayoutMaxDescription:
		return databasePageLayoutRefused("contentTab.description is at most %d characters", databasePageLayoutMaxDescription)
	case len(tab.Icon) > databasePageLayoutMaxIcon:
		return databasePageLayoutRefused("contentTab.icon is at most %d bytes", databasePageLayoutMaxIcon)
	case tab.Position < 0:
		return databasePageLayoutRefused("contentTab.position is 0 or more, not %d", tab.Position)
	}
	tab.Position = min(tab.Position, tabs)
	return nil
}
