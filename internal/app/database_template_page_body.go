package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A template body lives in its template's page, edited live as a database
// page's body is by someone who may edit its database. A body given as
// blocks, as a new template's or an archived one, is written into the page;
// archives carry it and each database page made from the template copies it.

type databaseTemplateBodyBlock struct {
	ID       string          `json:"id"`
	ParentID string          `json:"parentId"`
	Position string          `json:"position"`
	Type     string          `json:"type"`
	Content  json.RawMessage `json:"content"`
}

// databaseTemplateBodyBlocks turns a template body given as blocks into the
// template page's blocks. A block keeps its ID under the template's, so a
// body saved again keeps its IDs and two templates never share one. A stored
// body moving to its page keeps block kinds a new body may not take.
func databaseTemplateBodyBlocks(templateID string, raw json.RawMessage, stored bool) ([]newRowPageBlock, error) {
	blocks := []graphBlock{}
	if len(raw) > 0 && json.Unmarshal(raw, &blocks) != nil {
		return nil, databaseTemplateBodyRefused("blocks is a list of blocks, each with id, parentId, position, type and content")
	}
	scoped := func(id string) string {
		if id == "" || strings.HasPrefix(id, templateID+":") {
			return id
		}
		return templateID + ":" + id
	}
	for index := range blocks {
		blocks[index].ID = scoped(blocks[index].ID)
		if blocks[index].ParentID != nil {
			parentID := scoped(*blocks[index].ParentID)
			blocks[index].ParentID = &parentID
		}
	}
	prepared, err := prepareDatabaseTemplateBody(blocks, stored)
	if err != nil {
		return nil, err
	}
	body := make([]newRowPageBlock, 0, len(prepared))
	for _, block := range prepared {
		content := map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil {
			return nil, errDatabaseToolbarConfigConflict
		}
		body = append(body, newRowPageBlock{
			ID: block.ID, ParentID: block.ParentID, Position: block.Position, Type: block.Type, Content: content,
		})
	}
	return body, nil
}

