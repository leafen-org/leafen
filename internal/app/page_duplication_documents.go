package app

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func copyPageDuplicationDocumentsWindow(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
	blocks []pageDuplicationSourceBlock,
) error {
	documents, err := loadPageDuplicationDocuments(app, job.ID, item)
	if err != nil {
		return err
	}
	structuralDocument, sourceTextDocument, targetTextDocument, hasTextAuthority :=
		documents.structural, documents.source, documents.text, documents.hasText
	// Each block goes after its nearest already-copied predecessor, so a window
	// is inserted in source order rather than the ID order it was read in.
	blocks = append([]pageDuplicationSourceBlock(nil), blocks...)
	sort.SliceStable(blocks, func(left, right int) bool {
		if blocks[left].ParentID != blocks[right].ParentID {
			return blocks[left].ParentID < blocks[right].ParentID
		}
		if blocks[left].Position != blocks[right].Position {
			return blocks[left].Position < blocks[right].Position
		}
		return blocks[left].ID < blocks[right].ID
	})
	textDeltas := make([][]crdt.Delta, len(blocks))
	if hasTextAuthority {
		for index, block := range blocks {
			textDeltas[index] = sourceTextDocument.GetText(
				ygoBlockTextRootPrefix + block.ID,
			).ToDelta()
		}
		textDeltas, err = remapPageDuplicationTextDeltas(app, job.ID, textDeltas)
		if err != nil {
			return err
		}
	}
	// Each parent's order is read once and kept as blocks go in
	// (structuralOrderView): read again for every block, a long page's copy
	// took the square of its length.
	orders := newStructuralOrderView(structuralDocument)
	afterIDs, err := pageDuplicationStructuralAfterIDs(app, job.ID, job.sourceOwner(), item.SourcePageID, blocks)
	if err != nil {
		return err
	}
	for index, block := range blocks {
		if err := orders.insert(block.TargetID, block.TargetParentID, afterIDs[block.ID]); err != nil {
			return err
		}
		content := map[string]any{}
		if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
			return err
		}
		structuralDocument.Transact(func(transaction *crdt.Transaction) {
			transaction.GetMap(ygoBlockMetadataRoot).Set(transaction, block.TargetID, map[string]any{
				"parentId": block.TargetParentID, "type": block.Type, "content": content,
			})
		})
		if hasTextAuthority {
			if targetTextDocument.GetText(ygoBlockTextRootPrefix+block.TargetID).Len() != 0 {
				return errPageDuplicationConflict
			}
			targetTextDocument.Transact(func(transaction *crdt.Transaction) {
				transaction.GetText(ygoBlockTextRootPrefix+block.TargetID).ApplyDelta(
					transaction, textDeltas[index],
				)
			})
		}
	}
	structuralState := crdt.EncodeStateAsUpdateV1(structuralDocument, nil)
	if err := replaceYgoStructuralSnapshot(app, item.TargetPageID, structuralState); err != nil {
		return err
	}
	var textState []byte
	if hasTextAuthority {
		textState = crdt.EncodeStateAsUpdateV1(targetTextDocument, nil)
		if err := replaceYgoSnapshot(app, item.TargetPageID, textState); err != nil {
			return err
		}
	}
	keepPageDuplicationDocuments(job.ID, item, documents, structuralState, textState)
	return nil
}

func loadPageDuplicationStructuralDocument(app core.App, pageID string) (*crdt.Doc, error) {
	document, _, err := loadYgoStructuralDoc(app, pageID)
	return document, err
}

func loadPageDuplicationTextDocuments(
	app core.App,
	sourcePageID, targetPageID string,
) (*crdt.Doc, *crdt.Doc, bool, error) {
	sourceDocument, found, err := loadYgoDoc(app, sourcePageID)
	if err != nil {
		return nil, nil, false, err
	}
	if !found {
		return sourceDocument, crdt.New(), false, nil
	}
	targetDocument, _, err := loadYgoDoc(app, targetPageID)
	if err != nil {
		return nil, nil, false, err
	}
	return sourceDocument, targetDocument, true, nil
}

// pageDuplicationStructuralAfterIDs is, for each block of a window, the copy
// of its nearest earlier sibling already copied — every block of the page up
// to the window's last, as windows copy in ID order — reading each parent's
// children once: found block by block, a long page's copy scanned all the
// earlier siblings of each block (2026-10-16: a page of 3,000 paragraphs took
// over 40 minutes and kept the server busy).
func pageDuplicationStructuralAfterIDs(
	app core.App,
	jobID string,
	ownerID string,
	sourcePageID string,
	blocks []pageDuplicationSourceBlock,
) (map[string]*string, error) {
	afterIDs := map[string]*string{}
	inWindow := map[string]bool{}
	parents := []string{}
	lastID := ""
	for _, block := range blocks {
		inWindow[block.ID] = true
		lastID = max(lastID, block.ID)
		if !slices.Contains(parents, block.ParentID) {
			parents = append(parents, block.ParentID)
		}
	}
	for _, parentID := range parents {
		siblings := []struct {
			ID       string `db:"id"`
			TargetID string `db:"target_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[source]].[[id]], COALESCE([[mapping]].[[target_id]], '') AS [[target_id]]
			FROM {{leafen_blocks}} [[source]] INDEXED BY [[idx_leafen_blocks_page_order]]
			LEFT JOIN {{leafen_page_duplication_identity_map}} [[mapping]]
				ON [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='block'
				AND [[mapping]].[[source_id]]=[[source]].[[id]] AND [[mapping]].[[source_page_id]]=[[source]].[[page_id]]
			WHERE [[source]].[[owner]]={:ownerId} AND [[source]].[[page_id]]={:sourcePageId}
				AND [[source]].[[parent_id]]={:parentId}
			ORDER BY [[source]].[[position]], [[source]].[[id]]
		`).Bind(dbx.Params{
			"jobId": jobID, "ownerId": ownerID, "sourcePageId": sourcePageID, "parentId": parentID,
		}).All(&siblings); err != nil {
			return nil, err
		}
		var copied *string
		for _, sibling := range siblings {
			if inWindow[sibling.ID] {
				afterIDs[sibling.ID] = copied
			}
			if sibling.ID <= lastID && sibling.TargetID != "" {
				target := sibling.TargetID
				copied = &target
			}
		}
	}
	return afterIDs, nil
}
