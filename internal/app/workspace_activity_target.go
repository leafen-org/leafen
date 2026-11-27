package app

import (
	"encoding/json"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceActivityTarget struct {
	ID       string `db:"id"`
	PageID   string `db:"page_id"`
	SourceID string `db:"source_id"`
	Title    string `db:"title"`
}

type workspaceActivityTargets struct {
	entities map[string]workspaceActivityTarget
	sources  map[string]string
	// sourceNames are the databases' names now, as the feed names them.
	sourceNames map[string]string
	// shownJobs are the operations whose lines show: those done, but a copy
	// holding a database (workspaceActivityQuietCopySQL).
	shownJobs map[string]bool
	// copyJobs are those of them that copied, by the kind of their job: a
	// page's copy (pageDuplicationJobKind) or blocks copied into a page
	// (blockMoveJobKind; workspace_activity_copy_changes.go).
	copyJobs map[string]string
	// onlyReader is whether only the reader opens each page now
	// (workspace_activity_only_reader.go).
	onlyReader workspaceActivityOnlyReader
}

// Resource locations, linked sources and job outcomes are references, not
// grants. Fetch them in bounded batches; each distinct page still goes through
// the existing page authority, cached within this one transaction.
func loadWorkspaceActivityTargets(app core.App, rows []storedWorkspaceActivity) (workspaceActivityTargets, error) {
	result := workspaceActivityTargets{entities: map[string]workspaceActivityTarget{}, sources: map[string]string{},
		sourceNames: map[string]string{}, shownJobs: map[string]bool{}, copyJobs: map[string]string{}, onlyReader: workspaceActivityOnlyReader{}}
	groups := map[string][]string{}
	sources, jobs := []string{}, []string{}
	for _, row := range rows {
		groups[row.EntityType] = append(groups[row.EntityType], row.EntityID)
		var metadata workspaceActivityMetadata
		if json.Unmarshal([]byte(row.Metadata), &metadata) != nil {
			continue
		}
		if metadata.SourceID != "" {
			sources = append(sources, metadata.SourceID)
		}
		if metadata.OperationJobID != "" {
			jobs = append(jobs, metadata.OperationJobID)
		}
	}
	for _, entity := range []string{"dataSource", "view", "property", "block"} {
		if len(groups[entity]) == 0 {
			continue
		}
		table, source := "", "[[data_source_id]]"
		switch entity {
		case "dataSource":
			table, source = "leafen_data_sources", "[[id]]"
		case "view":
			table = "leafen_database_views"
		case "property":
			table = "leafen_database_properties"
		case "block":
			table, source = "leafen_blocks", "''"
		}
		encoded, err := json.Marshal(groups[entity])
		if err != nil {
			return result, err
		}
		targets := []workspaceActivityTarget{}
		if err = app.DB().NewQuery(`SELECT [[id]],[[page_id]],` + source + ` AS [[source_id]] FROM {{` + table + `}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).All(&targets); err != nil {
			return result, err
		}
		for _, target := range targets {
			result.entities[entity+"\x00"+target.ID] = target
			if target.SourceID != "" {
				sources = append(sources, target.SourceID)
			}
		}
	}
	if len(sources) > 0 {
		encoded, err := json.Marshal(sources)
		if err != nil {
			return result, err
		}
		targets := []workspaceActivityTarget{}
		if err = app.DB().NewQuery(`SELECT [[id]],[[page_id]],[[title]] FROM {{leafen_data_sources}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).All(&targets); err != nil {
			return result, err
		}
		for _, target := range targets {
			result.sources[target.ID] = target.PageID
			result.sourceNames[target.ID] = target.Title
		}
	}
	if len(jobs) > 0 {
		encoded, err := json.Marshal(jobs)
		if err != nil {
			return result, err
		}
		shown := []struct {
			ID        string `db:"id"`
			Kind      string `db:"kind"`
			Operation string `db:"operation"`
		}{}
		if err = app.DB().NewQuery(`SELECT [[job]].[[id]],[[job]].[[kind]],COALESCE([[move]].[[operation]],'') AS [[operation]]
			FROM {{leafen_jobs}} AS [[job]] LEFT JOIN {{leafen_block_move_jobs}} AS [[move]] ON [[move]].[[job_id]]=[[job]].[[id]]
			WHERE [[job]].[[status]]='completed'
			AND [[job]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND NOT ` + workspaceActivityQuietCopySQL("[[job]].[[id]]")).Bind(dbx.Params{"ids": string(encoded)}).All(&shown); err != nil {
			return result, err
		}
		for _, job := range shown {
			result.shownJobs[job.ID] = true
			if job.Kind == pageDuplicationJobKind || job.Kind == blockMoveJobKind && job.Operation == blockMoveOperationCopy {
				result.copyJobs[job.ID] = job.Kind
			}
		}
	}
	return result, nil
}

// A moved resource and its linked source require their current page authority.
// Deleted resources use their recorded page, already authorized by the reader.
func workspaceActivityTargetVisible(app core.App, actor string, scope workspaceAuthorization, row storedWorkspaceActivity, targets workspaceActivityTargets, cache map[string]*pageAuthorization) (bool, error) {
	if row.EntityType == "page" {
		page, err := workspaceActivityPage(app, actor, scope, row.EntityID, cache)
		return page != nil, err
	}
	switch row.EntityType {
	case "block", "dataSource", "property", "view":
	default:
		return false, nil
	}
	target, exists := targets.entities[row.EntityType+"\x00"+row.EntityID]
	if !exists {
		return true, nil
	}
	page, err := workspaceActivityPage(app, actor, scope, target.PageID, cache)
	if err != nil || page == nil {
		return false, err
	}
	if sourcePageID := targets.sources[target.SourceID]; sourcePageID != "" {
		page, err = workspaceActivityPage(app, actor, scope, sourcePageID, cache)
		return page != nil, err
	}
	return true, nil
}
