package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// A shared vault follows Leafen groups its owners and administrators link
// to it — from the web vault's Groups, by a Leafen group's name, or from
// Leafen's Vault app. A linked group is a Bitwarden group there: its
// people are whoever is in the Leafen group now (SCIM's groups too) and
// keeps a vault here; collections are given to it in the web vault. Who
// joins waits to be confirmed like anyone added by hand; who leaves every
// linked group, having come by one, is taken out and the others are told to
// change what they could see.

// groupLink is a Leafen group linked to a shared vault.
type groupLink struct {
	ID          string             `json:"id"`
	Group       string             `json:"group"`
	Name        string             `json:"name"`
	Collections []collectionAccess `json:"collections"`
}

func (org organization) links() []groupLink {
	links := []groupLink{}
	_ = json.Unmarshal([]byte(org.row.Text("Groups")), &links)
	return links
}

func (s *store) saveLinks(org organization, links []groupLink) error {
	encoded, _ := jsonText(links)
	if err := s.organizations.Update(org.row.ID, map[string]any{"Groups": encoded, "Revision": float64(nowMs())}); err != nil {
		return err
	}
	s.orgChanged(org.row.ID)
	return nil
}

// leafenGroup is one of the organization's groups as Leafen lists them.
type leafenGroup struct {
	ID, Name string
}

