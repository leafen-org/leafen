package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A view the API makes in a database may show another database's data
// source, as Notion's does (seen 2026-11-12: POST /v1/views with database A
// and database B's data_source_id, a tab of A's or a widget of A's
// dashboard, answers 200; A shows the tab as a linked view, "↗ B" over its
// rows; A's data_sources stay its own; the view lists with A's views and
// with B's source's, not with B's views). Leafen holds such a source as one
// the database links (database_container_linked_members.go), as its screen
// links one: the view's making adds the link, and the view is the
// database's own, as a linked source's views are. A form there is refused
// as a linked source's is (notionLinkedFormMessage); a dashboard shows no
// source, so it takes the database's first.

// notionViewDatabase is a database a view goes in: its page, and whose
// page that is.
type notionViewDatabase struct {
	ID      string `db:"id"`
	PageID  string `db:"page_id"`
	OwnerID string `db:"owner"`
}

// notionViewMember is how a database holds the source a view in it shows:
// the source's page, the database that owns it when the database links it
// (Linked), and whether the view's making links it (link).
type notionViewMember struct {
	SourcePageID string `db:"source_page_id"`
	Linked       string `db:"source_container_id"`
	link         bool
}

// loadNotionViewDatabase is the database by its internal ID, none when it
// is in the trash.
func loadNotionViewDatabase(app core.App, databaseID string) (notionViewDatabase, error) {
	database := notionViewDatabase{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[page_id]], [[container]].[[owner]]
		FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]] = {:containerId}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]])
	`).Bind(dbx.Params{"containerId": databaseID}).One(&database)
	if errors.Is(err, sql.ErrNoRows) {
		return database, errDatabaseRowPageNotFound
	}
	return database, err
}

// findNotionViewMember is how the database holds the source: as its own or
// linked, or, for another database's source, linked by the view's making.
// A source no database owns is errDatabaseRowPageNotFound.
func findNotionViewMember(app core.App, databaseID, dataSourceID string) (notionViewMember, error) {
	member := notionViewMember{}
	err := app.DB().NewQuery(`
		SELECT [[membership]].[[source_container_id]], [[source]].[[page_id]] AS [[source_page_id]]
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[membership]].[[data_source_id]]
		WHERE [[membership]].[[container_id]] = {:containerId} AND [[membership]].[[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"containerId": databaseID, "dataSourceId": dataSourceID}).One(&member)
	if !errors.Is(err, sql.ErrNoRows) {
		return member, err
	}
	// The database that owns it, first made first.
	owner := struct {
		ContainerID string `db:"container_id"`
		PageID      string `db:"source_page_id"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[membership]].[[container_id]], [[source]].[[page_id]] AS [[source_page_id]]
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[membership]].[[data_source_id]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[data_source_id]] = {:dataSourceId} AND [[membership]].[[source_container_id]] = ''
			AND [[container]].[[kind]] = 'source'
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]])
		ORDER BY [[container]].[[created]], [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).One(&owner)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner.ContainerID == databaseID {
		return member, errDatabaseRowPageNotFound
	}
	if err != nil {
		return member, err
	}
	return notionViewMember{SourcePageID: owner.PageID, Linked: owner.ContainerID, link: true}, nil
}

// notionViewLinkInput adds to a view's making, from the page as it stands,
// the database's link to the source, unless it has one by then.
func notionViewLinkInput(
	database notionViewDatabase, dataSourceID string, member notionViewMember,
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	if !member.link {
		return nil
	}
	return func(txApp core.App, _ pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
		metadata, err := structuralMetadata(doc)
		if err != nil {
			return input, err
		}
		entry, _, err := notionDataSourceMoveContainer(metadata, database.ID)
		if err != nil {
			return input, err
		}
		// The block as the change already writes it, when it does (a change
		// tried again holds the link it made then), or as it stands.
		at := slices.IndexFunc(input.Operations, func(operation graphOperation) bool {
			return operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.ID == database.ID
		})
		current := entry.Content
		if at >= 0 {
			current = map[string]any{}
			if err := json.Unmarshal(input.Operations[at].Block.Content, &current); err != nil {
				return input, err
			}
		}
		encoded, err := json.Marshal(current)
		if err != nil {
			return input, err
		}
		content, err := parseDatabaseContainerBlockContent(encoded)
		if err != nil {
			return input, errDatabaseRowPageConflict
		}
		own, linked := content.ownDataSourceIDs(), content.linkedMemberList()
		if slices.Contains(own, dataSourceID) || slices.ContainsFunc(linked, func(held databaseContainerLinkedMember) bool {
			return held.DataSourceID == dataSourceID
		}) {
			return input, nil
		}
		linked = append(linked, databaseContainerLinkedMember{DataSourceID: dataSourceID, SourceContainerID: member.Linked})
		block, err := notionDataSourceMoveBlock(txApp, entry, database.ID, database.PageID, database.OwnerID,
			withDatabaseContainerMembers(current, own, linked))
		if err != nil {
			return input, err
		}
		operations := slices.Clone(input.Operations)
		if at >= 0 {
			operations[at] = graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}
		} else {
			operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
		}
		input.Operations = operations
		return input, nil
	}
}

// notionViewFirstSource is the database's first source, own or linked,
// which a dashboard of another database's source takes.
func notionViewFirstSource(app core.App, databaseID string) (string, error) {
	var sourceID string
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_database_container_sources}}
		WHERE [[container_id]] = {:containerId}
		ORDER BY [[position]], [[data_source_id]] LIMIT 1
	`).Bind(dbx.Params{"containerId": databaseID}).Row(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errDatabaseRowPageNotFound
	}
	return sourceID, err
}
