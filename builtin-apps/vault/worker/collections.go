package main

import (
	"encoding/json"
	"errors"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// A shared vault's collections: a name its members' devices encrypt with
// the vault's key, and who may see, change or manage the items in it.
// Owners and administrators make and remove them; a member who manages one
// renames it and chooses its people.

func jsonText(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func collectionAnswer(row leafen.Row, object string) map[string]any {
	var external any
	if id := row.Text("External id"); id != "" {
		external = id
	}
	return map[string]any{"id": bitwardenID(row.ID), "organizationId": bitwardenID(row.Text("Organization")), "name": row.Text("Name"),
		"externalId": external, "object": object}
}

// collectionDetails is a collection as one member sees it.
func collectionDetails(row leafen.Row, m member) map[string]any {
	answer := collectionAnswer(row, "collectionDetails")
	access, given := m.access(bitwardenID(row.ID))
	answer["readOnly"], answer["hidePasswords"], answer["manage"] = access.ReadOnly, access.HidePasswords, access.Manage || m.manages()
	if !given && m.seesAll() {
		answer["readOnly"] = false
	}
	return answer
}

// seen are the collections of a shared vault a member sees.
func seen(rows []leafen.Row, m member) []leafen.Row {
	kept := []leafen.Row{}
	for _, row := range rows {
		if _, given := m.access(bitwardenID(row.ID)); given || m.seesAll() {
			kept = append(kept, row)
		}
	}
	return kept
}

// accessDetails is a collection with who may use it, for the web vault's
// administration.
func accessDetails(org organization, row leafen.Row, m member, members []member) map[string]any {
	answer := collectionDetails(row, m)
	users := []any{}
	for _, other := range members {
		if access, given := other.access(bitwardenID(row.ID)); given {
			users = append(users, map[string]any{"id": other.id(), "readOnly": access.ReadOnly, "hidePasswords": access.HidePasswords, "manage": access.Manage})
		}
	}
	_, assigned := m.access(bitwardenID(row.ID))
	answer["users"], answer["groups"], answer["assigned"], answer["unmanaged"], answer["object"] = users, groupsOfCollection(org, bitwardenID(row.ID)), assigned, false, "collectionAccessDetails"
	return answer
}

// myCollections are the collections of every shared vault the account is
// confirmed in.
func (s *store) myCollections(places []member) ([]any, error) {
	found := []any{}
	for _, m := range places {
		if m.State != memberConfirmed {
			continue
		}
		rows, err := orgRows(s.collections, m.Organization)
		if err != nil {
			return nil, err
		}
		for _, row := range seen(rows, m) {
			found = append(found, collectionDetails(row, m))
		}
	}
	return found, nil
}

func (c *call) collectionRoute(org organization, m member, rest string) (leafen.EndpointAnswer, error) {
	if m.State != memberConfirmed {
		return problem(403, "You'll see the shared vault's collections once you're confirmed."), nil
	}
	rows, err := orgRows(c.store.collections, org.row.ID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	members, err := c.store.membersOf(org)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	id, tail, _ := strings.Cut(rest, "/")
	switch c.method + " " + id {
	case "GET ":
		listed := []any{}
		for _, row := range seen(rows, m) {
			listed = append(listed, collectionAnswer(row, "collection"))
		}
		return list(listed), nil
	case "GET details":
		listed := []any{}
		for _, row := range rows {
			if _, given := m.access(bitwardenID(row.ID)); given || m.manages() {
				listed = append(listed, accessDetails(org, row, m, members))
			}
		}
		return list(listed), nil
	case "POST ":
		if !m.manages() {
			return problem(403, "Only the shared vault's owners and administrators make collections."), nil
		}
		return c.saveCollection(org, "", members)
	case "DELETE ", "POST delete":
		if !m.manages() {
			return problem(403, "Only the shared vault's owners and administrators delete collections."), nil
		}
		input := struct {
			IDs []string `json:"ids"`
		}{}
		if !c.decode(&input) || len(input.IDs) == 0 || len(input.IDs) > 100 {
			return problem(400, "Choose up to 100 collections."), nil
		}
		gone := []leafen.Row{}
		for _, row := range rows {
			for _, id := range input.IDs {
				if strings.EqualFold(bitwardenID(row.ID), id) {
					gone = append(gone, row)
				}
			}
		}
		return reply(200, nil), c.store.removeCollections(org, gone, members)
	case "POST bulk-access":
		return problem(400, "Give people collections one collection at a time."), nil
	}
	index := -1
	for at, row := range rows {
		if strings.EqualFold(bitwardenID(row.ID), id) {
			index = at
		}
	}
	if index < 0 {
		return problem(404, "That collection isn't in the shared vault."), nil
	}
	row := rows[index]
	access, given := m.access(bitwardenID(row.ID))
	manages := m.manages() || (given && access.Manage)
	switch c.method + " " + tail {
	case "GET ", "GET details":
		if !given && !m.seesAll() {
			return problem(404, "That collection isn't in the shared vault."), nil
		}
		return reply(200, accessDetails(org, row, m, members)), nil
	case "GET users":
		return reply(200, accessDetails(org, row, m, members)["users"]), nil
	case "PUT ", "POST ":
		if !manages {
			return problem(403, "You don't manage that collection."), nil
		}
		return c.saveCollection(org, row.ID, members)
	case "PUT users":
		if !manages {
			return problem(403, "You don't manage that collection."), nil
		}
		users := []collectionAccess{}
		if !c.decode(&users) {
			return problem(400, "The change couldn't be read."), nil
		}
		return reply(200, nil), c.store.giveCollection(org, bitwardenID(row.ID), users, members)
	case "DELETE ", "POST delete":
		if !m.manages() {
			return problem(403, "Only the shared vault's owners and administrators delete collections."), nil
		}
		return reply(200, nil), c.store.removeCollections(org, []leafen.Row{row}, members)
	}
	return problem(404, "Not found."), nil
}

// saveCollection makes a collection, or renames one, with its people.
func (c *call) saveCollection(org organization, rowID string, members []member) (leafen.EndpointAnswer, error) {
	input := struct {
		Name       string             `json:"name"`
		ExternalID *string            `json:"externalId"`
		Users      []collectionAccess `json:"users"`
		Groups     []collectionAccess `json:"groups"`
	}{}
	if !c.decode(&input) || input.Name == "" || tooLong(input.Name) {
		return problem(400, "Give the collection a name."), nil
	}
	values := map[string]any{"Name": input.Name, "Revision": float64(nowMs())}
	if input.ExternalID != nil {
		values["External id"] = *input.ExternalID
	}
	var err error
	if rowID == "" {
		values["Organization"] = org.row.ID
		rowID, err = c.store.collections.Add(values)
	} else {
		err = c.store.collections.Update(rowID, values)
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if input.Users != nil {
		if err := c.store.giveCollection(org, bitwardenID(rowID), input.Users, members); err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	if input.Groups != nil {
		if err := c.store.giveGroupsCollection(org, bitwardenID(rowID), input.Groups); err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	c.store.orgChanged(org.row.ID)
	row, err := c.store.collections.Row(rowID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, collectionAnswer(row, "collection")), nil
}

// giveCollection sets who may use a collection: the members listed, as
// listed; the others lose it.
func (s *store) giveCollection(org organization, collection string, users []collectionAccess, members []member) error {
	for _, other := range members {
		kept := []collectionAccess{}
		for _, access := range other.Collections {
			if !strings.EqualFold(access.ID, collection) {
				kept = append(kept, access)
			}
		}
		for _, user := range users {
			if strings.EqualFold(user.ID, other.id()) {
				kept = append(kept, collectionAccess{ID: collection, ReadOnly: user.ReadOnly, HidePasswords: user.HidePasswords, Manage: user.Manage})
			}
		}
		if len(kept) == len(other.Collections) && sameAccess(kept, other.Collections) {
			continue
		}
		encoded, _ := jsonText(kept)
		if err := s.members.Update(other.row.ID, map[string]any{"Collections": encoded, "Revision": float64(nowMs())}); err != nil {
			return err
		}
	}
	s.orgChanged(org.row.ID)
	return nil
}

func sameAccess(a, b []collectionAccess) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// removeCollections deletes collections: their items stay in the shared
// vault, out of them (its owners and administrators still see them).
func (s *store) removeCollections(org organization, rows []leafen.Row, members []member) error {
	gone := map[string]bool{}
	for _, row := range rows {
		gone[strings.ToLower(bitwardenID(row.ID))] = true
		if err := s.giveCollection(org, bitwardenID(row.ID), nil, members); err != nil {
			return err
		}
	}
	items, err := orgRows(s.ciphers, org.row.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		kept := []string{}
		for _, id := range itemCollections(item) {
			if !gone[strings.ToLower(id)] {
				kept = append(kept, id)
			}
		}
		if len(kept) != len(itemCollections(item)) {
			encoded, _ := jsonText(kept)
			if err := s.ciphers.Update(item.ID, map[string]any{"Collections": encoded, "Revision": float64(nowMs())}); err != nil {
				return err
			}
		}
	}
	budget := 2 * len(rows)
	if _, err := trashRows(s.tenancy, idsOf(rows), &budget); err != nil {
		return err
	}
	return s.orgRemoved(org.row.ID)
}

// itemCollections are the collections an item of a shared vault is in.
func itemCollections(row leafen.Row) []string {
	ids := []string{}
	_ = json.Unmarshal([]byte(row.Text("Collections")), &ids)
	return ids
}

// allMyCollections answers GET /api/collections.
func (c *call) allMyCollections(acct account) (leafen.EndpointAnswer, error) {
	places, err := c.store.memberships(acct)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	found, err := c.store.myCollections(places)
	if errors.Is(err, errMissing) {
		return list([]any{}), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return list(found), nil
}
