package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The migration report says what became of each Notion person and
// integration the scan found that the plan maps to no member, as the plan
// chose (notion_migration_people.go), after the count of pages and rows they
// made or last changed: each outcome once, with how many and their names
// folded under it (notion_migration_report_groups.go).

var notionMigrationPeopleReportText = map[string]map[string]string{
	accountLanguageKo: {
		"head":                                "Notion 사람과 통합 %d명:",
		notionMigrationChoiceMember:           "멤버와 이음",
		notionMigrationChoiceKeep:             "Notion 이름으로 둠 — 값과 만든 것에 이 이름이 보이고, 값과 멘션은 " + notionMigrationPeoplePlaceLink(accountLanguageKo) + "에서 멤버와 이을 수 있습니다",
		notionMigrationChoiceInvite:           "%s로 초대함 — 가입하면 값이 그 사람이 됩니다",
		notionMigrationChoiceClear:            "사람 값에서 뺌 — 멘션은 이름 그대로입니다",
		notionMigrationPersonIntegration:      "통합 — 만든 것에 연결로 보입니다",
		"integrationClear":                    "통합 — 사람 값에서 뺌",
		notionMigrationChoiceInvite + "Group": "초대함 — 가입하면 값이 그 사람이 됩니다",
		"invited":                             "%s (%s)",
		"group":                               "그중 %[2]d명: %[1]s",
	},
	accountLanguageEn: {
		"head":                                "%d Notion people and integrations:",
		notionMigrationChoiceMember:           "matched to a member",
		notionMigrationChoiceKeep:             "kept by their Notion name — their values and what they made show it, and their values and mentions can be matched to a member in " + notionMigrationPeoplePlaceLink(accountLanguageEn),
		notionMigrationChoiceInvite:           "invited at %s — their values become them once they join",
		notionMigrationChoiceClear:            "left out of person values — mentions keep their name",
		notionMigrationPersonIntegration:      "an integration — what it made shows it as a connection",
		"integrationClear":                    "an integration — left out of person values",
		notionMigrationChoiceInvite + "Group": "invited — their values become them once they join",
		"invited":                             "%s (%s)",
		"group":                               "%[2]d of them: %[1]s",
	},
}

// Notion people a run kept by their Notion name are matched to members later
// where every imported person is, Settings › People › Imported people
// (ImportedPeopleMapping.tsx), not on the import screen (2026-11-19: the plan
// told the agent "later in Settings' import", where a person found only the
// finished import's buttons). The report links there.

// notionMigrationPeoplePlace is that place's address: the people section,
// opened at its imported people.
const notionMigrationPeoplePlace = "/settings?section=people&part=imported-people"

// notionMigrationPeoplePlaceLink is that place named as the screen names it,
// linked, in the report's Markdown.
func notionMigrationPeoplePlaceLink(language string) string {
	if language == accountLanguageKo {
		return "[설정 → 사람 → 가져온 사용자](" + notionMigrationPeoplePlace + ")"
	}
	return "[Settings → People → Imported people](" + notionMigrationPeoplePlace + ")"
}

// notionMigrationPeopleKept counts the Notion people, not integrations, the
// plan leaves by their Notion name: those it keeps, and those it doesn't name.
func notionMigrationPeopleKept(plan notionMigrationPlan, summary notionMigrationSummary) int {
	chosen := map[string]string{}
	for _, person := range plan.People {
		chosen[notionMigrationSourceKey(person.NotionUserID)] = notionMigrationPersonChoice(person)
	}
	kept := 0
	for _, person := range summary.People {
		choice := chosen[notionMigrationSourceKey(person.ID)]
		if person.Type != notionMigrationPersonIntegration && (choice == "" || choice == notionMigrationChoiceKeep) {
			kept++
		}
	}
	return kept
}

// recordNotionMigrationPeopleKept keeps, as a run ends, how many Notion people
// it left by their Notion name, for the import screen to say where they are
// matched later.
func recordNotionMigrationPeopleKept(app core.App, job storedJob, migrationID string) error {
	stored, err := loadNotionMigration(app, job.Owner, job.WorkspaceID, migrationID)
	if err != nil {
		return err
	}
	plan := notionMigrationPlan{}
	if err := json.Unmarshal([]byte(stored.PlanJSON), &plan); err != nil {
		return err
	}
	summary, err := loadNotionMigrationSummary(app, stored)
	if err != nil {
		return err
	}
	kept := notionMigrationPeopleKept(plan, summary)
	return updateNotionMigrationRunReport(app, migrationID, func(report *notionMigrationRunReport) {
		report.PeopleKept = kept
	})
}

// notionMigrationPeopleReport writes the count of pages and rows a Notion
// person or integration matched to no member made or last changed, and what
// became of each person and integration the scan found.
func notionMigrationPeopleReport(
	language, authoredLine string, authored int, plan notionMigrationPlan, summary notionMigrationSummary,
	line func(string, ...any),
) {
	if authored > 0 {
		line("- "+authoredLine, authored)
	}
	text := notionMigrationPeopleReportText[language]
	if text == nil {
		text = notionMigrationPeopleReportText[accountLanguageEn]
	}
	if len(summary.People) == 0 {
		return
	}
	chosen := map[string]string{}
	for _, person := range plan.People {
		chosen[notionMigrationSourceKey(person.NotionUserID)] = notionMigrationPersonChoice(person)
	}
	line("- "+text["head"], len(summary.People))
	// Those of one outcome are one line, their names folded under it.
	grouping, lines := newNotionMigrationReportGrouping(), map[string]string{}
	for _, person := range summary.People {
		name := strings.TrimSpace(person.Name)
		if name == "" {
			name = person.ID
		}
		choice := chosen[notionMigrationSourceKey(person.ID)]
		if choice == "" {
			choice = notionMigrationChoiceKeep
		}
		key, outcome, named := choice, text[choice], name
		switch {
		case person.Type == notionMigrationPersonIntegration && choice == notionMigrationChoiceClear:
			key, outcome = "integrationClear", text["integrationClear"]
		case person.Type == notionMigrationPersonIntegration:
			key, outcome = notionMigrationPersonIntegration, text[notionMigrationPersonIntegration]
		case choice == notionMigrationChoiceInvite:
			email := strings.ToLower(strings.TrimSpace(person.Email))
			outcome = strings.Replace(outcome, "%s", email, 1)
			named = fmt.Sprintf(text["invited"], name, email)
		}
		grouping.add(key, named, 1)
		lines[key] = fmt.Sprintf("%s: %s", name, outcome)
	}
	writeNotionMigrationReportGroups(line, language, 1, grouping.list(nil, func(group *notionMigrationReportGroup) {
		outcome := cmpOr(text[group.Key+"Group"], text[group.Key])
		group.Say = fmt.Sprintf(text["group"], outcome, group.Count)
		if group.Total == 1 {
			group.One = lines[group.Key]
		}
	}))
}
