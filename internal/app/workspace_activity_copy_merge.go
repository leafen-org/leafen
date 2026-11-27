package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copy of blocks into a page — a button's blocks, a template's body, a
// paste that copies — is part of the person's edit of that page, as Notion's
// updates list it (seen 2026-11-17 by worker w1632 in two tabs, first
// account, test workspace: a button pressed a minute after its blocks were
// typed lists them, then the blocks it put in, under one "〈page〉 페이지를
// 편집했습니다"; one pressed eight minutes after is a line of its own, which
// a line typed a minute later joins — "e one, e two, e typed"). The copy
// writes its blocks with no versions of the page's history, so the edit
// keeps the copies it holds (CopyJobs, recordWorkspaceCopiedBlock) and lists
// the blocks they made as the page holds them now — one since deleted is
// gone — each copy's in the page's order, among what the edit's versions
// changed by when each was made.

// workspaceActivityTimedChange is a change of an edit and when it was made.
type workspaceActivityTimedChange struct {
	change workspaceActivityChange
	at     int64
}

// workspaceActivityChangesOf lists a page edit's changes: what its versions
// changed and the blocks its copies made, in the order they were made, those
// past workspaceActivityChangeItems counted.
func workspaceActivityChangesOf(app core.App, actor string, scope workspaceAuthorization, pageID string, row storedWorkspaceActivity,
	cache map[string]*pageAuthorization) ([]workspaceActivityChange, int, error) {
	var metadata workspaceActivityMetadata
	_ = json.Unmarshal([]byte(row.Metadata), &metadata)
	copied, more, err := workspaceActivityCopiedLines(app, actor, scope, pageID, metadata.CopyJobs, cache)
	if err != nil {
		return nil, 0, err
	}
	// Its versions list no block its copies made: those are its copies' lines.
	skip := func(blocks []string) (map[string]bool, error) {
		return workspaceActivityCopiedAmong(app, metadata.CopyJobs, blocks)
	}
	edited, editedMore, err := workspaceActivityVersionChanges(app, actor, scope, pageID, row, cache, skip)
	if err != nil {
		return nil, 0, err
	}
	changes := []workspaceActivityChange{}
	add := func(item workspaceActivityTimedChange) {
		if len(changes) == workspaceActivityChangeItems {
			more++
			return
		}
		changes = append(changes, workspaceActivityClipped(item.change))
	}
	// Each list is in the order it was made; a copy goes before what was
	// changed after it began.
	next := 0
	for _, item := range edited {
		for ; next < len(copied) && copied[next].at < item.at; next++ {
			add(copied[next])
		}
		add(item)
	}
	for ; next < len(copied); next++ {
		add(copied[next])
	}
	return changes, more + editedMore, nil
}

// workspaceActivityBlockCopyChanges lists the edit a copy of blocks made of
// the page it copied them into as a group of its own, as copies wrote it
// before a person's edit held them: the blocks it made.
func workspaceActivityBlockCopyChanges(app core.App, actor string, scope workspaceAuthorization, pageID string, row storedWorkspaceActivity,
	cache map[string]*pageAuthorization) ([]workspaceActivityChange, int, error) {
	var metadata workspaceActivityMetadata
	_ = json.Unmarshal([]byte(row.Metadata), &metadata)
	copied, more, err := workspaceActivityCopiedLines(app, actor, scope, pageID, []string{metadata.OperationJobID}, cache)
	if err != nil {
		return nil, 0, err
	}
	changes := []workspaceActivityChange{}
	for _, item := range copied {
		if len(changes) == workspaceActivityChangeItems {
			more++
			continue
		}
		changes = append(changes, workspaceActivityClipped(item.change))
	}
	return changes, more, nil
}

