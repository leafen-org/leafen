package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedLegacyTemplateBlueprintBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
	Depth    int    `db:"depth"`
}

func planLegacyTemplateUse(
	app core.App,
	authorization pageAuthorization,
	source interactiveAutomationSource,
	executionID string,
) (interactiveAutomationPlan, string, error) {
	blocks := []storedLegacyTemplateBlueprintBlock{}
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[blueprint]](
			[[id]], [[parent_id]], [[position]], [[type]], [[content]], [[depth]]
		) AS (
			SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]], 1
			FROM {{leafen_blocks}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]={:templateId}
			UNION ALL
			-- CROSS JOIN steps from the tree by key (owner, page, parent): SQLite's
			-- own order read the page's every block for each block (2026-11-08).
			SELECT [[child]].[[id]], [[child]].[[parent_id]], [[child]].[[position]],
				[[child]].[[type]], [[child]].[[content]], [[parent]].[[depth]] + 1
			FROM [[blueprint]] AS [[parent]]
			CROSS JOIN {{leafen_blocks}} AS [[child]] ON [[child]].[[parent_id]]=[[parent]].[[id]]
			WHERE [[child]].[[owner]]={:owner} AND [[child]].[[page_id]]={:pageId}
			LIMIT {:limit}
		)
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]], [[depth]]
		FROM [[blueprint]]
		ORDER BY [[depth]], [[parent_id]], [[position]], [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner":      authorization.Workspace.OwnerID,
		"pageId":     authorization.Page.Id,
		"templateId": source.ID,
		"limit":      maxGraphMutationOperations + 1,
	}).All(&blocks)
	if err != nil {
		return interactiveAutomationPlan{}, "", err
	}
	// However deep, as many blocks as one change takes (1,000, as one
	// Notion request); past that, refused saying so.
	if len(blocks) > maxGraphMutationOperations {
		return interactiveAutomationPlan{}, "", graphInputError(errorCodeGraphChangeTooLarge,
			"This template inserts more than 1,000 blocks at once; split it into smaller templates.")
	}
	forbidden := map[string]struct{}{
		graphBlockTypeInlineDatabase: {},
		graphBlockTypeChildPage:      {},
		graphBlockTypeChildDatabase:  {},
	}
	mappings := make(map[string]string, len(blocks))
	for _, block := range blocks {
		if _, unsupported := forbidden[block.Type]; unsupported {
			return interactiveAutomationPlan{}, "", errInteractiveAutomationUnsupported
		}
		if _, supported := graphBlockTypes[block.Type]; !supported {
			return interactiveAutomationPlan{}, "", errInteractiveAutomationUnsupported
		}
		mappings[block.ID] = automationStableID("hlt", executionID, source.ID, block.ID)
	}

	textDocument, err := loadNotionBlockTextDocument(app, authorization.Page.Id)
	if err != nil {
		return interactiveAutomationPlan{}, "", err
	}
	plan := interactiveAutomationPlan{
		Rows: map[string]*plannedInteractiveAutomationRow{},
		Effects: interactiveAutomationEffects{
			Blocks: []interactiveAutomationEffectBlock{},
			Pages:  []interactiveAutomationEffectPage{},
		},
		Outcomes: []interactiveAutomationOutcome{},
	}
	lastSibling := map[string]string{}
	digestRows := make([]any, 0, len(blocks))
	for index, block := range blocks {
		content := map[string]any{}
		if json.Unmarshal([]byte(block.Content), &content) != nil {
			return interactiveAutomationPlan{}, "", errInteractiveAutomationInvalid
		}
		rewritePageDuplicationReferences(
			content, false, map[string]string{}, mappings,
			map[string]string{}, map[string]string{},
		)
		canonical, err := json.Marshal(content)
		if err != nil {
			return interactiveAutomationPlan{}, "", errInteractiveAutomationInvalid
		}
		parentID := source.ParentID
		siblingKey := source.ID
		if block.ParentID != source.ID {
			var found bool
			parentID, found = mappings[block.ParentID]
			if !found {
				return interactiveAutomationPlan{}, "", errInteractiveAutomationInvalid
			}
			siblingKey = block.ParentID
		}
		afterID := lastSibling[siblingKey]
		if block.ParentID == source.ID && afterID == "" {
			afterID = source.ID
		}
		targetID := mappings[block.ID]
		text := textDocument.GetText(ygoBlockTextRootPrefix + block.ID)
		textDelta, err := json.Marshal(text.ToDelta())
		if err != nil {
			return interactiveAutomationPlan{}, "", errInteractiveAutomationInvalid
		}
		plan.Blocks = append(plan.Blocks, plannedInteractiveAutomationBlock{
			ID: targetID, ParentID: parentID, AfterID: afterID,
			Position: automationPositionAfter(source.Position, executionID, "legacy-template", index),
			Type:     block.Type, Content: string(canonical), Text: text.ToString(),
			SourceTextID: block.ID,
		})
		plan.Effects.Blocks = append(plan.Effects.Blocks, interactiveAutomationEffectBlock{ID: targetID})
		lastSibling[siblingKey] = targetID
		digestRows = append(digestRows, []any{
			block.ID, block.ParentID, block.Position, block.Type, json.RawMessage(block.Content),
			json.RawMessage(textDelta),
		})
	}
	encoded, err := json.Marshal(digestRows)
	if err != nil {
		return interactiveAutomationPlan{}, "", errInteractiveAutomationInvalid
	}
	digest := sha256.Sum256(encoded)
	return plan, hex.EncodeToString(digest[:]), nil
}
