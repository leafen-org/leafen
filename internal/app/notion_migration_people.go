package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The scan lists the Notion people and integrations an import names: in
// person values and mentions, and as who made and last changed a page or row.
// The plan says what becomes of each (decision of 2026-11-16, the user's
// "나머지는 모두 추천 진행"): a person is mapped to a workspace member, or
// kept by their Notion name — the default for anyone the plan leaves out —
// or invited, which only someone with an email can be, or left out of the
// person values. An integration (Notion's bot users) is never a member: it
// is kept as an account-less nameplate, or left out. A kept person or
// integration names what they made beside the person running the migration
// (notion_imported_people.go).

const (
	notionMigrationPersonPerson      = "person"
	notionMigrationPersonIntegration = "integration"

	notionMigrationChoiceMember = "member"
	notionMigrationChoiceKeep   = "keep"
	notionMigrationChoiceInvite = "invite"
	notionMigrationChoiceClear  = "clear"
)

// notionMigrationPersonType is what a Notion user the scan read is: an
// integration, or a person — one Notion won't describe among them.
func notionMigrationPersonType(user notionProviderUser) string {
	if user.Object == "bot" || user.Type == "bot" {
		return notionMigrationPersonIntegration
	}
	return notionMigrationPersonPerson
}

// notionMigrationPersonChoice is what the plan does with a person: what it
// says, a member when it names one and says nothing, else keeping them.
func notionMigrationPersonChoice(person notionMigrationPlanPerson) string {
	if action := strings.TrimSpace(person.Action); action != "" {
		return action
	}
	if strings.TrimSpace(person.MemberID) != "" {
		return notionMigrationChoiceMember
	}
	return notionMigrationChoiceKeep
}

// checkPeople reads the plan's people against the scan's: each named once,
// mapped to an eligible member or kept, invited — a person the scan has an
// email for — or left out; an integration only kept or left out. Mapping
// imported people is the workspace owner's, as on screen.
func (c *notionMigrationPlanChecker) checkPeople(
	app core.App, ownerID, workspaceID string, people []notionMigrationPlanPerson, scanned []notionMigrationPerson,
) error {
	if len(people) > 0 {
		if _, err := authorizeWorkspace(app, ownerID, workspaceID, permissionWorkspaceOwner); isPermissionDenied(err) {
			c.problem(notionMigrationProblemPersonInvalid, "", "", "workspace-owner")
		} else if err != nil {
			return err
		}
	}
	known := make(map[string]notionMigrationPerson, len(scanned))
	for _, person := range scanned {
		known[notionMigrationSourceKey(person.ID)] = person
	}
	seen, members, checked := map[string]bool{}, make([]string, 0, len(people)), map[string]bool{}
	for _, person := range people {
		member, choice := strings.TrimSpace(person.MemberID), notionMigrationPersonChoice(person)
		key := notionMigrationSourceKey(person.NotionUserID)
		scan := known[key]
		switch {
		case key == "":
			c.problem(notionMigrationProblemValueInvalid, person.NotionUserID, "", "people")
		case seen[key]:
			c.problem(notionMigrationProblemSourceDuplicate, person.NotionUserID, "", "people")
		case choice == notionMigrationChoiceMember && member == "",
			choice != notionMigrationChoiceMember && member != "",
			choice != notionMigrationChoiceMember && choice != notionMigrationChoiceKeep &&
				choice != notionMigrationChoiceInvite && choice != notionMigrationChoiceClear:
			c.problem(notionMigrationProblemValueInvalid, person.NotionUserID, "", "action")
		case scan.Type == notionMigrationPersonIntegration &&
			(choice == notionMigrationChoiceMember || choice == notionMigrationChoiceInvite):
			c.problem(notionMigrationProblemPersonInvalid, person.NotionUserID, "", notionMigrationPersonIntegration)
		case choice == notionMigrationChoiceInvite && strings.TrimSpace(scan.Email) == "":
			c.problem(notionMigrationProblemPersonInvalid, person.NotionUserID, "", "email")
		default:
			seen[key] = true
			if member != "" {
				members, checked[key] = append(members, member), true
			}
		}
	}
	eligible, err := eligibleImportedPeopleTargets(app, workspaceID, members)
	if err != nil {
		return err
	}
	for _, person := range people {
		if member := strings.TrimSpace(person.MemberID); checked[notionMigrationSourceKey(person.NotionUserID)] && !eligible[member] {
			c.problem(notionMigrationProblemPersonInvalid, person.NotionUserID, "", notionMigrationMemberAsListed(member))
		}
	}
	return nil
}

