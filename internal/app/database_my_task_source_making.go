package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// withMyTaskSourceMaking gives each picked source the person may add to what
// a task made in it shows once the server has it, so the page draws a task
// it makes on its device that way from the start (myTaskMadeHere.ts): the
// default template the server fills it from (createMyTask, and a row made
// on the device, content_graph_row_templates.go) — its title, icon and
// values — and what it is called while it has no title
// (databaseUntitledRowName) — and its database's properties as the person
// reads them (myTaskSourceProperties), which the task's page shows with its
// head and body. 2026-11-25: a task made in a source this device had not
// opened showed "새 작업" until a read after the server had made it from its
// template, and its page's properties 60–100 ms after its body.
func withMyTaskSourceMaking(ctx context.Context, app core.App, readerID string, sources []databaseMyTaskSourceWire) error {
	for index := range sources {
		if !sources[index].CanEdit {
			continue
		}
		properties, err := myTaskSourceProperties(app, readerID, sources[index])
		if err != nil {
			return err
		}
		sources[index].Properties = properties
		template, err := liveDefaultTemplateMetadata(app, sources[index].DataSourceID)
		if err != nil {
			return err
		}
		sources[index].DefaultTemplate = template
		if sources[index].UntitledName, err = databaseUntitledRowNameFor(ctx, app, readerID, sources[index].DataSourceID); err != nil {
			return err
		}
	}
	return nil
}

// liveDefaultTemplateMetadata is a data source's live default template
// (databaseDefaultTemplateID) as its toolbar lists it, or nil.
func liveDefaultTemplateMetadata(app core.App, dataSourceID string) (*databaseToolbarTemplateMetadataWire, error) {
	template := storedDatabaseToolbarTemplate{}
	err := app.DB().NewQuery(`SELECT [[template]].[[id]], [[template]].[[name]], [[template]].[[icon]],
		[[template]].[[title]], [[template]].[[properties_json]], [[template]].[[property_spans_json]],
		[[template]].[[is_default]], [[template]].[[position]],
		COALESCE([[state]].[[authority_revision]],1) AS [[authority_revision]],
		COALESCE([[state]].[[deleted_at]],'') AS [[deleted_at]], [[template]].[[updated_at]],
		COALESCE([[state]].[[repeat_json]],'') AS [[repeat_json]],
		COALESCE([[state]].[[repeat_next_run_at]],0) AS [[repeat_next_run_at]],
		COALESCE([[state]].[[repeat_paused_reason]],'') AS [[repeat_paused_reason]],
		EXISTS (
			SELECT 1 FROM {{leafen_blocks}} [[content_block]] INDEXED BY [[idx_leafen_blocks_page_order]]
			WHERE [[content_block]].[[owner]] = [[template]].[[owner]] AND [[content_block]].[[page_id]] = [[template]].[[id]]
		) AS [[has_content]]
		FROM {{leafen_database_templates}} AS [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} AS [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[data_source_id]] = {:id} AND [[template]].[[is_default]] = 1
			AND COALESCE([[state]].[[deleted_at]], '') = ''
		ORDER BY [[template]].[[position]], [[template]].[[id]] LIMIT 1`).
		Bind(dbx.Params{"id": dataSourceID}).One(&template)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	wire := databaseToolbarTemplateMetadataWires([]storedDatabaseToolbarTemplate{template})[0]
	return &wire, nil
}
