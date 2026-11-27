package main

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Items as the apps send and read them, the person's own or in a shared
// vault they see.

// itemInput is an item as an app sends it.
type itemInput struct {
	Type           int64   `json:"type"`
	FolderID       *string `json:"folderId"`
	Favorite       bool    `json:"favorite"`
	OrganizationID *string `json:"organizationId"`
	LastKnown      *string `json:"lastKnownRevisionDate"`
	parts          map[string]json.RawMessage
}

// readItem reads an item from a body: the item alone, or {"cipher": …,
// "collectionIds": […]} as apps send a shared one.
func readItem(body []byte) (itemInput, []string, string) {
	raw := map[string]json.RawMessage{}
	if json.Unmarshal(body, &raw) != nil {
		return itemInput{}, nil, "The item couldn't be read."
	}
	collections := []string{}
	if inner, ok := raw["cipher"]; ok {
		_ = json.Unmarshal(raw["collectionIds"], &collections)
		raw = map[string]json.RawMessage{}
		if json.Unmarshal(inner, &raw) != nil {
			return itemInput{}, nil, "The item couldn't be read."
		}
	}
	// Older apps write their keys capitalized.
	normal := map[string]json.RawMessage{}
	for key, value := range raw {
		if key != "" {
			normal[strings.ToLower(key[:1])+key[1:]] = value
		}
	}
	encoded, _ := json.Marshal(normal)
	input := itemInput{parts: map[string]json.RawMessage{}}
	if json.Unmarshal(encoded, &input) != nil {
		return input, nil, "The item couldn't be read."
	}
	if input.OrganizationID != nil && *input.OrganizationID == "" {
		input.OrganizationID = nil
	}
	if _, ok := itemKinds[input.Type]; !ok {
		return input, nil, "That kind of item isn't kept yet."
	}
	for _, part := range itemParts {
		if value, ok := normal[part]; ok && string(value) != "null" {
			input.parts[part] = value
		}
	}
	if _, ok := input.parts["name"]; !ok {
		return input, nil, "An item needs a name."
	}
	if data, _ := json.Marshal(input.parts); len(data) > itemMaxBytes {
		return input, nil, "The item is too large."
	}
	return input, collections, ""
}

func (input itemInput) folder() string {
	if input.FolderID == nil {
		return ""
	}
	return *input.FolderID
}

// itemOf is a kept item as an account's Bitwarden apps read it.
func itemOf(row leafen.Row, may rights, acctRow string) map[string]any {
	item := map[string]any{}
	_ = json.Unmarshal([]byte(row.Text("Data")), &item)
	for _, part := range itemParts {
		if _, ok := item[part]; !ok {
			item[part] = nil
		}
	}
	if item["reprompt"] == nil {
		item["reprompt"] = 0
	}
	folderID, favorite := row.Text("Folder"), row.Checkbox("Favorite")
	var organization any
	collections := []any{}
	if org := row.Text("Organization"); org != "" {
		organization = bitwardenID(org)
		mine := placesOf(row)[acctRow]
		folderID, favorite = mine.Folder, mine.Favorite
		for _, id := range itemCollections(row) {
			collections = append(collections, id)
		}
	}
	var folder any
	if folderID != "" {
		folder = folderID
	}
	var deleted any
	if at := number(row, "Deleted"); at > 0 {
		deleted = stamp(at)
	}
	item["id"], item["type"], item["folderId"], item["favorite"] = bitwardenID(row.ID), number(row, "Type"), folder, favorite
	item["organizationId"], item["organizationUseTotp"], item["edit"], item["viewPassword"] = organization, true, may.edit, may.viewPassword
	item["permissions"] = map[string]any{"delete": may.edit, "restore": may.edit}
	item["collectionIds"], item["attachments"] = collections, nil
	item["creationDate"], item["revisionDate"], item["deletedDate"] = stamp(number(row, "Created")), stamp(number(row, "Revision")), deleted
	item["object"] = "cipherDetails"
	return item
}