// loadDatabaseTemplatePageBody reads a template page's body as blocks with
// their text, the form a template body is given and exported in. Rich keeps
// the text's formatting, which archives carry; the template editor takes the
// plain text it edits.
func loadDatabaseTemplatePageBody(app core.App, templateID string, rich bool) ([]map[string]any, error) {
	stored := []struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
		Position string `db:"position"`
		Type     string `db:"type"`
		Content  string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:templateId}
		ORDER BY [[parent_id]], [[position]], [[id]]
	`).Bind(dbx.Params{"templateId": templateID}).All(&stored); err != nil {
		return nil, err
	}
	blocks := make([]map[string]any, 0, len(stored))
	if len(stored) == 0 {
		return blocks, nil
	}
	document, err := loadRowPageTextDocument(app, templateID)
	if err != nil {
		return nil, err
	}
	for _, block := range stored {
		content := map[string]any{}
		if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
			return nil, err
		}
		if spans, text := rowPageBlockText(document, block.ID); len(spans) > 0 {
			content["text"] = text
			if rich {
				content["rich"] = spans
			}
		}
		var parentID any
		if block.ParentID != "" {
			parentID = block.ParentID
		}
		blocks = append(blocks, map[string]any{
			"id": block.ID, "parentId": parentID, "position": block.Position, "type": block.Type, "content": content,
		})
	}
	return blocks, nil
}

// replaceDatabaseTemplatePageBody gives a template page the body given as
// blocks in place of the one it has, as a new or imported template's page
// gets its first body; the page documents are written afresh.
func replaceDatabaseTemplatePageBody(app core.App, templateID, workspaceID, actorID string, raw json.RawMessage) error {
	if len(raw) > databaseRowPageMaxBodyBytes {
		return databaseTemplateBodyRefused("blocks is %d bytes, over the %d a row's page keeps", len(raw), databaseRowPageMaxBodyBytes)
	}
	blocks, err := databaseTemplateBodyBlocks(templateID, raw, false)
	if err != nil {
		return err
	}
	// Its files pass the graph's one check (content_graph_given_body_files.go),
	// which says what of them it refuses.
	if err := checkGivenPageBodyBlockFiles(app, workspaceID, blocks); err != nil {
		if refused := (&graphRequestError{}); errors.As(err, &refused) {
			return databaseToolbarRefused("blocks: %s", refused.message)
		}
		return err
	}
	for _, statement := range []string{
		`DELETE FROM {{leafen_file_references}} WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:templateId}
			AND [[owner_kind]] IN ('block', 'block_trash')`,
		// The blocks are found by their page; the workspace is read after
		// (+), or SQLite, which keeps no statistics, reads the workspace's
		// every block.
		`DELETE FROM {{leafen_blocks}} WHERE [[page_id]] = {:templateId} AND +[[workspace_id]] = {:workspaceId}`,
		`DELETE FROM {{leafen_ygo_updates}} WHERE [[room]] = {:templateId}`,
		`DELETE FROM {{leafen_ygo_structural_updates}} WHERE [[room]] = {:templateId}`,
		`DELETE FROM {{leafen_ygo_documents}} WHERE [[room]] = {:templateId}`,
		`DELETE FROM {{leafen_ygo_structural_documents}} WHERE [[room]] = {:templateId}`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(dbx.Params{
			"templateId": templateID, "workspaceId": workspaceID,
		}).Execute(); err != nil {
			return err
		}
	}
	return writeRowPageBody(app, templateID, actorID, blocks, time.Now().UTC().UnixMilli(), false)
}

// prepareDatabaseTemplateBody checks a template body given as blocks: unique
// IDs, parents inside the body without cycles, block kinds a database page's
// body holds, as the body is copied into database pages, and file blocks in
// their stored form.
func prepareDatabaseTemplateBody(blocks []graphBlock, stored bool) ([]databaseTemplateBodyBlock, error) {
	ids := make(map[string]int, len(blocks))
	parents := make(map[string]string, len(blocks))
	for index := range blocks {
		block := &blocks[index]
		id, err := normalizeGraphID(block.ID)
		if err != nil {
			return nil, databaseTemplateBodyRefused("blocks[%d].id is 1–%d characters", index, maxGraphIdentifierLength)
		}
		if first, duplicate := ids[id]; duplicate {
			return nil, databaseTemplateBodyRefused("blocks[%d].id is blocks[%d]'s too: each block has its own", index, first)
		}
		block.ID = id
		ids[id] = index
		if block.ParentID != nil {
			parentID, err := normalizeGraphID(*block.ParentID)
			if err != nil {
				return nil, databaseTemplateBodyRefused("blocks[%d].parentId is left out, or 1–%d characters", index, maxGraphIdentifierLength)
			}
			block.ParentID = &parentID
			parents[id] = parentID
		}
	}
	for index := range blocks {
		id := blocks[index].ID
		parentID, nested := parents[id]
		if !nested {
			continue
		}
		if parentID == id {
			return nil, databaseTemplateBodyRefused("blocks[%d].parentId is the block itself", index)
		}
		if _, exists := ids[parentID]; !exists {
			return nil, databaseTemplateBodyRefused("blocks[%d].parentId names no block of the body", index)
		}
		for seen := map[string]struct{}{id: {}}; parentID != ""; parentID = parents[parentID] {
			if _, cycle := seen[parentID]; cycle {
				return nil, databaseTemplateBodyRefused("blocks[%d].parentId leads round in a circle back to it", index)
			}
			seen[parentID] = struct{}{}
		}
	}
	prepared := make([]databaseTemplateBodyBlock, 0, len(blocks))
	for index := range blocks {
		block := &blocks[index]
		position, err := normalizeGraphPosition(block.Position)
		if err != nil {
			return nil, databaseTemplateBodyRefused("blocks[%d].%s", index, firstNonempty(refusalReason(err), "position is empty"))
		}
		content, err := normalizeJSONObject(block.Content)
		if err != nil {
			return nil, databaseTemplateBodyRefused("blocks[%d].content is an object", index)
		}
		block.Type = strings.TrimSpace(block.Type)
		// A body given as blocks holds no sub-page's block: a template's
		// sub-page is a page made under its page.
		if _, supported := graphBlockTypes[block.Type]; !supported {
			return nil, databaseTemplateBodyRefused("blocks[%d].type %q isn't a block type", index, block.Type)
		}
		if !stored && (databaseRowBodyRejectsType(block.Type) || block.Type == graphBlockTypeChildPage) {
			return nil, databaseTemplateBodyRefused("blocks[%d].type %s isn't one a template's body holds: a database page's body has no pages or databases of its own", index, block.Type)
		}
		candidate := &graphBlock{Type: block.Type, Content: content}
		if graphBlockCarriesManagedFileReference(candidate) {
			if _, err := parseGraphFileBlockContent(content); err != nil {
				return nil, databaseTemplateBodyRefused("blocks[%d].content names its file as an uploaded file of the workspace", index)
			}
		} else if block.Type == graphBlockTypeFile {
			external, err := parseGraphExternalFileBlockContent(content)
			if err != nil {
				return nil, databaseTemplateBodyRefused("blocks[%d].content is a file's address and name", index)
			}
			content, err = json.Marshal(external)
			if err != nil {
				return nil, errDatabaseToolbarConfigConflict
			}
		} else if block.Type == graphBlockTypePDF {
			external, err := parseGraphExternalPDFBlockContent(content)
			if err != nil {
				return nil, databaseTemplateBodyRefused("blocks[%d].content is a PDF's address", index)
			}
			content, err = json.Marshal(external)
			if err != nil {
				return nil, errDatabaseToolbarConfigConflict
			}
		} else if _, managedFile := graphManagedFileBlockTypes[block.Type]; managedFile && block.Type != graphBlockTypeEmbed {
			// An embed with no file is a link drawn in its service's frame, as
			// a page's is: Notion's task tracker template holds empty Google
			// Drive and Figma ones (seen 2026-11-06).
			return nil, databaseTemplateBodyRefused("blocks[%d] is a %s block with no file: upload the file first and name it in content", index, block.Type)
		}
		parentID := ""
		if block.ParentID != nil {
			parentID = *block.ParentID
		}
		prepared = append(prepared, databaseTemplateBodyBlock{
			ID: block.ID, ParentID: parentID, Position: position, Type: block.Type, Content: content,
		})
	}
	return prepared, nil
}

// workspaceExportTemplatePayloadWithBody puts a template page's body in an
// exported template, as archives carry a template with its body.
func workspaceExportTemplatePayloadWithBody(app core.App, payload string) (string, error) {
	value := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return "", err
	}
	if err := keepExportedDatabaseTemplateValues(app, value); err != nil {
		return "", err
	}
	templateID, _ := value["id"].(string)
	blocks, err := loadDatabaseTemplatePageBody(app, templateID, true)
	if err != nil {
		return "", err
	}
	value["blocks"] = blocks
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

// isDatabaseTemplatePageID reports whether a page ID is a template's page.
func isDatabaseTemplatePageID(app core.App, pageID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[database_template]]`).
		Bind(dbx.Params{"id": pageID}).Row(&count)
	return count == 1, err
}