// leafenGroups reads the organization's groups, a window of them at a time
// to the last.
func (s *store) leafenGroups() ([]leafenGroup, error) {
	groups := []leafenGroup{}
	cursor := ""
	for {
		path := "/app-groups"
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}
		answer, err := leafen.API(leafen.APIRequest{Tenancy: s.tenancy, Method: "GET", Path: path})
		if err != nil {
			return nil, err
		}
		listed := struct {
			Groups []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"groups"`
			NextCursor string `json:"nextCursor"`
		}{}
		if answer.Status != 200 || json.Unmarshal(answer.Body, &listed) != nil {
			return nil, errors.New("the organization's groups couldn't be read")
		}
		for _, group := range listed.Groups {
			groups = append(groups, leafenGroup{ID: group.ID, Name: group.Name})
		}
		if listed.NextCursor == "" || listed.NextCursor == cursor {
			return groups, nil
		}
		cursor = listed.NextCursor
	}
}

// link links a Leafen group to a shared vault, found by its id or name.
func (s *store) link(org organization, group string) (groupLink, string, error) {
	groups, err := s.leafenGroups()
	if err != nil {
		return groupLink{}, "", err
	}
	at := slices.IndexFunc(groups, func(found leafenGroup) bool {
		return found.ID == group || strings.EqualFold(strings.TrimSpace(found.Name), strings.TrimSpace(group))
	})
	if at < 0 {
		return groupLink{}, fmt.Sprintf("Leafen has no group called %q: a shared vault's groups are your organization's groups in Leafen.", group), nil
	}
	links := org.links()
	for _, linked := range links {
		if linked.Group == groups[at].ID {
			return linked, "", nil
		}
	}
	made := groupLink{ID: bitwardenID(hexID()), Group: groups[at].ID, Name: groups[at].Name, Collections: []collectionAccess{}}
	if err := s.saveLinks(org, append(links, made)); err != nil {
		return groupLink{}, "", err
	}
	return made, "", nil
}

func groupAnswer(org organization, link groupLink, object string) map[string]any {
	collections := []any{}
	for _, given := range link.Collections {
		collections = append(collections, map[string]any{"id": given.ID, "readOnly": given.ReadOnly, "hidePasswords": given.HidePasswords, "manage": given.Manage})
	}
	return map[string]any{"id": link.ID, "organizationId": org.id(), "name": link.Name, "accessAll": false, "externalId": link.Group,
		"collections": collections, "object": object}
}

// groupRoute answers the web vault's Groups for a shared vault.
func (c *call) groupRoute(org organization, m member, rest string) (leafen.EndpointAnswer, error) {
	links := org.links()
	id, tail, _ := strings.Cut(rest, "/")
	switch c.method + " " + id {
	case "GET ", "GET details":
		listed := []any{}
		for _, link := range links {
			listed = append(listed, groupAnswer(org, link, map[bool]string{true: "groupDetails", false: "group"}[id == "details"]))
		}
		return list(listed), nil
	case "POST ":
		if !m.manages() {
			return problem(403, "Only the shared vault's owners and administrators link groups."), nil
		}
		input := struct {
			Name        string             `json:"name"`
			Collections []collectionAccess `json:"collections"`
		}{}
		if !c.decode(&input) || strings.TrimSpace(input.Name) == "" {
			return problem(400, "Give the name of a group in your Leafen organization."), nil
		}
		made, refusal, err := c.store.link(org, input.Name)
		if refusal != "" || err != nil {
			return problem(400, refusal), err
		}
		fresh, err := c.store.organizationByID(org.id())
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if made, refusal, err = c.store.giveGroup(fresh, made.ID, input.Collections); refusal != "" || err != nil {
			return problem(400, refusal), err
		}
		if err := c.store.follow(fresh, 40); err != nil {
			leafen.Log("the shared vault's groups weren't followed: " + err.Error())
		}
		return reply(200, groupAnswer(fresh, made, "group")), nil
	case "DELETE ", "POST delete":
		return problem(400, "Remove groups one at a time."), nil
	}
	at := slices.IndexFunc(links, func(link groupLink) bool { return strings.EqualFold(link.ID, id) })
	if at < 0 {
		return problem(404, "That group isn't linked to the shared vault."), nil
	}
	switch c.method + " " + tail {
	case "GET ", "GET details":
		return reply(200, groupAnswer(org, links[at], "groupDetails")), nil
	case "GET users":
		members, err := c.store.membersOf(org)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		ids := []any{}
		for _, other := range members {
			if slices.Contains(other.groups(), links[at].ID) {
				ids = append(ids, other.id())
			}
		}
		return reply(200, ids), nil
	}
	if !m.manages() {
		return problem(403, "Only the shared vault's owners and administrators change its groups."), nil
	}
	switch c.method + " " + tail {
	case "PUT ", "POST ":
		input := struct {
			Collections []collectionAccess `json:"collections"`
		}{}
		if !c.decode(&input) {
			return problem(400, "The change couldn't be read."), nil
		}
		changed, refusal, err := c.store.giveGroup(org, links[at].ID, input.Collections)
		if refusal != "" || err != nil {
			return problem(400, refusal), err
		}
		return reply(200, groupAnswer(org, changed, "group")), nil
	case "PUT users":
		return problem(400, "Who is in a group comes from Leafen: change the group there."), nil
	case "DELETE ", "POST delete":
		if err := c.store.saveLinks(org, slices.Delete(links, at, at+1)); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		fresh, err := c.store.organizationByID(org.id())
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if err := c.store.follow(fresh, 40); err != nil {
			leafen.Log("the shared vault's groups weren't followed: " + err.Error())
		}
		return reply(200, nil), nil
	}
	return problem(404, "Not found."), nil
}

// giveGroup sets the collections a linked group's people may use.
func (s *store) giveGroup(org organization, linkID string, given []collectionAccess) (groupLink, string, error) {
	checked, ok, err := s.collectionsGiven(org, given)
	if err != nil || !ok {
		return groupLink{}, "Choose the shared vault's collections.", err
	}
	links := org.links()
	at := slices.IndexFunc(links, func(link groupLink) bool { return strings.EqualFold(link.ID, linkID) })
	if at < 0 {
		return groupLink{}, "That group isn't linked to the shared vault.", nil
	}
	links[at].Collections = checked
	return links[at], "", s.saveLinks(org, links)
}

// giveGroupsCollection sets which linked groups may use a collection, as
// the web vault's collection dialog lists them.
func (s *store) giveGroupsCollection(org organization, collection string, groups []collectionAccess) error {
	links := org.links()
	for at := range links {
		kept := []collectionAccess{}
		for _, access := range links[at].Collections {
			if !strings.EqualFold(access.ID, collection) {
				kept = append(kept, access)
			}
		}
		for _, group := range groups {
			if strings.EqualFold(group.ID, links[at].ID) {
				kept = append(kept, collectionAccess{ID: collection, ReadOnly: group.ReadOnly, HidePasswords: group.HidePasswords, Manage: group.Manage})
			}
		}
		links[at].Collections = kept
	}
	return s.saveLinks(org, links)
}

// groupsOfCollection are the linked groups given a collection.
func groupsOfCollection(org organization, collection string) []any {
	found := []any{}
	for _, link := range org.links() {
		for _, access := range link.Collections {
			if strings.EqualFold(access.ID, collection) {
				found = append(found, map[string]any{"id": link.ID, "readOnly": access.ReadOnly, "hidePasswords": access.HidePasswords, "manage": access.Manage})
			}
		}
	}
	return found
}

// follow brings a shared vault's members in line with its linked groups:
// their people with a vault here join (waiting to be confirmed), and those
// who came by a group and are in none of them now go. Leafen is asked once
// for each hundred people and hundred linked groups, however many there are.
// budget bounds those calls when a person waits on the answer (0 is none, as
// for the schedule): past it, the schedule follows within ten minutes.
func (s *store) follow(org organization, budget int) error {
	links := org.links()
	members, err := s.membersOf(org)
	if err != nil {
		return err
	}
	if len(links) == 0 && !slices.ContainsFunc(members, func(m member) bool { return m.Source == "group" || len(m.groups()) > 0 }) {
		return nil
	}
	accounts := map[string]account{}
	for row, err := range s.accounts.Rows(leafen.Query{Where: []leafen.Where{{Column: "Status", Operator: "equals", Value: statusActive}}}) {
		if err != nil {
			return err
		}
		acct := accountOf(row)
		accounts[acct.Person] = acct
	}
	people := make([]string, 0, len(accounts))
	for person := range accounts {
		people = append(people, person)
	}
	slices.Sort(people)
	// Each linked group's people among those with a vault, in the links'
	// order.
	linkOf := map[string]string{}
	groups := []string{}
	for _, link := range links {
		linkOf[link.Group] = link.ID
		groups = append(groups, link.Group)
	}
	in, calls := map[string]map[string]bool{}, 0
	for start := 0; start < len(people); start += 100 {
		page := people[start:min(start+100, len(people))]
		for from := 0; from < len(groups); from += 100 {
			if calls++; budget > 0 && calls > budget {
				return errors.New("too many people to follow while someone waits; the schedule follows them")
			}
			found, err := peopleInGroups(s.tenancy, page, groups[from:min(from+100, len(groups))])
			if err != nil {
				return err
			}
			for person, theirs := range found {
				for _, group := range theirs {
					if in[person] == nil {
						in[person] = map[string]bool{}
					}
					in[person][linkOf[group]] = true
				}
			}
		}
	}
	inLinks := map[string][]string{}
	for person, linked := range in {
		for _, link := range links {
			if linked[link.ID] {
				inLinks[person] = append(inLinks[person], link.ID)
			}
		}
	}
	gone, joined := []member{}, 0
	known := map[string]bool{}
	for _, m := range members {
		known[m.Person] = true
		now := inLinks[m.Person]
		switch {
		case m.Source == "group" && len(now) == 0:
			gone = append(gone, m)
		case !slices.Equal(m.groups(), now):
			encoded, _ := jsonText(now)
			if err := s.members.Update(m.row.ID, map[string]any{"Groups": encoded, "Revision": float64(nowMs())}); err != nil {
				return err
			}
		}
	}
	for person, now := range inLinks {
		if known[person] {
			continue
		}
		acct := accounts[person]
		encoded, _ := jsonText(now)
		rowID, err := s.addMember(org.row.ID, acct, roleUser, memberAccepted, "", "group", false, nil)
		if err != nil {
			return err
		}
		if err := s.members.Update(rowID, map[string]any{"Groups": encoded}); err != nil {
			return err
		}
		joined++
	}
	if len(gone) > 0 {
		if err := s.removeMembers(org, gone); err != nil {
			return err
		}
	}
	if joined > 0 {
		s.orgChanged(org.row.ID)
		s.askToConfirm(org, members, joined)
	}
	return nil
}

// peopleInGroups says which of the people are in which of the Leafen
// groups (up to a hundred of each) and may still use the app.
func peopleInGroups(tenancy string, people, groups []string) (map[string][]string, error) {
	answer, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: "GET", Path: "/app-people",
		Query: "ids=" + url.QueryEscape(strings.Join(people, ",")) + "&groups=" + url.QueryEscape(strings.Join(groups, ","))})
	if err != nil {
		return nil, err
	}
	listed := struct {
		People []struct {
			ID     string   `json:"id"`
			Groups []string `json:"groups"`
		} `json:"people"`
	}{}
	if answer.Status != 200 || json.Unmarshal(answer.Body, &listed) != nil {
		return nil, errors.New("the groups' people couldn't be read")
	}
	found := map[string][]string{}
	for _, person := range listed.People {
		found[person.ID] = person.Groups
	}
	return found, nil
}

// followGroups is the schedule that keeps every shared vault in line with
// its groups, and goes on deleting closed ones.
//
//go:wasmexport follow
func followGroups() int32 {
	call := leafen.ScheduleCall{}
	if err := leafen.InputJSON(&call); err != nil || call.Tenancy == "" {
		return fail("the schedule names no organization")
	}
	s, err := openStore(call.Tenancy)
	if errors.Is(err, errNotSetUp) {
		return 0
	}
	if err != nil {
		return fail(err.Error())
	}
	budget := 120
	for row, err := range s.organizations.Rows(leafen.Query{}) {
		if err != nil {
			return fail(err.Error())
		}
		org := organizationOf(row)
		if row.Option("Status") != "active" {
			if err := clearOrganization(s, row.ID, &budget); err != nil {
				return fail(err.Error())
			}
			continue
		}
		if err := s.follow(org, 0); err != nil {
			leafen.Log("shared vault not followed: " + err.Error())
		}
	}
	return 0
}
