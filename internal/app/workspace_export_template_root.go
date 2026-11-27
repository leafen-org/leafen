package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database template exported on its own from its page's "…" comes out as
// a database page exported on its own does: its title, the values it holds
// under its database's properties, and its body. Notion's template page
// exports its HTML, Markdown & CSV and PDF as a page's (seen 2026-11-16: a
// template "NT1" with a body line "b" came out as "# NT1\n\nb"). A template
// is no row (database_template_row_page.go), so the row stages don't meet
// it: this stage gives the export's root template as the row and row page
// blocks they give a root database page.

// workspaceExportTemplateRootSQL names the export's root when it is a template.
const workspaceExportTemplateRootSQL = `[[template]].[[id]] = {:targetId}
	AND EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[root]]
		WHERE [[root]].[[job_id]] = {:jobId} AND [[root]].[[page_id]] = [[template]].[[id]]
			AND [[root]].[[is_root]] AND [[root]].[[row_page]])`

func loadWorkspaceExportScopedTemplateRootWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Scope.Kind != workspaceExportScopePage || !workspaceExportRendersDocuments(payload.Format) {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "11", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPart"], params["afterId"] = after[0], after[1]
	return queryWorkspaceExportStage(app, `
		SELECT [[kind]], [[sort_key]], [[page_id]], [[payload]] FROM (
			SELECT 'row' AS [[kind]], '0' AS [[part]], [[template]].[[id]] AS [[item_id]],
				'11:0:' || [[template]].[[id]] AS [[sort_key]], [[template]].[[page_id]], json_object(
					'id', [[template]].[[id]], 'pageId', [[template]].[[page_id]], 'dataSourceId', [[template]].[[data_source_id]],
					'position', [[template]].[[position]], 'icon', [[record]].[[icon]], 'iconType', [[record]].[[icon_type]],
					'cover', [[record]].[[cover]], 'coverPosition', [[record]].[[cover_position]], 'font', [[record]].[[font]],
					'smallText', json(CASE WHEN [[record]].[[small_text]] THEN 'true' ELSE 'false' END),
					'fullWidth', json(CASE WHEN [[record]].[[full_width]] THEN 'true' ELSE 'false' END),
					'tableOfContentsDisplay', [[record]].[[table_of_contents_display]],
					'templateTitle', [[template]].[[title]], 'templateValues', json([[template]].[[properties_json]]),
					'templateSpans', json([[template]].[[property_spans_json]])) AS [[payload]]
			FROM {{leafen_database_templates}} AS [[template]]
			JOIN {{leafen_pages}} AS [[record]] ON [[record]].[[id]] = [[template]].[[id]] AND [[record]].[[database_template]]
			WHERE `+workspaceExportTemplateRootSQL+` AND [[template]].[[workspace_id]] = {:workspaceId}
				AND [[template]].[[owner]] = {:ownerId}
			UNION ALL
			SELECT 'rowPageBlock', '1', [[items]].[[id]], '11:1:' || [[items]].[[id]], [[template]].[[page_id]], json_object(
					'id', [[items]].[[id]], 'pageId', [[template]].[[page_id]], 'rowId', [[template]].[[id]],
					'parentId', [[items]].[[parent_id]], 'position', [[items]].[[position]],
					'type', [[items]].[[type]], 'content', json([[items]].[[content]]),
					'createdAt', [[items]].[[created]], 'updatedAt', [[items]].[[updated]])
			FROM {{leafen_blocks}} AS [[items]]
			JOIN {{leafen_database_templates}} AS [[template]] ON [[template]].[[id]] = [[items]].[[page_id]]
				AND [[template]].[[workspace_id]] = [[items]].[[workspace_id]]
			WHERE `+workspaceExportTemplateRootSQL+` AND [[items]].[[workspace_id]] = {:workspaceId}
				AND [[items]].[[owner]] = {:ownerId}
		) WHERE ([[part]], [[item_id]]) > ({:afterPart}, {:afterId})
		ORDER BY [[part]], [[item_id]] LIMIT {:limit}
	`, params, limit)
}

// workspaceExportTemplateRowValues gives the export's root template, as a
// row, the values it holds, under its database's property types as a row's
// cells are read (attachWorkspaceExportRowValues), its title the title's.
func workspaceExportTemplateRowValues(app core.App, row map[string]any) error {
	held, ok := row["templateValues"].(map[string]any)
	if !ok {
		return nil
	}
	spans, _ := row["templateSpans"].(map[string]any)
	title, _ := row["templateTitle"].(string)
	delete(row, "templateValues")
	delete(row, "templateSpans")
	delete(row, "templateTitle")
	properties := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:source}`).
		Bind(dbx.Params{"source": mapString(row, "dataSourceId")}).All(&properties); err != nil {
		return err
	}
	values := map[string]any{}
	for _, property := range properties {
		if property.Type == graphValueTypeTitle {
			values[property.ID] = map[string]any{"type": property.Type, "value": title}
			continue
		}
		value, present := held[property.ID]
		if !present || value == nil {
			continue
		}
		cell := map[string]any{"type": property.Type, "value": value}
		if written, ok := spans[property.ID]; ok {
			if encoded, err := json.Marshal(written); err == nil && len(databaseStoredSpans(string(encoded))) > 0 {
				cell["spans"] = written
			}
		}
		values[property.ID] = cell
	}
	row["values"] = values
	return nil
}