func (c *call) cipherRoute(acct account, id, rest string) (leafen.EndpointAnswer, error) {
	ciphers := c.store.ciphers
	v, _, err := c.store.viewOf(acct)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	switch {
	case c.method == "GET" && id == "":
		items, err := c.store.visibleItems(acct, v)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return list(items), nil
	case c.method == "GET" && id == "organization-details":
		return c.orgItems(acct, v)
	case c.method == "POST" && (id == "" || id == "create" || id == "admin") && rest == "":
		input, collections, refusal := readItem(c.body)
		if refusal != "" {
			return problem(400, refusal), nil
		}
		if input.OrganizationID != nil {
			return c.createShared(acct, v, input, collections)
		}
		now := nowMs()
		data, _ := json.Marshal(input.parts)
		rowID, err := ciphers.Add(map[string]any{"Kind": itemKinds[input.Type], "Account": []string{acct.row.ID}, "Type": float64(input.Type),
			"Folder": input.folder(), "Favorite": input.Favorite, "Data": string(data), "Created": float64(now), "Revision": float64(now), "Deleted": float64(0)})
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return c.itemAnswer(acct, v, rowID)
	case id == "share" && (c.method == "PUT" || c.method == "POST"):
		return c.shareMany(acct, v)
	case id == "delete" || id == "restore" || id == "move" || id == "delete-admin" || id == "restore-admin" || (id == "admin" && c.method == "DELETE") ||
		(id == "" && c.method == "DELETE"):
		return c.manyItems(acct, v, strings.TrimSuffix(strings.TrimPrefix(id, "admin"), "-admin"))
	}
	row, may, err := c.findItem(acct, v, id)
	if errors.Is(err, errMissing) {
		return problem(404, "That item isn't in your vault."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	// The web vault's administration calls the same changes, by its owners
	// and administrators, with "admin" after them.
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "admin"), "-")
	writes := c.method != "GET"
	if writes && !may.edit && rest != "partial" {
		return problem(403, "You may only read that item."), nil
	}
	switch c.method + " " + rest {
	case "GET ", "GET details":
		return reply(200, itemOf(row, may, acct.row.ID)), nil
	case "PUT ", "POST ":
		input, _, refusal := readItem(c.body)
		if refusal != "" {
			return problem(400, refusal), nil
		}
		if org := row.Text("Organization"); (org == "") != (input.OrganizationID == nil) || (org != "" && !strings.EqualFold(bitwardenID(org), *input.OrganizationID)) {
			return problem(400, "Move an item to a shared vault by sharing it."), nil
		}
		if input.LastKnown != nil {
			if known, err := time.Parse(time.RFC3339Nano, *input.LastKnown); err == nil && number(row, "Revision")-known.UnixMilli() > 1000 {
				return problem(400, "The client copy of this cipher is out of date. Resync the client and try again."), nil
			}
		}
		data, _ := json.Marshal(input.parts)
		values := placeValues(row, acct.row.ID, input.folder(), input.Favorite)
		values["Kind"], values["Type"], values["Data"], values["Revision"] = itemKinds[input.Type], float64(input.Type), string(data), float64(nowMs())
		if err := ciphers.Update(row.ID, values); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return c.itemAnswer(acct, v, row.ID)
	case "PUT partial", "POST partial":
		input := struct {
			FolderID *string `json:"folderId"`
			Favorite bool    `json:"favorite"`
		}{}
		if !c.decode(&input) {
			return problem(400, "The change couldn't be read."), nil
		}
		folder := ""
		if input.FolderID != nil {
			folder = *input.FolderID
		}
		values := placeValues(row, acct.row.ID, folder, input.Favorite)
		values["Revision"] = float64(nowMs())
		if err := ciphers.Update(row.ID, values); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return c.itemAnswer(acct, v, row.ID)
	case "PUT share", "POST share":
		input, collections, refusal := readItem(c.body)
		if refusal != "" {
			return problem(400, refusal), nil
		}
		return c.share(acct, v, row, input, collections)
	case "PUT collections", "POST collections", "PUT collections_v2", "POST collections_v2":
		return c.setCollections(acct, v, row, may)
	case "PUT delete":
		return c.trashItems(acct, []leafen.Row{row}, true)
	case "PUT restore":
		if _, err := c.trashItems(acct, []leafen.Row{row}, false); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return c.itemAnswer(acct, v, row.ID)
	case "DELETE ", "POST delete":
		return c.removeItems(acct, []leafen.Row{row})
	}
	return problem(404, "Not found."), nil
}