// notionMigrationConfirmPersonOf is a scanned person or integration the plan
// maps to no member as the plan's one confirmation asks about them
// (notion_migration_confirm.go): what the plan does with them now, what it
// may, and Leafen's recommendation.
func notionMigrationConfirmPersonOf(
	person notionMigrationPerson, members map[string]string, chosen string,
) leafenMCPNotionMigrationConfirmPerson {
	if chosen == "" {
		chosen = notionMigrationChoiceKeep
	}
	entry := leafenMCPNotionMigrationConfirmPerson{
		NotionUserID: person.ID, Name: person.Name, Email: person.Email, Type: notionMigrationPersonPerson, Action: chosen,
		Choices:        []string{notionMigrationChoiceKeep, notionMigrationChoiceClear},
		Recommendation: notionMigrationConfirmNoEmail,
	}
	email := strings.ToLower(strings.TrimSpace(person.Email))
	switch {
	case person.Type == notionMigrationPersonIntegration:
		entry.Type, entry.Recommendation = notionMigrationPersonIntegration, notionMigrationConfirmIntegration
		return entry
	case email != "" && members[email] != "":
		entry.MemberID, entry.Recommendation = members[email], notionMigrationConfirmMapPerson
	case email != "":
		entry.Recommendation = notionMigrationConfirmNoMember
		entry.Choices = append(entry.Choices, notionMigrationChoiceInvite)
	}
	entry.Choices = append(entry.Choices, notionMigrationChoiceMember)
	return entry
}

// notionMigrationInvitePrefix marks, in the people step, a person the plan
// invites, by the e-mail the scan read.
const notionMigrationInvitePrefix = "invite:"

// notionMigrationPeopleTarget is what the people step maps a plan's person
// to: their member, their invitation, leaving them out of person values, or
// nothing for one kept by their Notion name.
func notionMigrationPeopleTarget(person notionMigrationPlanPerson, emails map[string]string) string {
	switch notionMigrationPersonChoice(person) {
	case notionMigrationChoiceMember:
		return strings.TrimSpace(person.MemberID)
	case notionMigrationChoiceClear:
		return importedPeopleCleared
	case notionMigrationChoiceInvite:
		if email := emails[notionMigrationSourceKey(person.NotionUserID)]; email != "" {
			return notionMigrationInvitePrefix + email
		}
	}
	return ""
}

// notionMigrationInviteEmails is the scanned e-mail of each person the plan
// invites.
func notionMigrationInviteEmails(scan notionMigrationScanStore, people []notionMigrationPlanPerson) (map[string]string, error) {
	invited := map[string]bool{}
	for _, person := range people {
		if notionMigrationPersonChoice(person) == notionMigrationChoiceInvite {
			invited[notionMigrationSourceKey(person.NotionUserID)] = true
		}
	}
	emails := map[string]string{}
	if len(invited) == 0 {
		return emails, nil
	}
	err := scan.walk(notionMigrationItemPerson, func(value string) error {
		person := notionMigrationPerson{}
		if err := json.Unmarshal([]byte(value), &person); err != nil {
			return err
		}
		if key := notionMigrationSourceKey(person.ID); invited[key] {
			emails[key] = strings.ToLower(strings.TrimSpace(person.Email))
		}
		return nil
	})
	return emails, err
}

// inviteNotionMigrationPeople invites to the workspace each person the plan
// invites — approving the plan is the invite's press, as choosing someone in
// a share invites them — and has their values name them as an invitee, who
// becomes their account once it proves the address
// (people_value_invitee_runner.go).
func inviteNotionMigrationPeople(app core.App, job storedJob, mappings map[string]string) error {
	for source, target := range mappings {
		email, invite := strings.CutPrefix(target, notionMigrationInvitePrefix)
		if !invite {
			continue
		}
		var organizationID string
		if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": job.WorkspaceID}).Row(&organizationID); err != nil {
			return err
		}
		if _, err := inviteWorkspaceMember(context.Background(), app, job.WorkspaceID, organizationID, job.Owner, email, time.Now()); err != nil {
			return err
		}
		if err := registerOfferedPeopleValueInvitees(app, job.WorkspaceID, job.Owner, []string{email}); err != nil {
			return err
		}
		mappings[source] = peopleValueInviteeID(job.WorkspaceID, email)
	}
	return nil
}
