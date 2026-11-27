package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// goneDatabaseLinksAside is what a write keeps that its writer can't see
// (database_gone_links_live.go): the members out of sight each database it
// writes already holds, taken out of the database while the write is
// checked, as its reads leave them out, and put back once it is, those not
// gone for good coming back with their database in its 30 days.
type goneDatabaseLinksAside struct {
	held map[string][]databaseContainerLinkedMember
}

// authorizeLinkedDatabaseReferencesKeepingGone authorizes what a change
// links from other pages (authorizeLinkedDatabaseReferences) once the
// databases it writes have set aside their members out of sight, and admits
// the targets out of sight that its relations keep.
func authorizeLinkedDatabaseReferencesKeepingGone(
	app core.App, principalID string, current pageAuthorization, pageID string, operations []graphOperation,
) (map[string]graphEntityReference, map[string]graphEntityReference, goneDatabaseLinksAside, error) {
	aside := goneDatabaseLinksAside{held: map[string][]databaseContainerLinkedMember{}}
	links := newGoneDatabaseLinks(app, current.Workspace.WorkspaceID)
	targets := map[string]graphEntityReference{}
	for _, operation := range operations {
		var err error
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil &&
			operation.Block.Type == graphBlockTypeInlineDatabase:
			err = aside.database(links, pageID, operation.Block)
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			err = keepGoneRelationTarget(links, current.Workspace.OwnerID, pageID, operation.Property, targets)
		}
		if err != nil {
			return nil, nil, aside, err
		}
	}
	sources, properties, err := authorizeLinkedDatabaseReferences(app, principalID, current, pageID, operations)
	if err != nil {
		return nil, nil, aside, err
	}
	for id, reference := range targets {
		if _, admitted := sources[id]; !admitted {
			sources[id] = reference
		}
	}
	return sources, properties, aside, nil
}

// database sets aside the members out of sight the database already holds.
// An empty link written as it stands names its source still: it is
// refused, as a link to a source no one sees is.
func (aside goneDatabaseLinksAside) database(links *goneDatabaseLinks, pageID string, block *graphBlock) error {
	stored := []string{}
	if err := links.app.DB().NewQuery(`
		SELECT [[content]] FROM {{leafen_blocks}}
		WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[type]] = {:type} LIMIT 1
	`).Bind(dbx.Params{"id": block.ID, "pageId": pageID, "type": graphBlockTypeInlineDatabase}).Column(&stored); err != nil || len(stored) == 0 {
		return err
	}
	before, err := parseDatabaseContainerBlockContent(json.RawMessage(stored[0]))
	if err != nil {
		return nil
	}
	written, err := parseDatabaseContainerBlockContent(block.Content)
	if err != nil {
		return nil
	}
	if err := links.load(append(before.memberDataSourceIDs(), written.memberDataSourceIDs()...)); err != nil {
		return err
	}
	held := slices.DeleteFunc(before.linkedMemberList(), func(member databaseContainerLinkedMember) bool {
		return !links.gone(member.DataSourceID)
	})
	// An empty link shows its writer what it held: linked anew, it holds
	// what it is linked to alone.
	if len(held) == 0 || len(before.ownDataSourceIDs()) == 0 && len(held) == len(before.linkedMemberList()) {
		return nil
	}
	heldBefore := func(id string) bool {
		return slices.ContainsFunc(held, func(member databaseContainerLinkedMember) bool { return member.DataSourceID == id })
	}
	own := written.ownDataSourceIDs()
	kept := slices.DeleteFunc(written.linkedMemberList(), func(member databaseContainerLinkedMember) bool {
		return heldBefore(member.DataSourceID)
	})
	if len(own) == 0 && len(kept) == 0 {
		return nil
	}
	content := map[string]any{}
	if err := json.Unmarshal(block.Content, &content); err != nil {
		return nil
	}
	if block.Content, err = json.Marshal(withDatabaseContainerMembers(content, own, kept)); err != nil {
		return err
	}
	// Those gone for good stay gone.
	ids := make([]string, len(held))
	for index, member := range held {
		ids[index] = member.DataSourceID
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	existing := []string{}
	if err := links.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&existing); err != nil {
		return err
	}
	held = slices.DeleteFunc(held, func(member databaseContainerLinkedMember) bool {
		return !slices.Contains(existing, member.DataSourceID) ||
			slices.ContainsFunc(kept, func(other databaseContainerLinkedMember) bool { return other.DataSourceID == member.DataSourceID })
	})
	if len(held) > 0 {
		aside.held[block.ID] = held
	}
	return nil
}

// restore puts back the members each database set aside.
func (aside goneDatabaseLinksAside) restore(operations []graphOperation) {
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil || len(aside.held[operation.Block.ID]) == 0 {
			continue
		}
		written, err := parseDatabaseContainerBlockContent(operation.Block.Content)
		content := map[string]any{}
		if err != nil || json.Unmarshal(operation.Block.Content, &content) != nil {
			continue
		}
		linked := append(written.linkedMemberList(), aside.held[operation.Block.ID]...)
		if encoded, err := json.Marshal(withDatabaseContainerMembers(content, written.ownDataSourceIDs(), linked)); err == nil {
			operation.Block.Content = encoded
		}
	}
}

// keepGoneRelationTarget admits the target out of sight a relation keeps as
// it was: one its writer neither chose nor sees.
func keepGoneRelationTarget(
	links *goneDatabaseLinks, owner, pageID string, property *graphProperty, targets map[string]graphEntityReference,
) error {
	target := graphRelationTarget(property.Type, property.Config)
	if target == "" {
		return nil
	}
	stored := []struct {
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := links.app.DB().NewQuery(`
		SELECT [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId} LIMIT 1
	`).Bind(dbx.Params{"id": property.ID, "pageId": pageID, "dataSourceId": property.DataSourceID}).All(&stored); err != nil ||
		len(stored) == 0 || graphRelationTarget(stored[0].Type, json.RawMessage(stored[0].Config)) != target {
		return err
	}
	if err := links.load([]string{target}); err != nil || !links.gone(target) {
		return err
	}
	// In its 30 days it is on its page still; gone for good, on none.
	pages := []string{}
	if err := links.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": target}).Column(&pages); err != nil {
		return err
	}
	reference := graphEntityReference{ID: target, Owner: owner}
	if len(pages) > 0 {
		reference.PageID = pages[0]
	}
	targets[target] = reference
	return nil
}
