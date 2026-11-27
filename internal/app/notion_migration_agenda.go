package app

import (
	"fmt"
	"strings"
)

// What the plan's agenda says beside the scan's issues (settleAgenda): all
// that this move won't bring as it is, item by item, so the person hears it
// before they approve and the report lists it again — not only what the
// scan met, but what Leafen can't bring yet: how pages and views look until
// they are read in the person's Notion (notion_migration_formats_read.go),
// the Notion people no member stands for, and what no Notion move brings
// (who sees what, version history, automations) — and each formula or
// rollup a merge or a split brings beside its target's of the name.

const (
	// How pages, templates and views look — with a view's own default
	// template, forms' words and views' conditions on deleted properties —
	// when nothing has read them in the person's signed-in Notion yet: all
	// the reader hands in (notion_migration_formats_read.go) but the blocks
	// with comments and their resolved discussions, which the estimate and
	// the report say apart.
	notionMigrationAgendaFormatsNotRead = "formats_not_read"
	// A Notion person the plan maps to no member: their values keep the
	// Notion name, and what they made shows the person importing.
	notionMigrationAgendaPersonNotMember = "person_not_member"
	// What no Notion move brings.
	notionMigrationAgendaSharingNotMoved     = "sharing_not_moved"
	notionMigrationAgendaHistoryNotMoved     = "history_not_moved"
	notionMigrationAgendaAutomationsNotMoved = "automations_not_moved"
)

// notionMigrationScanAgenda is the scan's issues to settle under a plan: a
// connection Notion keeps comments from matters only to a plan bringing them.
func notionMigrationScanAgenda(plan notionMigrationPlan, issues []notionMigrationIssue) []notionMigrationIssue {
	kept := make([]notionMigrationIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.Code != notionMigrationIssueCommentsUnreadable || plan.Comments == "import" {
			kept = append(kept, issue)
		}
	}
	return kept
}

// notionMigrationLeftItems are the agenda's items beyond the scan's issues.
func (c *notionMigrationPlanChecker) notionMigrationLeftItems(plan notionMigrationPlan, summary notionMigrationSummary) []notionMigrationIssue {
	items := []notionMigrationIssue{}
	chosen := map[string]string{}
	for _, person := range plan.People {
		chosen[notionMigrationSourceKey(person.NotionUserID)] = notionMigrationPersonChoice(person)
	}
	for _, person := range summary.People {
		choice, named := chosen[notionMigrationSourceKey(person.ID)]
		if !named {
			choice = notionMigrationChoiceKeep
		}
		if person.Type == notionMigrationPersonIntegration || choice == notionMigrationChoiceMember || choice == notionMigrationChoiceInvite {
			continue
		}
		items = append(items, notionMigrationIssue{
			Code: notionMigrationAgendaPersonNotMember, SourceID: person.ID, SourceTitle: person.Name, Detail: choice,
		})
	}
	// A formula or rollup a merge or a split brings beside its target's of
	// the name (notion_migration_merge_computed_plan.go).
	databases := map[string]notionMigrationDatabase{}
	for _, database := range summary.Databases {
		databases[notionMigrationSourceKey(database.ID)] = database
	}
	items = append(items, newNotionMergePlanColumns(plan, databases).besideItems()...)
	// A bundle from another tool has no Notion to read or share from.
	if strings.TrimSpace(c.stored.SourceLabel) != "" {
		return items
	}
	reader, found, err := loadNotionMigrationFormatsReader(c.app, c.stored.ID)
	if err != nil {
		c.err = err
	}
	if !found || reader.ReadAt == 0 {
		items = append(items, notionMigrationIssue{Code: notionMigrationAgendaFormatsNotRead})
	} else {
		// Views' conditions on deleted properties (notion_migration_formats_deleted_filters.go).
		items = append(items, c.notionMigrationDeletedFilterItems(reader)...)
	}
	for _, code := range []string{
		notionMigrationAgendaSharingNotMoved, notionMigrationAgendaHistoryNotMoved, notionMigrationAgendaAutomationsNotMoved,
	} {
		items = append(items, notionMigrationIssue{Code: code})
	}
	return items
}

// notionMigrationIssueLine is a report label with its count, or the label
// alone for what is said once.
func notionMigrationIssueLine(label string, count int) string {
	if strings.Contains(label, "%") {
		return fmt.Sprintf(label, count)
	}
	return label
}

func init() {
	for code, words := range map[string][2]string{
		notionMigrationAgendaFormatsNotRead: {
			"Notion 화면에서 꾸밈을 읽지 않았습니다 — 페이지·행·템플릿의 전체 너비·작은 글씨·글꼴, 페이지 배치, 보드 열 순서·숨김·색, 줄바꿈, 손으로 정한 행 순서, 폼 제목·설명·질문은 리펜 기본값으로 오고, 한 보기에만 정한 기본 템플릿은 데이터베이스의 기본 템플릿으로 오며, 폼은 응답을 받지 않는 채로 옵니다. Notion에서 지운 속성을 가리키는 보기 조건이 있는지도 알 수 없습니다",
			"How Notion looks wasn’t read in Notion — pages’, rows’ and templates’ full width, small text and font, page layouts, board columns’ order, hiding and colors, wrap, rows put in order by hand and forms’ titles, descriptions and questions come at Leafen’s defaults, a default template set for one view only comes as the database’s default, and forms come taking no answers. Whether views’ conditions name properties deleted in Notion can’t be told either",
		},
		notionMigrationAgendaPersonNotMember: {
			"리펜 멤버로 잇지 않은 Notion 사람 %d명 — 사람 값은 Notion 이름으로 남고, 그 사람이 만들거나 고친 행은 옮긴 사람으로 보입니다",
			"%d Notion people no member stands for — their values keep the Notion name, and rows they made or changed show the person importing",
		},
		notionMigrationAgendaSharingNotMoved: {
			"공유·권한은 옮기지 않습니다 — 리펜에서는 팀스페이스와 공유가 누가 보는지 정합니다",
			"Sharing and permissions don’t move — in Leafen, teamspaces and shares decide who sees what",
		},
		notionMigrationAgendaHistoryNotMoved: {
			"버전 기록은 옮기지 않습니다 — 옮긴 때의 내용만 옵니다",
			"Version history doesn’t move — the content comes as it is at the move",
		},
		notionMigrationAgendaAutomationsNotMoved: {
			"자동화는 옮기지 못합니다 — Notion이 연결에 주지 않습니다",
			"Automations don’t move — Notion doesn’t give them to a connection",
		},
	} {
		notionMigrationReportText[accountLanguageKo]["issue:"+code] = words[0]
		notionMigrationReportText[accountLanguageEn]["issue:"+code] = words[1]
	}
}
