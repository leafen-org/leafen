package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration asks the person once (2026-11-16: a real move asked where it
// goes, the connection, the scope, four structure choices, the approval, the
// formats' scope and the template rows one by one, and still put the result
// at the top of the person's private pages without asking). Everything else
// stays as it is in Notion; what the person has to decide comes together
// with Leafen's recommendation for each: where the move goes, the Notion
// people no member stands for, what moves, each relation the plan gathers
// into another with the rows that fill both, and each name the plan's option
// values give to options Notion keeps apart with the rows that change. The
// agent asks it in one message with the time it takes, and runs on the yes.

// leafenMCPNotionMigrationConfirm is the one confirmation of a plan.
type leafenMCPNotionMigrationConfirm struct {
	Destination leafenMCPNotionMigrationConfirmDestination `json:"destination"`
	// The Notion people rows name that the plan maps to no member.
	People []leafenMCPNotionMigrationConfirmPerson `json:"people,omitempty"`
	Scope  leafenMCPNotionMigrationConfirmScope    `json:"scope"`
	// Each relation the plan gathers into another, with the rows that fill
	// both (notion_migration_confirm_gathered.go).
	Gathered []leafenMCPNotionMigrationConfirmGathered `json:"gathered,omitempty"`
	// Each name the plan's option values give to options Notion keeps
	// apart, with the rows of each (notion_migration_option_merges.go).
	MergedOptions []leafenMCPNotionMigrationConfirmMergedOptions `json:"merged_options,omitempty"`

	// While the server's scheduled backups are off, that and where they are
	// turned on, for the same message (backup_off_warning.go).
	Backups string `json:"backups,omitempty"`
	// The road the move takes, in the person's language
	// (notion_migration_first_words.go), and where they watch it live once
	// it runs, Leafen's import window: the one question is the message an
	// agent ends its turn on, and the person reads it whole (2026-11-23: five
	// agents told of both mid-way kept them to their own thinking).
	FirstWords string `json:"first_words,omitempty"`
	Link       string `json:"link,omitempty"`
}

type leafenMCPNotionMigrationConfirmDestination struct {
	// The plan's destinationPageId; empty is the top of the person's
	// private pages, which only they see.
	PageID         string `json:"page_id"`
	Title          string `json:"title,omitempty"`
	Recommendation string `json:"recommendation"`
}

type leafenMCPNotionMigrationConfirmPerson struct {
	NotionUserID string `json:"notion_user_id"`
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	// A person or an integration (notion_migration_people.go).
	Type string `json:"type"`
	// A member with the same e-mail, to map the person to.
	MemberID string `json:"member_id,omitempty"`
	// What the plan does with them now and what it may: keep, invite (one
	// with an e-mail), clear, or member with a memberId.
	Action         string   `json:"action"`
	Choices        []string `json:"choices"`
	Recommendation string   `json:"recommendation"`
}

type leafenMCPNotionMigrationConfirmScope struct {
	Roots          int    `json:"roots"`
	Pages          int    `json:"pages"`
	Databases      int    `json:"databases"`
	Rows           int    `json:"rows"`
	Recommendation string `json:"recommendation"`
}

const (
	notionMigrationConfirmPrivateTop = "Empty puts it at the top of the person’s private pages, which only they see. " +
		"Recommend a page their team sees — a teamspace’s (find one with notion-search, or ask where) — " +
		"and keep it private only if the move is theirs alone."
	notionMigrationConfirmKeepPlace = "Keep it: the move goes under this page."
	notionMigrationConfirmMapPerson = "Map them to member_id, the member with the same e-mail: add it to the plan’s people."
	notionMigrationConfirmNoMember  = "No member has their e-mail. Recommend keep: their values and what they made show their Notion name, and the owner can match them to a member later in Settings → People → Imported people (설정 → 사람 → 가져온 사용자). " +
		"Or invite (action invite: approving the plan sends the invitation, and their values become them once they join), or clear them from person values."
	notionMigrationConfirmNoEmail = "Notion gave no e-mail, so they can’t be matched to a member by it or invited: a connection without " +
		"“Read user information including email addresses” (its Capabilities in Notion) gets none — choose it yourself after one yes, under “User capabilities” " +
		"on the connection’s own page in Notion (" + notionConnectionsPage + ", the connection Leafen reads with; in their browser that choice is the one step " +
		"you show them, marked), and run the scan again, and they can be. " +
		"Else recommend keep: their values and what they made show their Notion name, " +
		"and the owner can match them to a member later in Settings → People → Imported people (설정 → 사람 → 가져온 사용자). Or clear them from person values."
	notionMigrationConfirmIntegration = "An integration, never a member. Recommend keep: what it made shows its name as a connection’s, beside you. Or clear it from person values."
	notionMigrationConfirmScopeAsIs   = "Move it as it is: every page and database under the roots chosen, with comments and files."
)

// notionMigrationConfirmOf is the confirmation of a plan state: its plan,
// or its as-is draft before there is one.
func notionMigrationConfirmOf(app core.App, ownerID, workspaceID string, state notionMigrationPlanState) (*leafenMCPNotionMigrationConfirm, error) {
	plan := state.Plan
	if plan == nil {
		plan = state.Draft
	}
	if plan == nil {
		return nil, nil
	}
	stored, err := loadNotionMigration(app, ownerID, workspaceID, state.MigrationID)
	if err != nil {
		return nil, err
	}
	summary, err := loadNotionMigrationSummary(app, stored)
	if err != nil {
		return nil, err
	}
	confirm := &leafenMCPNotionMigrationConfirm{Backups: notionMigrationBackupsOffLine(app, ownerID), FirstWords: notionMigrationFirstWords(app, ownerID)}
	confirm.Destination.PageID = strings.TrimSpace(plan.DestinationPageID)
	confirm.Destination.Recommendation = notionMigrationConfirmPrivateTop
	if confirm.Destination.PageID != "" {
		confirm.Destination.Recommendation = notionMigrationConfirmKeepPlace
		if page, err := app.FindRecordById(pagesCollection, confirm.Destination.PageID); err == nil {
			confirm.Destination.Title = page.GetString("title")
		}
	}
	mapped, chosen := map[string]bool{}, map[string]string{}
	for _, person := range plan.People {
		if strings.TrimSpace(person.MemberID) != "" {
			mapped[notionMigrationSourceKey(person.NotionUserID)] = true
		}
		chosen[notionMigrationSourceKey(person.NotionUserID)] = notionMigrationPersonChoice(person)
	}
	members, err := notionMigrationMembersByEmail(app, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, person := range summary.People {
		if mapped[notionMigrationSourceKey(person.ID)] {
			continue
		}
		entry := notionMigrationConfirmPersonOf(person, members, chosen[notionMigrationSourceKey(person.ID)])
		confirm.People = append(confirm.People, entry)
	}
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &roots); err != nil {
		return nil, err
	}
	confirm.Scope = leafenMCPNotionMigrationConfirmScope{
		Roots: len(roots), Pages: summary.Counts.Pages, Databases: len(summary.Databases),
		Recommendation: notionMigrationConfirmScopeAsIs,
	}
	for _, database := range summary.Databases {
		confirm.Scope.Rows += database.Rows
	}
	confirm.Gathered = notionMigrationConfirmGatheredOf(*plan, summary)
	confirm.MergedOptions = notionMigrationConfirmMergedOptionsOf(*plan, summary)
	if state.Check != nil {
		expected := state.Check.Expected
		confirm.Scope.Pages, confirm.Scope.Databases, confirm.Scope.Rows = expected.Pages, expected.Databases, expected.Rows
	}
	return confirm, nil
}

// notionMigrationMembersByEmail is each member of a workspace — its owner
// and the members a person can be mapped to — by their e-mail.
func notionMigrationMembersByEmail(app core.App, workspaceID string) (map[string]string, error) {
	rows := []struct {
		ID    string `db:"id"`
		Email string `db:"email"`
	}{}
	err := app.DB().NewQuery(`
		SELECT DISTINCT [[user]].[[id]] AS [[id]], [[user]].[[email]] AS [[email]]
		FROM {{users}} [[user]] JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]={:workspaceId}
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]]=[[workspace]].[[organization]]
			AND [[organization_member]].[[user]]=[[user]].[[id]] AND [[organization_member]].[[status]]='active'
		WHERE [[user]].[[email]] != '' AND ([[user]].[[id]]=[[workspace]].[[workspace_owner]] OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
			WHERE [[member]].[[workspace_id]]=[[workspace]].[[id]] AND [[member]].[[user_id]]=[[user]].[[id]]
				AND [[member]].[[role]]='member'))
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&rows)
	if err != nil {
		return nil, err
	}
	members := make(map[string]string, len(rows))
	for _, row := range rows {
		members[strings.ToLower(strings.TrimSpace(row.Email))] = row.ID
	}
	return members, nil
}

// withNotionMigrationConfirm adds the confirmation to a plan's answer, and
// once the plan has no problems says to ask it once and run on the yes.
func withNotionMigrationConfirm(app core.App, ownerID, workspaceID string, output leafenMCPNotionMigrationPlanOutput) leafenMCPNotionMigrationPlanOutput {
	confirm, err := notionMigrationConfirmOf(app, ownerID, workspaceID, output.notionMigrationPlanState)
	if err != nil || confirm == nil {
		return output
	}
	output.Confirm = confirm
	if output.Next == "" && (output.Check == nil || output.Check.OK) {
		// The road in Leafen's words opens the message (2026-11-23: named as
		// confirm.first_words alone, three agents left it out).
		output.Next = strings.Replace(notionMigrationConfirmNext, notionMigrationFirstWordsMark, "“"+confirm.FirstWords+"”", 1)
		// The run's first line goes with the yes that starts it: an agent says
		// the stage as it calls the run (notion_migration_stage_lines.go).
		language, _ := serverTextLanguage(app, ownerID, nil)
		output.Next += " On their yes, as the run starts, its line to the person, in Leafen's words: “" +
			notionMigrationStageLine(language, notionMigrationStageReading) + "”"
	}
	return output
}

const notionMigrationConfirmNext = "Left to do: the one question to the person, in one message, opening with the road their move takes, which their yes agrees to, " +
	"in Leafen's words (confirm.first_words): " + notionMigrationFirstWordsMark + " Then confirm's destination, people and scope, each with its recommendation; " +
	"each relation it gathers with the rows that fill both (confirm.gathered, its recommendation), " +
	"each name its option values give to options Notion keeps apart, with the rows that change (confirm.merged_options, its recommendation), " +
	"what the plan changes and what can't come (the agenda) — in their words; map, preserve, report, merge, into, memberId and the reason codes are the plan's — how long the whole move takes: estimate.say, Leafen's reckoning from the scan; " +
	"when confirm.backups is there, that scheduled backups are off, with its recommendation to turn them on first; " +
	"and where they will watch the move live once it runs, Leafen's import window: " + notionMigrationLinkMark + ". " +
	"With a browser to run it in, the same message says, before Notion opens for it, " + notionMigrationReadingAsk + ". " +
	"Their answer goes into the plan (destinationPageId, people); on their yes, first the settings reading (leafen_notion_migration_formats), " +
	"then leafen_notion_migration_run with its revision: " +
	"the run goes to the end on its own — content, formats, templates and comments, then the check against Notion — and nothing waits on them."

// notionMigrationFirstWordsMark stands for the first words in the one
// question's next until it quotes them.
const notionMigrationFirstWordsMark = "the road in confirm.first_words."

// withNotionMigrationConfirmLink gives a plan answer's one question the
// address where the person will watch the move live, in its confirm and its
// next.
func withNotionMigrationConfirmLink(
	app core.App, authority mcpAuthority, workspaceID string, output leafenMCPNotionMigrationPlanOutput,
) leafenMCPNotionMigrationPlanOutput {
	if output.Confirm == nil {
		return output
	}
	output.Confirm.Link = notionMigrationLiveLink(app, authority, workspaceID)
	output.Next = notionMigrationNamesLink(output.Next, output.Confirm.Link)
	return output
}
