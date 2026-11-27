package app

import (
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a person's profile lists of their work, as Notion's (seen
// 2026-10-12): "최근 활동" — the pages they made and edited lately, newest
// first; "〈이름〉 님의 페이지" — the pages they made, last edited first; and
// "최고의 공동 작업자" — the people who edited the same pages most. Each
// reads only pages the reader may open now, so a profile shows nobody a
// page, nor a collaboration, they couldn't see.

const (
	personActivityEventWindow   = 300
	personActivityPageLimit     = 60
	personActivityLimit         = 100
	personPageCandidateLimit    = 120
	personPageLimit             = 30
	personCollaboratorLimit     = 5
	personCollaborationWindowMS = int64(90 * 24 * time.Hour / time.Millisecond)
)

type personActivity struct {
	PageID string `json:"pageId" db:"page_id"`
	Kind   string `json:"kind" db:"kind"`
	At     int64  `json:"at" db:"occurred_at"`
	personPageLabel
}

type personPage struct {
	PageID   string `json:"pageId" db:"id"`
	EditedAt string `json:"editedAt" db:"updated"`
	personPageLabel
}

type personPageLabel struct {
	Title    string `json:"title" db:"title"`
	Icon     string `json:"icon" db:"icon"`
	IconType string `json:"iconType" db:"icon_type"`
	PageKind string `json:"pageKind" db:"page_kind"`
	// ViewBlock is a full-page database that is a view block, called
	// "제목 없음" with no title, and SourceTitles an untitled one's sources
	// while it holds several, "A 및 B" (page_name_kind.go).
	ViewBlock    bool                  `json:"viewBlock,omitempty" db:"view_block"`
	SourceTitles []databaseSourceTitle `json:"sourceTitles,omitempty" db:"-"`
}

type storedPersonPageLabel struct {
	ID string `db:"id"`
	personPageLabel
}

// loadPersonWork reads the person's pages, their recent activity when the
// workspace shows it, and the people they work with most.
func loadPersonWork(app core.App, authorization workspaceAuthorization, personID string, withActivity bool) (
	[]personPage, []personActivity, []string, error,
) {
	params := dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID}
	events := []personActivity{}
	// The pages they made and edited, and those they commented on — a database
	// page's comments are its row's — newest first.
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[kind]], [[occurred_at]] FROM (
			SELECT [[page_id]], CASE [[interaction_type]] WHEN 'page.created' THEN 'created' ELSE 'edited' END AS [[kind]],
				[[occurred_at]]
			FROM {{leafen_page_interactions}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:personId}
				AND [[interaction_type]] IN ('page.created', 'page.edited')
			ORDER BY [[occurred_at]] DESC LIMIT {:limit})
		UNION ALL
		SELECT [[page_id]], [[kind]], [[occurred_at]] FROM (
			SELECT CASE [[target_kind]] WHEN 'database_row' THEN [[target_id]] ELSE [[page_id]] END AS [[page_id]],
				'commented' AS [[kind]], CAST((julianday([[created_at]]) - 2440587.5) * 86400000 AS INTEGER) AS [[occurred_at]]
			FROM {{leafen_comments}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[author_id]] = {:personId} AND [[target_kind]] IN ('page', 'database_row')
			ORDER BY [[created_at]] DESC LIMIT {:limit})
		ORDER BY [[occurred_at]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID,
		"limit": personActivityEventWindow}).All(&events); err != nil {
		return nil, nil, nil, err
	}
	pages := []personPage{}
	// The person's pages are read by their maker in the workspace, the trash,
	// import and row marks after the row (+): SQLite, which has no statistics,
	// took the (workspace, import) index and read the workspace's every page.
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[updated]], [[title]], [[icon]], [[icon_type]], [[kind]] AS [[page_kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]]
		FROM {{leafen_pages}} [[page]]
		WHERE [[workspace]] = {:workspaceId} AND [[created_by]] = {:personId} AND +[[in_trash]] = 0
			AND [[deletion_job_id]] = '' AND +[[notion_import_staging]] = 0 AND COALESCE([[archived_at]], '') = ''
			AND +[[data_source_id]] = '' AND [[database_template]] = 0
		ORDER BY [[updated]] DESC, [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID,
		"limit": personPageCandidateLimit}).All(&pages); err != nil {
		return nil, nil, nil, err
	}

	// Which of the pages the reader may open, judged once each.
	candidates := []string{}
	seen := map[string]bool{}
	for _, event := range events {
		if !seen[event.PageID] && len(seen) < personActivityPageLimit {
			seen[event.PageID] = true
			candidates = append(candidates, event.PageID)
		}
	}
	for _, page := range pages {
		if !seen[page.PageID] {
			seen[page.PageID] = true
			candidates = append(candidates, page.PageID)
		}
	}
	readable, err := readablePageIDs(app, authorization.PrincipalID, candidates)
	if err != nil {
		return nil, nil, nil, err
	}

	shownPages := make([]personPage, 0, personPageLimit)
	for _, page := range pages {
		if readable[page.PageID] && len(shownPages) < personPageLimit {
			shownPages = append(shownPages, page)
		}
	}
	activity := make([]personActivity, 0, personActivityLimit)
	worked := []string{}
	for _, event := range events {
		if !readable[event.PageID] {
			continue
		}
		// The people they work with most are those who edit the same pages.
		if event.Kind != "commented" && !slices.Contains(worked, event.PageID) {
			worked = append(worked, event.PageID)
		}
		if withActivity && len(activity) < personActivityLimit {
			activity = append(activity, event)
		}
	}
	if err := labelPersonActivity(app, activity); err != nil {
		return nil, nil, nil, err
	}
	labels := make(map[string][]*personPageLabel)
	for index := range shownPages {
		labels[shownPages[index].PageID] = append(labels[shownPages[index].PageID], &shownPages[index].personPageLabel)
	}
	for index := range activity {
		labels[activity[index].PageID] = append(labels[activity[index].PageID], &activity[index].personPageLabel)
	}
	if err := namePersonPagesBySources(app, labels); err != nil {
		return nil, nil, nil, err
	}
	params["since"] = time.Now().UnixMilli() - personCollaborationWindowMS
	collaborators, err := loadPersonCollaborators(app, params, worked)
	if err != nil {
		return nil, nil, nil, err
	}
	return shownPages, activity, collaborators, nil
}

