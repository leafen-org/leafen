package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database out of everyone's sight — in its 30 days after a permanent
// deletion, being deleted, or gone for good (page_deletion_hold.go) — leaves
// what other pages keep of it as its purge leaves it
// (page_permanent_delete_cleanup_lanes.go), in its 30 days as after, the
// rule the exports keep (workspace_export_gone_links.go):
//   - a database that shows sources of its own, or other linked ones, shows
//     those: the members it links from it, and their views, leave its reads;
//   - a linked database with nothing else to show is an empty link: its views
//     read as views waiting for their source, as a linked view not yet linked
//     does ("데이터 소스 연결"), and it moves, goes and links anew as one;
//   - a relation to it stays its database's, with none of its rows, its name
//     and settings changeable.
// A write keeps what its writer can't see: the members a database holds out
// of sight stay through it, and come back with their database in its 30
// days; a relation keeps its target.

// dataSourcesInSight sorts data sources by whether everyone may still see
// them: in the workspace, outside every deletion's fence, the page holding
// each and the pages over it keeping none. One gone for good is not found.
// Each is found by its ID and each page's fence by its ancestors, so a page
// read naming a few sources costs a few lookups, whatever the workspace holds.
func dataSourcesInSight(app core.App, workspaceID string, ids []string) (map[string]bool, error) {
	seen := make(map[string]bool, len(ids))
	unknown := []string{}
	for _, id := range ids {
		if _, known := seen[id]; !known && id != "" {
			seen[id] = false
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return seen, nil
	}
	encoded, err := json.Marshal(unknown)
	if err != nil {
		return nil, err
	}
	found := []struct {
		ID          string `db:"id"`
		PageID      string `db:"page_id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]], [[workspace_id]] FROM {{leafen_data_sources}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&found); err != nil {
		return nil, err
	}
	for _, source := range found {
		if source.WorkspaceID != workspaceID {
			continue
		}
		fenced, err := pagePermanentDeleteFenceActive(app, source.PageID)
		if err != nil {
			return nil, err
		}
		seen[source.ID] = !fenced
	}
	return seen, nil
}

// goneDatabaseLinks answers, for one page, which sources it names are out of
// sight.
type goneDatabaseLinks struct {
	app         core.App
	workspaceID string
	inSight     map[string]bool
}

func newGoneDatabaseLinks(app core.App, workspaceID string) *goneDatabaseLinks {
	return &goneDatabaseLinks{app: app, workspaceID: workspaceID, inSight: map[string]bool{}}
}

func (links *goneDatabaseLinks) load(ids []string) error {
	unknown := []string{}
	for _, id := range ids {
		if _, known := links.inSight[id]; !known && id != "" {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	seen, err := dataSourcesInSight(links.app, links.workspaceID, unknown)
	for id, shown := range seen {
		links.inSight[id] = shown
	}
	return err
}

func (links *goneDatabaseLinks) gone(id string) bool {
	shown, known := links.inSight[id]
	return known && !shown
}

// remaining is what a database shows of its members once those out of sight
// leave it, and whether any left; with none of its own and no linked one
// left, it is an empty link.
func (links *goneDatabaseLinks) remaining(content databaseContainerBlockContent) ([]string, []databaseContainerLinkedMember, bool, error) {
	if err := links.load(content.memberDataSourceIDs()); err != nil {
		return nil, nil, false, err
	}
	linked := content.linkedMemberList()
	kept := slices.DeleteFunc(slices.Clone(linked), func(member databaseContainerLinkedMember) bool {
		return links.gone(member.DataSourceID)
	})
	return content.ownDataSourceIDs(), kept, len(kept) != len(linked), nil
}

// pageWorkspace is the workspace a page is in.
func pageWorkspace(app core.App, pageID string) (string, error) {
	workspaces := []string{}
	err := app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id} LIMIT 1`).
		Bind(dbx.Params{"id": pageID}).Column(&workspaces)
	if err != nil || len(workspaces) == 0 {
		return "", err
	}
	return workspaces[0], nil
}
