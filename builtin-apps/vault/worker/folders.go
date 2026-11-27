package main

import (
	"errors"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Folders: a name the person's apps encrypt, and the items that name it.

func folderOf(row leafen.Row) map[string]any {
	return map[string]any{"id": bitwardenID(row.ID), "name": row.Text("Name"), "revisionDate": stamp(number(row, "Revision")), "object": "folder"}
}

func folderList(rows []leafen.Row) []any {
	folders := []any{}
	for _, row := range rows {
		folders = append(folders, folderOf(row))
	}
	return folders
}

func (c *call) folderRoute(acct account, id, rest string) (leafen.EndpointAnswer, error) {
	folders := c.store.folders
	input := struct {
		Name string `json:"name"`
	}{}
	switch {
	case c.method == "GET" && id == "":
		rows, err := owned(folders, acct.row.ID)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return list(folderList(rows)), nil
	case c.method == "POST" && id == "":
		if !c.decode(&input) || input.Name == "" || tooLong(input.Name) {
			return problem(400, "Give the folder a name."), nil
		}
		rowID, err := folders.Add(map[string]any{"Name": input.Name, "Account": []string{acct.row.ID}, "Revision": float64(nowMs())})
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return c.folderAnswer(acct, rowID)
	}
	row, err := ownedRow(folders, acct.row.ID, id)
	if errors.Is(err, errMissing) {
		return problem(404, "That folder isn't in your vault."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	switch c.method + " " + rest {
	case "GET ":
		return reply(200, folderOf(row)), nil
	case "PUT ", "POST ":
		if !c.decode(&input) || input.Name == "" || tooLong(input.Name) {
			return problem(400, "Give the folder a name."), nil
		}
		if err := folders.Update(row.ID, map[string]any{"Name": input.Name, "Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return c.folderAnswer(acct, row.ID)
	case "DELETE ", "POST delete":
		// Its items stay, out of any folder.
		items, err := owned(c.store.ciphers, acct.row.ID)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		inFolder := []leafen.Row{}
		for _, item := range items {
			if strings.EqualFold(item.Text("Folder"), id) {
				inFolder = append(inFolder, item)
			}
		}
		if err := writeRows(c.store.ciphers, inFolder, map[string]any{"Folder": "", "Revision": float64(nowMs())}); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		// And the shared items the person put in it.
		v, _, err := c.store.viewOf(acct)
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		for org := range v {
			shared, err := orgRows(c.store.ciphers, org)
			if err != nil {
				return leafen.EndpointAnswer{}, err
			}
			for _, item := range shared {
				if mine := placesOf(item)[acct.row.ID]; strings.EqualFold(mine.Folder, id) {
					if err := c.store.ciphers.Update(item.ID, placeValues(item, acct.row.ID, "", mine.Favorite)); err != nil {
						return leafen.EndpointAnswer{}, err
					}
				}
			}
		}
		if err := trashRow(c.store.tenancy, row.ID); err != nil {
			return leafen.EndpointAnswer{}, err
		}
		return reply(200, nil), c.store.removed(acct)
	}
	return problem(404, "Not found."), nil
}

func (c *call) folderAnswer(acct account, rowID string) (leafen.EndpointAnswer, error) {
	c.store.changed(acct)
	row, err := c.store.folders.Row(rowID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, folderOf(row)), nil
}
