package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Who could open what, carried along when a workspace moves to another
// Leafen server (the decision of 2026-11-05 19:41, "옮길 때 사람·공유를
// 옮길까"): the people paired with accounts there keep their page shares and
// teamspace places, a page shared with every member or with an address
// stays so, and the public links and sites are told to the new server to
// offer turning on again at its address — none goes public by itself. The
// bundle (docs/bundle.md) stays the content alone; this rides the move's
// finish (workspace_move_in.go) and goes in once the pages are in.

// workspaceMoveAccess is what a move carries beside the bundle.
type workspaceMoveAccess struct {
	Permissions []workspaceMovePermission      `json:"permissions,omitempty"`
	Teamspaces  []workspaceMoveTeamspaceMember `json:"teamspaces,omitempty"`
	Links       []workspaceMoveLink            `json:"links,omitempty"`
	Sites       []workspaceMoveSite            `json:"sites,omitempty"`
	// Applied is set once the shares went in, so a job run again skips them.
	Applied bool `json:"applied,omitempty"`
}

// workspaceMovePermission is a page share: to a person (by the old server's
// account), an address not signed up yet, or the workspace's members.
type workspaceMovePermission struct {
	PageID        string `db:"page_id" json:"pageId"`
	PrincipalType string `db:"principal_type" json:"principalType"`
	PrincipalID   string `db:"principal_id" json:"principalId,omitempty"`
	Role          string `db:"role" json:"role"`
}

type workspaceMoveTeamspaceMember struct {
	TeamspaceID string `db:"teamspace_id" json:"teamspaceId"`
	UserID      string `db:"user_id" json:"userId"`
	Role        string `db:"role" json:"role"`
}

// workspaceMoveLink is a page open to anyone with its link on the old server.
type workspaceMoveLink struct {
	PageID    string `db:"page_id" json:"pageId"`
	ExpiresAt string `db:"expires_at" json:"expiresAt,omitempty"`
}

// workspaceMoveSite is a page published as a site on the old server.
type workspaceMoveSite struct {
	PageID string `db:"page_id" json:"pageId"`
	Slug   string `db:"slug" json:"slug"`
}

// collectWorkspaceMoveAccess reads, on the old server, what the move carries.
func collectWorkspaceMoveAccess(app core.App, workspaceID string) (workspaceMoveAccess, error) {
	access := workspaceMoveAccess{}
	if err := app.DB().NewQuery(`
		SELECT p.[[page_id]] AS page_id, p.[[principal_type]] AS principal_type, p.[[principal_id]] AS principal_id, p.[[role]] AS role
		FROM {{leafen_page_permissions}} p JOIN {{leafen_pages}} g ON g.[[id]] = p.[[page_id]]
		WHERE p.[[workspace_id]] = {:workspace} AND g.[[workspace]] = {:workspace}
			AND p.[[principal_type]] IN ('user','email','workspace')
		ORDER BY p.[[page_id]], p.[[principal_type]], p.[[principal_id]]`).
		Bind(dbx.Params{"workspace": workspaceID}).All(&access.Permissions); err != nil {
		return access, err
	}
	if err := app.DB().NewQuery(`
		SELECT [[teamspace_id]] AS teamspace_id, [[principal_id]] AS user_id, [[role]] AS role
		FROM {{leafen_teamspace_members}}
		WHERE [[workspace_id]] = {:workspace} AND [[principal_type]] = 'user' AND [[deleted_at]] = 0
		ORDER BY [[teamspace_id]], [[principal_id]]`).
		Bind(dbx.Params{"workspace": workspaceID}).All(&access.Teamspaces); err != nil {
		return access, err
	}
	if err := app.DB().NewQuery(`
		SELECT [[root_page_id]] AS page_id, [[expires_at]] AS expires_at FROM {{leafen_public_share_links}}
		WHERE [[workspace_id]] = {:workspace} AND [[web_enabled]] = 1 ORDER BY [[root_page_id]]`).
		Bind(dbx.Params{"workspace": workspaceID}).All(&access.Links); err != nil {
		return access, err
	}
	if err := app.DB().NewQuery(`
		SELECT [[root_page_id]] AS page_id, [[slug]] AS slug FROM {{leafen_public_sites}}
		WHERE [[workspace_id]] = {:workspace} AND [[published]] = 1 ORDER BY [[root_page_id]]`).
		Bind(dbx.Params{"workspace": workspaceID}).All(&access.Sites); err != nil {
		return access, err
	}
	return access, nil
}