// labelPersonActivity names each page of the activity as it is now.
func labelPersonActivity(app core.App, activity []personActivity) error {
	if len(activity) == 0 {
		return nil
	}
	ids := []string{}
	for _, event := range activity {
		if !slices.Contains(ids, event.PageID) {
			ids = append(ids, event.PageID)
		}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	labels := []storedPersonPageLabel{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[title]], [[icon]], [[icon_type]], [[kind]] AS [[page_kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]]
		FROM {{leafen_pages}} [[page]] WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&labels); err != nil {
		return err
	}
	byID := make(map[string]personPageLabel, len(labels))
	for _, label := range labels {
		byID[label.ID] = label.personPageLabel
	}
	for index := range activity {
		activity[index].personPageLabel = byID[activity[index].PageID]
	}
	return nil
}

// namePersonPagesBySources gives the untitled full-page databases of several
// sources among the person's pages and activity their sources' titles, which
// name them as everywhere (full_page_database_sources_name.go).
func namePersonPagesBySources(app core.App, labels map[string][]*personPageLabel) error {
	ids := []string{}
	for id, named := range labels {
		if named[0].PageKind == pageKindDatabase && !named[0].ViewBlock && named[0].Title == "" {
			ids = append(ids, id)
		}
	}
	titles, err := untitledFullPageDatabaseSourceTitles(app, ids)
	for id, sources := range titles {
		for _, label := range labels[id] {
			label.SourceTitles = sources
		}
	}
	return err
}

// loadPersonCollaborators ranks the workspace's other people by how often
// they made or edited, in the last 90 days, the pages the person worked on
// that the reader may open.
func loadPersonCollaborators(app core.App, params dbx.Params, pageIDs []string) ([]string, error) {
	collaborators := []string{}
	if len(pageIDs) == 0 {
		return collaborators, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	params["pageIds"] = string(encoded)
	rows := []struct {
		UserID string `db:"user_id"`
		Count  int    `db:"edits"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[interaction]].[[user_id]], COUNT(*) AS [[edits]]
		FROM {{leafen_page_interactions}} [[interaction]]
		WHERE [[interaction]].[[workspace_id]] = {:workspaceId}
			AND [[interaction]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pageIds}))
			AND [[interaction]].[[interaction_type]] IN ('page.created', 'page.edited')
			AND [[interaction]].[[occurred_at]] >= {:since} AND [[interaction]].[[user_id]] != {:personId}
			AND [[interaction]].[[user_id]] IN (SELECT [[user_id]] FROM (` + workspacePeopleSQL() + `
				AND NOT ` + integrationAccountSQL("[[user]].[[id]]") + `) WHERE [[suspended]] = 0)
		GROUP BY [[interaction]].[[user_id]]
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(left, right int) bool {
		if rows[left].Count != rows[right].Count {
			return rows[left].Count > rows[right].Count
		}
		return rows[left].UserID < rows[right].UserID
	})
	for _, row := range rows {
		if len(collaborators) == personCollaboratorLimit {
			break
		}
		collaborators = append(collaborators, row.UserID)
	}
	return collaborators, nil
}
