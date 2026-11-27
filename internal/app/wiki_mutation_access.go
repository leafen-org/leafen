package app

import (
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func loadWikiMutationPageRecords(
	app core.App,
	pageIDs []string,
) (map[string]*core.Record, error) {
	result := make(map[string]*core.Record, len(pageIDs))
	if len(pageIDs) == 0 {
		return result, nil
	}
	if len(pageIDs) > maxWikiMutationItemsPerTurn {
		return nil, errWikiConflict
	}
	records, err := app.FindRecordsByIds(pagesCollection, pageIDs)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		result[record.Id] = record
	}
	if len(result) != len(pageIDs) {
		return nil, errWikiConflict
	}
	return result, nil
}

func insertWikiMutationItems(
	app core.App,
	job storedWikiMutation,
	parent storedWikiMutationItem,
	children []wikiChild,
	now int64,
) error {
	if len(children) == 0 {
		return nil
	}
	if len(children) > maxWikiMutationItemsPerTurn {
		return errWikiConflict
	}
	params := dbx.Params{"jobId": job.ID, "depth": parent.Depth + 1, "now": now}
	values := make([]string, 0, len(children))
	for index, child := range children {
		pageKey := "page" + strconv.Itoa(index)
		parentKey := "parent" + strconv.Itoa(index)
		params[pageKey] = child.ID
		params[parentKey] = child.ParentID
		values = append(values,
			"({:jobId},{:"+pageKey+"},{:"+parentKey+"},{:depth},{:now},{:now})")
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_wiki_mutation_items}}
			([[job_id]],[[page_id]],[[parent_id]],[[depth]],[[created_at]],[[updated_at]])
		VALUES ` + joinSQL(values) + `
		ON CONFLICT ([[job_id]],[[page_id]]) DO NOTHING
	`).Bind(params).Execute()
	return err
}

func markWikiMutationItemsApplied(
	app core.App,
	jobID string,
	pageIDs []string,
	now int64,
) error {
	if len(pageIDs) == 0 {
		return nil
	}
	if len(pageIDs) > maxWikiMutationItemsPerTurn {
		return errWikiConflict
	}
	params := dbx.Params{"jobId": jobID, "now": now}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := "page" + strconv.Itoa(index)
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutation_items}} SET [[applied]]=1,[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[page_id]] IN (` + joinSQL(placeholders) + `)
			AND [[applied]]=0
	`).Bind(params).Execute()
	return err
}