// authorizeDatabaseTemplateBody admits a template page's body to someone with
// the role on its database's page, as the template editor is. Changes need a
// live template whose database's page is not in the trash.
func authorizeDatabaseTemplateBody(
	app core.App, principalID, templateID string, minimum pageAccessRole,
) (pageAuthorization, error) {
	return authorizeDatabaseTemplateBodyBy(app, templateID, minimum, func(containerID string) (pageAuthorization, error) {
		return authorizePageAccessRole(app, principalID, containerID, minimum)
	})
}

// authorizeDatabaseTemplateBodyBy is authorizeDatabaseTemplateBody with the
// role on its database's page decided by container: a person's or the
// system's (system_job_access.go).
func authorizeDatabaseTemplateBodyBy(
	app core.App, templateID string, minimum pageAccessRole,
	container func(containerID string) (pageAuthorization, error),
) (pageAuthorization, error) {
	page, err := app.FindRecordById(pagesCollection, templateID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !isDatabaseTemplatePageRecord(page) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	template := struct {
		PageID    string `db:"page_id"`
		DeletedAt string `db:"deleted_at"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[template]].[[page_id]], COALESCE([[state]].[[deleted_at]], '') AS [[deleted_at]]
		FROM {{leafen_database_templates}} [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": templateID}).One(&template)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	above, err := container(template.PageID)
	if err != nil {
		return pageAuthorization{}, err
	}
	if minimum >= pageAccessRoleComment {
		if template.DeletedAt != "" {
			return pageAuthorization{}, permissionDenied()
		}
		if trashed, err := pageInTrashPath(app, template.PageID); err != nil {
			return pageAuthorization{}, err
		} else if trashed {
			return pageAuthorization{}, pageInTrashError()
		}
	}
	return pageAuthorization{Workspace: above.Workspace, Page: page, AccessRole: above.AccessRole}, nil
}