// itemAnswer answers with an item as kept now, and notes the change.
func (c *call) itemAnswer(acct account, v view, rowID string) (leafen.EndpointAnswer, error) {
	row, err := c.store.ciphers.Row(rowID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	c.itemChanged(acct, row)
	may := allRights
	if row.Text("Organization") != "" {
		may, _ = v.rightsOn(row)
	}
	return reply(200, itemOf(row, may, acct.row.ID)), nil
}

// manyItems moves, trashes, restores or removes several items at once:
// those the account may change, of the ones named.
func (c *call) manyItems(acct account, v view, what string) (leafen.EndpointAnswer, error) {
	input := struct {
		IDs      []string `json:"ids"`
		FolderID *string  `json:"folderId"`
	}{}
	if !c.decode(&input) || len(input.IDs) == 0 || len(input.IDs) > 500 {
		return problem(400, "Choose up to 500 items."), nil
	}
	rows := []leafen.Row{}
	for _, id := range input.IDs {
		row, may, err := c.findItem(acct, v, id)
		if errors.Is(err, errMissing) || (err == nil && !may.edit && what != "move") {
			continue
		}
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		rows = append(rows, row)
	}
	switch {
	case what == "move" && (c.method == "PUT" || c.method == "POST"):
		folder := ""
		if input.FolderID != nil {
			folder = *input.FolderID
		}
		own, shared := split(rows)
		if err := writeRows(c.store.ciphers, own, map[string]any{"Folder": folder, "Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		for _, row := range shared {
			if err := c.store.ciphers.Update(row.ID, placeValues(row, acct.row.ID, folder, placesOf(row)[acct.row.ID].Favorite)); err != nil {
				return leafen.EndpointAnswer{}, err
			}
		}
		c.manyChanged(acct, rows)
		return reply(200, nil), nil
	case what == "delete" && c.method == "PUT":
		return c.trashItems(acct, rows, true)
	case what == "restore" && c.method == "PUT":
		if _, err := c.trashItems(acct, rows, false); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		restored := []any{}
		for _, row := range rows {
			fresh, err := c.store.ciphers.Row(row.ID)
			if err != nil {
				return leafen.EndpointAnswer{}, err
			}
			may := allRights
			if fresh.Text("Organization") != "" {
				may, _ = v.rightsOn(fresh)
			}
			restored = append(restored, itemOf(fresh, may, acct.row.ID))
		}
		return list(restored), nil
	case (what == "delete" && c.method == "POST") || (what == "" && c.method == "DELETE"):
		return c.removeItems(acct, rows)
	}
	return problem(404, "Not found."), nil
}

// split parts the account's own items from shared ones.
func split(rows []leafen.Row) (own, shared []leafen.Row) {
	for _, row := range rows {
		if row.Text("Organization") == "" {
			own = append(own, row)
		} else {
			shared = append(shared, row)
		}
	}
	return own, shared
}

// manyChanged notes a change to items where their devices look.
func (c *call) manyChanged(acct account, rows []leafen.Row) {
	noted := map[string]bool{}
	for _, row := range rows {
		org := row.Text("Organization")
		if !noted[org] {
			noted[org] = true
			c.itemChanged(acct, row)
		}
	}
}

// trashItems puts items in the vault's trash, or takes them out.
func (c *call) trashItems(acct account, rows []leafen.Row, trash bool) (leafen.EndpointAnswer, error) {
	now := nowMs()
	deleted := float64(0)
	if trash {
		deleted = float64(now)
	}
	if err := writeRows(c.store.ciphers, rows, map[string]any{"Deleted": deleted, "Revision": float64(now)}); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	c.manyChanged(acct, rows)
	return reply(200, nil), nil
}

// removeAtOnce is how many items one request removes for good, within a
// run's host calls.
const removeAtOnce = 80

// removeItems removes items for good.
func (c *call) removeItems(acct account, rows []leafen.Row) (leafen.EndpointAnswer, error) {
	if len(rows) > removeAtOnce {
		return problem(400, "Delete up to 80 items at a time."), nil
	}
	budget := 2 * len(rows)
	if _, err := trashRows(c.store.tenancy, idsOf(rows), &budget); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	own, shared := split(rows)
	orgs := map[string]bool{}
	for _, row := range shared {
		orgs[row.Text("Organization")] = true
	}
	for org := range orgs {
		if err := c.store.orgRemoved(org); err != nil {
			return leafen.EndpointAnswer{}, err
		}
	}
	if len(own) == 0 && len(shared) > 0 {
		return reply(200, nil), nil
	}
	return reply(200, nil), c.store.removed(acct)
}
