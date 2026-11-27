package main

import (
	"encoding/json"
	"errors"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Items in shared vaults: encrypted with the vault's key, in collections,
// and seen by the members those collections are given to. Each member keeps
// their own folder and favorite for a shared item.

// rights are what an account may do with an item.
type rights struct{ edit, viewPassword, manage bool }

var allRights = rights{edit: true, viewPassword: true, manage: true}

// view is what an account sees of shared vaults: its confirmed places, by
// the vault's row.
type view map[string]member

// viewOf reads an account's places, and those it is confirmed in.
func (s *store) viewOf(acct account) (view, []member, error) {
	places, err := s.memberships(acct)
	if err != nil {
		return nil, nil, err
	}
	confirmed := view{}
	for at, m := range places {
		org, err := s.organizationByID(bitwardenID(m.Organization))
		if errors.Is(err, errMissing) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		places[at] = m.in(org)
		if m.State == memberConfirmed {
			confirmed[m.Organization] = places[at]
		}
	}
	return confirmed, places, nil
}

// rightsOn says whether the account sees a shared item, and what it may
// do with it.
func (v view) rightsOn(row leafen.Row) (rights, bool) {
	m, ok := v[row.Text("Organization")]
	if !ok {
		return rights{}, false
	}
	if m.seesAll() {
		return allRights, true
	}
	found, seen := rights{}, false
	for _, id := range itemCollections(row) {
		if access, given := m.access(id); given {
			seen = true
			found.edit = found.edit || !access.ReadOnly
			found.viewPassword = found.viewPassword || !access.HidePasswords
			found.manage = found.manage || access.Manage
		}
	}
	return found, seen
}

// place is a member's own folder and favorite for a shared item.
type place struct {
	Folder   string `json:"folder,omitempty"`
	Favorite bool   `json:"favorite,omitempty"`
}

func placesOf(row leafen.Row) map[string]place {
	places := map[string]place{}
	_ = json.Unmarshal([]byte(row.Text("Places")), &places)
	return places
}

// placeValues are the changes that set the account's folder and favorite
// for an item: on the row for a personal one, among the members' places for
// a shared one.
func placeValues(row leafen.Row, acctRow, folder string, favorite bool) map[string]any {
	if row.Text("Organization") == "" {
		return map[string]any{"Folder": folder, "Favorite": favorite}
	}
	places := placesOf(row)
	if folder == "" && !favorite {
		delete(places, acctRow)
	} else {
		places[acctRow] = place{Folder: folder, Favorite: favorite}
	}
	encoded, _ := jsonText(places)
	return map[string]any{"Places": encoded}
}

// findItem reads an item the account sees: its own, or a shared one.
func (c *call) findItem(acct account, v view, id string) (leafen.Row, rights, error) {
	rowID, ok := rowOf(id)
	if !ok {
		return leafen.Row{}, rights{}, errMissing
	}
	row, err := c.store.ciphers.Row(rowID)
	if errors.Is(err, leafen.ErrNoRow) {
		return row, rights{}, errMissing
	}
	if err != nil {
		return row, rights{}, err
	}
	if row.Text("Organization") != "" {
		found, seen := v.rightsOn(row)
		if !seen {
			return row, found, errMissing
		}
		return row, found, nil
	}
	for _, owner := range row.IDs("Account") {
		if owner == acct.row.ID {
			return row, allRights, nil
		}
	}
	return row, rights{}, errMissing
}

// visibleItems are the account's items and the shared ones it sees.
func (s *store) visibleItems(acct account, v view) ([]any, error) {
	rows, err := owned(s.ciphers, acct.row.ID)
	if err != nil {
		return nil, err
	}
	items := []any{}
	for _, row := range rows {
		items = append(items, itemOf(row, allRights, acct.row.ID))
	}
	for org := range v {
		shared, err := orgRows(s.ciphers, org)
		if err != nil {
			return nil, err
		}
		for _, row := range shared {
			if found, seen := v.rightsOn(row); seen {
				items = append(items, itemOf(row, found, acct.row.ID))
			}
		}
	}
	return items, nil
}

// itemChanged notes a change to an item where its devices look: the
// account's, or its shared vault's.
func (c *call) itemChanged(acct account, row leafen.Row) {
	if org := row.Text("Organization"); org != "" {
		c.store.orgChanged(org)
		return
	}
	c.store.changed(acct)
}

// mayPut checks that an account may put an item in a shared vault's
// collections: it edits at least one of them, and every one is the vault's.
func (c *call) mayPut(v view, orgID string, collectionIDs []string) (member, []string, string, error) {
	org, err := c.store.organizationByID(orgID)
	if errors.Is(err, errMissing) {
		return member{}, nil, "That shared vault isn't here.", nil
	}
	if err != nil {
		return member{}, nil, "", err
	}
	m, ok := v[org.row.ID]
	if !ok {
		return member{}, nil, "You aren't confirmed in that shared vault yet.", nil
	}
	rows, err := orgRows(c.store.collections, org.row.ID)
	if err != nil {
		return m, nil, "", err
	}
	kept, editable := []string{}, false
	for _, id := range collectionIDs {
		found := ""
		for _, row := range rows {
			if strings.EqualFold(bitwardenID(row.ID), id) {
				found = bitwardenID(row.ID)
			}
		}
		if found == "" {
			return m, nil, "Choose the shared vault's collections.", nil
		}
		if access, given := m.access(found); m.manages() || (given && !access.ReadOnly) {
			editable = true
		}
		kept = append(kept, found)
	}
	if !editable && !(m.manages() && len(kept) == 0) {
		return m, nil, "Choose a collection you may change.", nil
	}
	return m, kept, "", nil
}

// createShared keeps a new item in a shared vault.
func (c *call) createShared(acct account, v view, input itemInput, collectionIDs []string) (leafen.EndpointAnswer, error) {
	m, kept, refusal, err := c.mayPut(v, *input.OrganizationID, collectionIDs)
	if refusal != "" || err != nil {
		return problem(400, refusal), err
	}
	now := float64(nowMs())
	data, _ := json.Marshal(input.parts)
	collections, _ := jsonText(kept)
	values := map[string]any{"Kind": itemKinds[input.Type], "Organization": m.Organization, "Collections": collections, "Type": float64(input.Type),
		"Data": string(data), "Created": now, "Revision": now, "Deleted": float64(0), "Places": "{}"}
	rowID, err := c.store.ciphers.Add(values)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if input.folder() != "" || input.Favorite {
		row, err := c.store.ciphers.Row(rowID)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if err := c.store.ciphers.Update(rowID, placeValues(row, acct.row.ID, input.folder(), input.Favorite)); err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	return c.itemAnswer(acct, v, rowID)
}

// share moves the account's own item into a shared vault, as its device
// encrypted it again with the vault's key.
func (c *call) share(acct account, v view, row leafen.Row, input itemInput, collectionIDs []string) (leafen.EndpointAnswer, error) {
	if row.Text("Organization") != "" {
		return problem(400, "The item is in a shared vault already."), nil
	}
	if input.OrganizationID == nil || *input.OrganizationID == "" {
		return problem(400, "Choose a shared vault."), nil
	}
	m, kept, refusal, err := c.mayPut(v, *input.OrganizationID, collectionIDs)
	if refusal != "" || err != nil {
		return problem(400, refusal), err
	}
	if len(kept) == 0 {
		return problem(400, "Choose a collection you may change."), nil
	}
	data, _ := json.Marshal(input.parts)
	collections, _ := jsonText(kept)
	places := map[string]place{}
	if folder, favorite := row.Text("Folder"), row.Checkbox("Favorite"); folder != "" || favorite {
		places[acct.row.ID] = place{Folder: folder, Favorite: favorite}
	}
	placed, _ := jsonText(places)
	// A row's account is a relation, which only a change of the row itself
	// (not of its values) clears.
	if err := writeRows(c.store.ciphers, []leafen.Row{row}, map[string]any{"Account": []string{}, "Organization": m.Organization, "Collections": collections,
		"Kind": itemKinds[input.Type], "Type": float64(input.Type), "Data": string(data), "Folder": "", "Favorite": false, "Places": placed,
		"Revision": float64(nowMs())}); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	// The item left the account's own vault.
	c.store.changed(acct)
	return c.itemAnswer(acct, v, row.ID)
}

// shareMany shares several items at once.
func (c *call) shareMany(acct account, v view) (leafen.EndpointAnswer, error) {
	input := struct {
		Ciphers       []json.RawMessage `json:"ciphers"`
		CollectionIDs []string          `json:"collectionIds"`
	}{}
	if !c.decode(&input) || len(input.Ciphers) == 0 || len(input.Ciphers) > 50 {
		return problem(400, "Share up to 50 items at a time."), nil
	}
	shared := []any{}
	for _, raw := range input.Ciphers {
		item, _, refusal := readItem(raw)
		id := struct {
			ID string `json:"id"`
		}{}
		_ = json.Unmarshal(raw, &id)
		if refusal != "" {
			return problem(400, refusal), nil
		}
		row, _, err := c.findItem(acct, v, id.ID)
		if errors.Is(err, errMissing) {
			return problem(404, "That item isn't in your vault."), nil
		}
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		answer, err := c.share(acct, v, row, item, input.CollectionIDs)
		if err != nil || answer.Status != 200 {
			return answer, err
		}
		fresh, err := c.store.ciphers.Row(row.ID)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		found, _ := v.rightsOn(fresh)
		shared = append(shared, itemOf(fresh, found, acct.row.ID))
	}
	return list(shared), nil
}

// setCollections changes which of its vault's collections a shared item is
// in; collections the account can't change stay as they are.
func (c *call) setCollections(acct account, v view, row leafen.Row, found rights) (leafen.EndpointAnswer, error) {
	input := struct {
		CollectionIDs []string `json:"collectionIds"`
	}{}
	org := row.Text("Organization")
	if org == "" || !c.decode(&input) {
		return problem(400, "Choose collections for an item in a shared vault."), nil
	}
	if !found.edit {
		return problem(403, "You may only read that item."), nil
	}
	m := v[org]
	_, kept, refusal, err := c.mayPut(v, bitwardenID(org), input.CollectionIDs)
	if refusal != "" && !(len(input.CollectionIDs) == 0 && m.manages()) {
		return problem(400, refusal), err
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	for _, id := range itemCollections(row) {
		if access, given := m.access(id); !m.manages() && (!given || access.ReadOnly) {
			kept = append(kept, id)
		}
	}
	encoded, _ := jsonText(kept)
	if err := c.store.ciphers.Update(row.ID, map[string]any{"Collections": encoded, "Revision": float64(nowMs())}); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	c.store.orgChanged(org)
	fresh, err := c.store.ciphers.Row(row.ID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	now, seen := v.rightsOn(fresh)
	if strings.HasSuffix(c.path, "_v2") {
		var item any
		if seen {
			item = itemOf(fresh, now, acct.row.ID)
		}
		return reply(200, map[string]any{"cipher": item, "unavailable": !seen, "object": "optionalCipherDetails"}), nil
	}
	return reply(200, nil), nil
}

// orgItems lists a shared vault's items for the web vault: every one for
// its owners and administrators, else those the account sees.
func (c *call) orgItems(acct account, v view) (leafen.EndpointAnswer, error) {
	org, err := c.store.organizationByID(c.query.Get("organizationId"))
	if errors.Is(err, errMissing) {
		return list([]any{}), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if _, ok := v[org.row.ID]; !ok {
		return list([]any{}), nil
	}
	rows, err := orgRows(c.store.ciphers, org.row.ID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	items := []any{}
	for _, row := range rows {
		if found, seen := v.rightsOn(row); seen {
			item := itemOf(row, found, acct.row.ID)
			item["object"] = "cipherMiniDetails"
			items = append(items, item)
		}
	}
	return list(items), nil
}
