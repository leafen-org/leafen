package app

import (
	"fmt"
	"strings"
)

// The migration report names many things of one kind once (2026-11-20, the
// person's word on an agent's reading of a report that said "표 보기의
// 줄바꿈: 계약 관리 (Notion에서 읽지 못함)" a line for each view, dozens of
// times): a kind with more than one is one line saying how many of what and
// why, the ones it is folded under it in a toggle; a kind with one is that
// one's line. The report's sections each group their own lines this way —
// what Notion's API didn't give (notion_migration_formats_report.go), what
// didn't come whole with its pages (notion_import_not_whole.go), the files
// not copied (notion_migration_report_details.go), the check's differences
// (notion_migration_verify_step.go), the people (notion_migration_people_report.go)
// and what the source didn't give a run again — and the agent's
// leafen_notion_migration_status says each group in the same words
// (mcp_notion_migration_status.go), so its end report is grouped as the page is.

// notionMigrationReportGroupShown is how many of a group's items the report
// lists in its toggle; the rest are counted.
const notionMigrationReportGroupShown = 100

// notionMigrationReportGroup is a kind of thing the report names: its line
// when there are several (Say), the line of the one when there is only one
// (One), the ones it is as the toggle lists them and how many there are in
// all (Total, when the list read was cut), and how many things they stand
// for (Count: a database's rows are one item and many rows).
type notionMigrationReportGroup struct {
	Key   string
	Say   string
	One   string
	Items []string
	Total int
	Count int
}

// leafenMCPNotionReportGroup is a group as the agent's status gives it: the
// report's line for it and how many things it counts.
type leafenMCPNotionReportGroup struct {
	Say   string `json:"say"`
	Count int    `json:"count"`
}

// notionMigrationReportMore is the toggle's last line for the items past those
// it lists.
var notionMigrationReportMore = map[string]string{
	accountLanguageKo: "그 밖에 %d개",
	accountLanguageEn: "%d more",
}

// writeNotionMigrationReportGroups writes groups depth levels in, as the page
// reads its blocks back: a group of one as its line, a larger one as a
// toggle whose summary is its line and whose blocks are its items.
func writeNotionMigrationReportGroups(line func(string, ...any), language string, depth int, groups []notionMigrationReportGroup) {
	more := cmpOr(notionMigrationReportMore[language], notionMigrationReportMore[accountLanguageEn])
	tabs := strings.Repeat("\t", depth)
	for _, group := range groups {
		if group.One != "" {
			line("%s- %s", tabs, group.One)
			continue
		}
		line("%s<details>", tabs)
		line("%s<summary>%s</summary>", tabs, group.Say)
		shown := group.Items
		if len(shown) > notionMigrationReportGroupShown {
			shown = shown[:notionMigrationReportGroupShown]
		}
		for _, item := range shown {
			line("%s\t- %s", tabs, item)
		}
		if rest := max(group.Total, len(group.Items)) - len(shown); rest > 0 {
			line("%s\t- %s", tabs, fmt.Sprintf(more, rest))
		}
		line("%s</details>", tabs)
	}
}

// notionMigrationReportGroupsCount is how many things groups stand for in
// all, as their lines count them.
func notionMigrationReportGroupsCount(groups []notionMigrationReportGroup) int {
	count := 0
	for _, group := range groups {
		count += group.Count
	}
	return count
}

// notionMigrationReportGroupsSaid are groups as the agent's status gives them.
func notionMigrationReportGroupsSaid(groups []notionMigrationReportGroup) []leafenMCPNotionReportGroup {
	said := make([]leafenMCPNotionReportGroup, 0, len(groups))
	for _, group := range groups {
		said = append(said, leafenMCPNotionReportGroup{Say: cmpOr(group.One, group.Say), Count: group.Count})
	}
	return said
}

// notionMigrationReportGrouping gathers items into groups by a key, the groups
// in the order their first item came.
type notionMigrationReportGrouping struct {
	order  []string
	groups map[string]*notionMigrationReportGroup
}

func newNotionMigrationReportGrouping() *notionMigrationReportGrouping {
	return &notionMigrationReportGrouping{groups: map[string]*notionMigrationReportGroup{}}
}

// add puts an item in the group key names; weight is how many things the item
// stands for.
func (grouping *notionMigrationReportGrouping) add(key, item string, weight int) {
	group := grouping.group(key)
	group.Items = append(group.Items, item)
	group.Total++
	group.Count += weight
}

// tally says the group key names has total items in all and stands for
// count things, though fewer of its items were listed (or none).
func (grouping *notionMigrationReportGrouping) tally(key string, total, count int) {
	group := grouping.group(key)
	group.Total, group.Count = max(total, len(group.Items)), max(count, group.Count)
}

// group is the group key names, made when it has no item yet.
func (grouping *notionMigrationReportGrouping) group(key string) *notionMigrationReportGroup {
	group, found := grouping.groups[key]
	if !found {
		group = &notionMigrationReportGroup{Key: key}
		grouping.groups[key] = group
		grouping.order = append(grouping.order, key)
	}
	return group
}

// list is the groups, those keys names first in its order and the rest as
// their first item came, each said by say: its line, and the line of its one
// item when it has only one.
func (grouping *notionMigrationReportGrouping) list(keys []string, say func(*notionMigrationReportGroup)) []notionMigrationReportGroup {
	out, placed := []notionMigrationReportGroup{}, map[string]bool{}
	for _, key := range append(append([]string{}, keys...), grouping.order...) {
		if group, found := grouping.groups[key]; found && !placed[key] {
			say(group)
			out, placed[key] = append(out, *group), true
		}
	}
	return out
}