// workspaceActivityCopiedLines is the lines of the blocks copies made in a
// page, each copy's in its order (workspaceActivityCopyOrder) at when it
// began, copies in the order they began. Only the first
// workspaceActivityChangeBlocks blocks made are read; those past them are
// counted, not read, as an edit's versions count theirs
// (workspaceActivityVersionChanges) — a copy of thousands of blocks reads 64
// of them.
func workspaceActivityCopiedLines(app core.App, actor string, scope workspaceAuthorization, pageID string, jobs []string,
	cache map[string]*pageAuthorization) ([]workspaceActivityTimedChange, int, error) {
	lines := []workspaceActivityTimedChange{}
	if len(jobs) == 0 {
		return lines, 0, nil
	}
	encoded, err := json.Marshal(jobs)
	if err != nil {
		return nil, 0, err
	}
	params := dbx.Params{"pageId": pageID, "jobs": string(encoded), "limit": workspaceActivityChangeBlocks}
	stored := []struct {
		storedPageHistoryBlockState
		JobID string `db:"job_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block]].[[id]],[[block]].[[parent_id]],[[block]].[[position]],[[block]].[[type]],[[block]].[[content]],
			[[block]].[[created]],[[block]].[[updated]],[[item]].[[job_id]]
		FROM {{leafen_block_move_items}} AS [[item]]
		CROSS JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[item]].[[target_block_id]] AND [[block]].[[page_id]]={:pageId}
		WHERE [[item]].[[job_id]] IN (SELECT [[value]] FROM json_each({:jobs})) AND [[item]].[[moved]]=1
		ORDER BY [[block]].[[created]],[[block]].[[id]]
		LIMIT {:limit}
	`).Bind(params).All(&stored); err != nil {
		return nil, 0, err
	}
	if len(stored) == 0 {
		return lines, 0, nil
	}
	// Those past the blocks read are counted. Both read from the copies'
	// items (CROSS JOIN), so what they cost is the copies', not the page's.
	more := 0
	if len(stored) == workspaceActivityChangeBlocks {
		total := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_block_move_items}} AS [[item]]
			CROSS JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[item]].[[target_block_id]] AND [[block]].[[page_id]]={:pageId}
			WHERE [[item]].[[job_id]] IN (SELECT [[value]] FROM json_each({:jobs})) AND [[item]].[[moved]]=1
		`).Bind(params).Row(&total); err != nil {
			return nil, 0, err
		}
		more = max(0, total-len(stored))
	}
	document, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return nil, 0, err
	}
	// Each copy's blocks, at when its first was made.
	began, byJob, order := map[string]int64{}, map[string][]pageHistoryBlockState{}, []string{}
	for _, block := range stored {
		if _, seen := began[block.JobID]; !seen {
			began[block.JobID] = block.CreatedAt
			order = append(order, block.JobID)
		}
		byJob[block.JobID] = append(byJob[block.JobID], pageHistoryBlockState{ID: block.ID, ParentID: block.ParentID,
			Position: block.Position, Type: block.Type, Content: json.RawMessage(block.Content), CreatedAt: block.CreatedAt})
	}
	sort.SliceStable(order, func(left, right int) bool { return began[order[left]] < began[order[right]] })
	for _, job := range order {
		changes, past, err := workspaceActivityMadeChanges(app, actor, scope, byJob[job], document, []workspaceActivityChange{}, cache)
		if err != nil {
			return nil, 0, err
		}
		more += past
		for _, change := range changes {
			lines = append(lines, workspaceActivityTimedChange{change: change, at: began[job]})
		}
	}
	return lines, more, nil
}

// workspaceActivityCopiedAmong is which of blocks the copies made, each
// block looked up in each copy (idx_leafen_block_move_target).
func workspaceActivityCopiedAmong(app core.App, jobs, blocks []string) (map[string]bool, error) {
	copied := map[string]bool{}
	if len(jobs) == 0 || len(blocks) == 0 {
		return copied, nil
	}
	encodedJobs, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	encodedBlocks, err := json.Marshal(blocks)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[wanted]].[[value]] FROM json_each({:blocks}) AS [[wanted]]
		CROSS JOIN {{leafen_block_move_items}} AS [[item]]
		WHERE [[item]].[[job_id]] IN (SELECT [[value]] FROM json_each({:jobs}))
			AND [[item]].[[target_block_id]]=[[wanted]].[[value]] AND [[item]].[[moved]]=1
	`).Bind(dbx.Params{"jobs": string(encodedJobs), "blocks": string(encodedBlocks)}).Column(&ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		copied[id] = true
	}
	return copied, nil
}

// workspaceActivityCopyJobsKept is a group's metadata as chosen, keeping
// every copy the group held before and the entry holds, in the order they
// came, each once. None is dropped: a copy left out would drop its blocks
// from the edit unsaid, uncounted in its "외 N" — the blocks read are
// bounded, those past it counted, when the edit is read
// (workspaceActivityCopiedLines).
func workspaceActivityCopyJobsKept(chosen string) string {
	return `CASE WHEN json_extract([[metadata_json]], '$.copyJobs') IS NULL AND json_extract(excluded.[[metadata_json]], '$.copyJobs') IS NULL
		THEN ` + chosen + `
		ELSE json_set(` + chosen + `, '$.copyJobs', json((SELECT json_group_array([[value]]) FROM (
			SELECT [[value]] FROM (
				SELECT [[value]], 0 AS [[part]], [[key]] AS [[at]] FROM json_each([[metadata_json]], '$.copyJobs')
				UNION ALL
				SELECT [[value]], 1, [[key]] FROM json_each(excluded.[[metadata_json]], '$.copyJobs')
				WHERE [[value]] NOT IN (SELECT [[value]] FROM json_each([[metadata_json]], '$.copyJobs'))
			) ORDER BY [[part]], [[at]])))) END`
}