// applyWorkspaceMoveAccess puts the carried shares in on the new server: a
// person's only when they were paired with an account here, an address's and
// the members' as they were, on the pages that came in.
func applyWorkspaceMoveAccess(app core.App, workspaceID, ownerID string, access workspaceMoveAccess,
	people []workspaceImportPerson) error {
	paired := map[string]string{}
	for _, person := range people {
		if person.TargetID != "" && person.PairOf == "" {
			paired[person.SourceID] = person.TargetID
		}
	}
	now := time.Now().UTC().UnixMilli()
	for _, share := range access.Permissions {
		principal := share.PrincipalID
		switch share.PrincipalType {
		case "user":
			principal = paired[share.PrincipalID]
		case "workspace":
			principal = workspaceID
		case "email":
		default:
			continue
		}
		if principal == "" || principal == ownerID && share.PrincipalType == "user" {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_permissions}}
				([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],[[created_by]],[[created_at]],[[updated_at]])
			SELECT {:id},{:workspace},[[id]],{:type},{:principal},{:role},{:owner},{:now},{:now}
			FROM {{leafen_pages}} WHERE [[id]] = {:page} AND [[workspace]] = {:workspace}
			ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO NOTHING`).
			Bind(dbx.Params{"id": security.RandomString(15), "workspace": workspaceID, "page": share.PageID,
				"type": share.PrincipalType, "principal": principal, "role": share.Role, "owner": ownerID, "now": now}).
			Execute(); err != nil {
			return err
		}
	}
	for _, place := range access.Teamspaces {
		user := paired[place.UserID]
		if user == "" {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_teamspace_members}}
				([[id]],[[workspace_id]],[[teamspace_id]],[[principal_type]],[[principal_id]],[[workspace_member_id]],
				 [[role]],[[revision]],[[deleted_at]],[[created_by]],[[created_at]],[[updated_at]])
			SELECT {:id},{:workspace},[[id]],'user',{:user},{:user},{:role},1,0,{:owner},{:now},{:now}
			FROM {{leafen_teamspaces}} WHERE [[id]] = {:teamspace} AND [[workspace_id]] = {:workspace}
			ON CONFLICT ([[teamspace_id]],[[principal_type]],[[principal_id]]) DO NOTHING`).
			Bind(dbx.Params{"id": security.RandomString(15), "workspace": workspaceID, "teamspace": place.TeamspaceID,
				"user": user, "role": place.Role, "owner": ownerID, "now": now}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// applyWorkspaceMoveAccessTurn puts the carried shares in once the moved
// workspace's pages and history are in, and notes it in the job.
func (r *durableJobRunner) applyWorkspaceMoveAccessTurn(job storedJob, payload workspaceImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if err := applyWorkspaceMoveAccess(txApp, current.WorkspaceID, current.Owner, *payload.MoveAccess, payload.People); err != nil {
			return err
		}
		applied := *payload.MoveAccess
		applied.Applied = true
		payload.MoveAccess = &applied
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "payload": string(encoded),
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
}

// workspaceMovedSummary is what the new server offers once a moved workspace
// is in: the people who weren't paired with an account here, to invite, and
// the pages that were public there, to turn on again here.
type workspaceMovedSummary struct {
	People []workspaceMovedPerson `json:"people"`
	Links  []workspaceMovedPage   `json:"links"`
	Sites  []workspaceMovedPage   `json:"sites"`
}

type workspaceMovedPerson struct {
	Name  string `db:"name" json:"name"`
	Email string `db:"email" json:"email"`
}

type workspaceMovedPage struct {
	PageID string `json:"pageId"`
	Title  string `json:"title"`
}

// readWorkspaceMovedSummary reads it from the people the move kept and the
// last move's finished job; nil when the workspace didn't move here.
func readWorkspaceMovedSummary(app core.App, workspaceID string) *workspaceMovedSummary {
	summary := workspaceMovedSummary{People: []workspaceMovedPerson{}, Links: []workspaceMovedPage{}, Sites: []workspaceMovedPage{}}
	_ = app.DB().NewQuery(`
		SELECT m.[[name]] AS name, m.[[email]] AS email FROM {{leafen_workspace_moved_people}} m
		WHERE m.[[workspace_id]] = {:workspace} AND m.[[email]] != ''
			AND NOT EXISTS (SELECT 1 FROM {{users}} u WHERE lower(u.[[email]]) = m.[[email]]
				AND EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} w WHERE w.[[workspace_id]] = {:workspace} AND w.[[user_id]] = u.[[id]]))
		ORDER BY m.[[name]], m.[[email]]`).Bind(dbx.Params{"workspace": workspaceID}).All(&summary.People)
	var raw string
	if err := app.DB().NewQuery(`
		SELECT [[payload_json]] FROM {{leafen_jobs}}
		WHERE [[workspace_id]] = {:workspace} AND [[kind]] = {:kind} AND [[request_id]] LIKE 'workspace-move:%' AND [[status]] = 'completed'
		ORDER BY [[updated]] DESC LIMIT 1`).Bind(dbx.Params{"workspace": workspaceID, "kind": workspaceImportJobKind}).Row(&raw); err != nil {
		if len(summary.People) == 0 {
			return nil
		}
		return &summary
	}
	payload := workspaceImportJobPayload{}
	if json.Unmarshal([]byte(raw), &payload) == nil && payload.MoveAccess != nil {
		title := func(pageID string) (string, bool) {
			var value string
			err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[workspace]] = {:workspace}`).
				Bind(dbx.Params{"id": pageID, "workspace": workspaceID}).Row(&value)
			return value, err == nil
		}
		on := func(table, pageID, live string) bool {
			var count int
			_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{` + table + `}} WHERE [[root_page_id]] = {:id} AND [[workspace_id]] = {:workspace} AND ` + live).
				Bind(dbx.Params{"id": pageID, "workspace": workspaceID}).Row(&count)
			return count > 0
		}
		for _, link := range payload.MoveAccess.Links {
			if value, ok := title(link.PageID); ok && !on("leafen_public_share_links", link.PageID, "[[web_enabled]] = 1") {
				summary.Links = append(summary.Links, workspaceMovedPage{PageID: link.PageID, Title: value})
			}
		}
		for _, site := range payload.MoveAccess.Sites {
			if value, ok := title(site.PageID); ok && !on("leafen_public_sites", site.PageID, "[[published]] = 1") {
				summary.Sites = append(summary.Sites, workspaceMovedPage{PageID: site.PageID, Title: value})
			}
		}
	}
	if len(summary.People)+len(summary.Links)+len(summary.Sites) == 0 {
		return nil
	}
	return &summary
}
