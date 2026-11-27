package app

import "github.com/pocketbase/pocketbase/core"

// People's own page-level rule, as Notion's People shows it in its share
// window's "고급" — "Person — 편집 허용", with no menu, counted as "고급 1" on
// People's page and on each person's page (seen 2026-11-16 in Chrome at
// 1440 by its owner, People's page and a person's page in two tabs at two
// times). What it lets a person edit on their own page is what Notion's
// restricted member edits on theirs (seen 2026-11-16 in Chrome at 1440, in
// two tabs at two times, nothing saved): their About and their cover, which
// the profile writes (people_profile.go, people_profile_cover.go); the name
// is no field there, and Email and Membership Type — like Person, kept by
// the server from their account (people_database.go) — take no press. A
// person's page unlinked from People's rule (authorizePeopleDatabasePage)
// has it no more.

// peoplePersonRuleID names that rule, which no change reaches: it is no
// stored rule (database_page_rules.go), so updating or deleting it finds none.
const peoplePersonRuleID = "people-person"

// withPeoplePageRule puts People's own rule first among a page's rules when
// the page is People's database page or a person's page still under People's
// rule; any other page's rules come back as they are.
func withPeoplePageRule(app core.App, page *core.Record, rules publicMutationPageRules) (publicMutationPageRules, error) {
	ruled, database, err := peoplePageRuleHolds(app, page)
	if err != nil || !ruled {
		return rules, err
	}
	rule := publicMutationPageRule{ID: peoplePersonRuleID, DataSourceID: database.DataSourceID,
		PropertyID: database.PersonPropertyID, Role: publicMutationRoleEdit, Fixed: true}
	rules.Rules = append([]publicMutationPageRule{rule}, rules.Rules...)
	for _, property := range rules.Properties {
		if property.DataSourceID == rule.DataSourceID && property.PropertyID == rule.PropertyID {
			return rules, nil
		}
	}
	// A person's page names no properties of its own; the rule's is People's Person.
	rules.Properties = append(rules.Properties, publicMutationRuleProperty{
		DataSourceID: rule.DataSourceID, PropertyID: rule.PropertyID, Name: "Person", Type: "person",
	})
	return rules, nil
}

// peoplePageRuleHolds is whether People's own rule holds on a page: People's
// database page, or a person's page not unlinked from People's rule.
func peoplePageRuleHolds(app core.App, page *core.Record) (bool, peopleDatabase, error) {
	database, _, people, err := peopleDatabasePageOf(app, page)
	if err != nil || !people {
		return false, database, err
	}
	if !isDatabaseRowPageRecord(page) {
		return true, database, nil
	}
	lowered, err := pageShareUnlinked(app, page.Id)
	return !lowered, database, err
}
