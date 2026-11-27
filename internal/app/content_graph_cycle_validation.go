package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func validateGraphBlockCycles(app core.App, owner, pageID string, operations []graphOperation) error {
	parents := map[string]string{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock {
			continue
		}
		parentID := ""
		if operation.Block.ParentID != nil {
			parentID = *operation.Block.ParentID
		}
		parents[operation.Block.ID] = parentID
	}

	state := map[string]uint8{}
	var visit func(string) error
	visit = func(blockID string) error {
		switch state[blockID] {
		case 1:
			return graphInputError(errorCodeGraphReferenceInvalid, "Block parent relationships can't form a cycle.")
		case 2:
			return nil
		}
		state[blockID] = 1
		if parentID := parents[blockID]; parentID != "" {
			if _, changed := parents[parentID]; changed {
				if err := visit(parentID); err != nil {
					return err
				}
			}
		}
		state[blockID] = 2
		return nil
	}
	ordered := make([]string, 0, len(parents))
	for blockID := range parents {
		ordered = append(ordered, blockID)
	}
	sort.Strings(ordered)
	for _, blockID := range ordered {
		if err := visit(blockID); err != nil {
			return err
		}
	}
	if len(ordered) == 0 {
		return nil
	}

	params := dbx.Params{"owner": owner, "pageId": pageID}
	values := make([]string, 0, len(ordered))
	for index, childID := range ordered {
		parentID := parents[childID]
		if parentID == "" {
			continue
		}
		childKey := fmt.Sprintf("cycleChild%d", index)
		parentKey := fmt.Sprintf("cycleParent%d", index)
		params[childKey] = childID
		params[parentKey] = parentID
		values = append(values, "({:"+childKey+"}, {:"+parentKey+"})")
	}
	if len(values) == 0 {
		return nil
	}
	query := fmt.Sprintf(`
		WITH RECURSIVE
		[[proposed]] ([[child_id]], [[parent_id]]) AS (VALUES %s),
		[[ancestors]] ([[root_id]], [[id]]) AS (
			SELECT [[proposed]].[[child_id]], [[proposed]].[[parent_id]]
			FROM [[proposed]]
			WHERE [[proposed]].[[parent_id]] != ''
			UNION
			SELECT
				[[ancestors]].[[root_id]],
				CASE
					WHEN [[override]].[[child_id]] IS NOT NULL THEN [[override]].[[parent_id]]
					ELSE [[blocks]].[[parent_id]]
				END
			FROM [[ancestors]]
			LEFT JOIN [[proposed]] AS [[override]]
				ON [[override]].[[child_id]] = [[ancestors]].[[id]]
			LEFT JOIN {{leafen_blocks}} AS [[blocks]]
				ON [[blocks]].[[id]] = [[ancestors]].[[id]]
				AND [[blocks]].[[owner]] = {:owner}
				AND [[blocks]].[[page_id]] = {:pageId}
			WHERE CASE
				WHEN [[override]].[[child_id]] IS NOT NULL THEN [[override]].[[parent_id]]
				ELSE [[blocks]].[[parent_id]]
			END != ''
		)
		SELECT COUNT(*)
		FROM [[ancestors]]
		WHERE [[ancestors]].[[root_id]] = [[ancestors]].[[id]]
	`, strings.Join(values, ", "))
	var cycles int
	if err := app.DB().NewQuery(query).Bind(params).Row(&cycles); err != nil {
		return err
	}
	if cycles > 0 {
		return graphInputError(errorCodeGraphReferenceInvalid, "Block parent relationships can't form a cycle.")
	}
	return nil
}
