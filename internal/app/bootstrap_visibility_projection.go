package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	bootstrapVisibilityProjectionWindow           = 256
	bootstrapVisibilityProjectionRequestTurnLimit = 32
	errorCodeBootstrapVisibilityProjectionPending = "LEAFEN_BOOTSTRAP_VISIBILITY_PROJECTION_PENDING"
)

const (
	bootstrapVisibilityProjectionDeletePage = iota
	bootstrapVisibilityProjectionDeleteWorkspace
	bootstrapVisibilityProjectionInsertWorkspace
	bootstrapVisibilityProjectionInsertPage
)

type bootstrapVisibilityProjectionTurn struct {
	Mutations int
	HasMore   bool
}

type bootstrapVisibilityProjectionJob struct {
	Kind              string `db:"kind"`
	AuthorityKey      string `db:"authority_key"`
	PrincipalID       string `db:"principal_id"`
	OrganizationID    string `db:"organization_id"`
	WorkspaceID       string `db:"workspace_id"`
	PageID            string `db:"page_id"`
	MembershipID      string `db:"membership_id"`
	Revision          int64  `db:"revision"`
	Phase             int    `db:"phase"`
	CursorPrincipalID string `db:"cursor_principal"`
	CursorWorkspaceID string `db:"cursor_workspace"`
	CursorRecordID    string `db:"cursor_record"`
}

func advanceBootstrapVisibilityProjection(
	ctx context.Context,
	app core.App,
) (bootstrapVisibilityProjectionTurn, error) {
	turn := bootstrapVisibilityProjectionTurn{}
	if err := ctx.Err(); err != nil {
		return turn, err
	}
	pending, err := hasBootstrapVisibilityProjectionWork(ctx, app)
	if err != nil || !pending {
		return turn, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		job, err := loadBootstrapVisibilityProjectionJob(ctx, txApp)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		mutations, err := advanceBootstrapVisibilityProjectionJob(ctx, txApp, job)
		if err != nil {
			return err
		}
		turn.Mutations = mutations
		pending, err := hasBootstrapVisibilityProjectionWork(ctx, txApp)
		if err != nil {
			return err
		}
		turn.HasMore = pending
		return nil
	})
	return turn, err
}

func hasBootstrapVisibilityProjectionWork(ctx context.Context, app core.App) (bool, error) {
	var pending int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_bootstrap_visibility_projection_jobs}}
			INDEXED BY [[idx_leafen_bootstrap_visibility_projection_jobs_ready]] LIMIT 1
		)
	`).WithContext(ctx).Row(&pending)
	return pending > 0, err
}

// settleBootstrapVisibilityProjectionFor does the jobs touching a person,
// up to a request's share: false while some are left. With no job at all it
// only reads, as the runner's idle turn does: the write connection it would
// take is the one every change waits for, and the list read behind it
// waited there too (2026-11-14, NAS condition: an API write burst held each
// page list read up to 4.9 s).
func settleBootstrapVisibilityProjectionFor(ctx context.Context, app core.App, principalID string) (bool, error) {
	if pending, err := hasBootstrapVisibilityProjectionWork(ctx, app); err != nil || !pending {
		return err == nil, err
	}
	for turnIndex := 0; turnIndex < bootstrapVisibilityProjectionRequestTurnLimit; turnIndex++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		done := false
		err := app.RunInTransaction(func(txApp core.App) error {
			job, err := loadBootstrapVisibilityProjectionJobFor(ctx, txApp, principalID)
			if errors.Is(err, sql.ErrNoRows) {
				done = true
				return nil
			}
			if err != nil {
				return err
			}
			_, err = advanceBootstrapVisibilityProjectionJob(ctx, txApp, job)
			return err
		})
		if err != nil || done {
			return done, err
		}
	}
	return false, nil
}

// loadBootstrapVisibilityProjectionJobFor takes the oldest job touching a
// person: their membership of a workspace, their organization memberships,
// their organizations, and the workspaces they own, belong to or are listed
// in — each found by the jobs' key.
func loadBootstrapVisibilityProjectionJobFor(
	ctx context.Context,
	app core.App,
	principalID string,
) (bootstrapVisibilityProjectionJob, error) {
	// A workspace membership's key is the person's ID, its length first, then
	// the workspace's: the person's keys run from that prefix to the prefix
	// and a byte above any ID's.
	prefix := fmt.Sprintf("%d:%s", len(principalID), principalID)
	job := bootstrapVisibilityProjectionJob{}
	err := app.DB().NewQuery(`
		UPDATE {{leafen_bootstrap_visibility_projection_jobs}}
		SET [[updated_at]]=[[updated_at]]+1
		WHERE ([[kind]],[[authority_key]]) = (
			SELECT [[kind]],[[authority_key]] FROM {{leafen_bootstrap_visibility_projection_jobs}}
			WHERE ([[kind]] = 'workspace_member' AND [[authority_key]] >= {:prefix} AND [[authority_key]] < {:prefixEnd})
				OR ([[kind]] = 'organization_membership' AND [[authority_key]] IN (
					SELECT [[id]] FROM {{leafen_organization_memberships}} WHERE [[user]] = {:principalId}))
				OR ([[kind]] = 'organization' AND [[authority_key]] IN (
					SELECT [[organization]] FROM {{leafen_organization_memberships}} WHERE [[user]] = {:principalId}))
				OR ([[kind]] = 'workspace' AND [[authority_key]] IN (
					SELECT [[workspace_id]] FROM {{leafen_workspace_memberships}} WHERE [[user_id]] = {:principalId}
					UNION SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]] = {:principalId}
					UNION SELECT [[workspace_id]] FROM {{leafen_bootstrap_workspace_visibility}} WHERE [[principal_id]] = {:principalId}))
			ORDER BY [[updated_at]] LIMIT 1
		)
		RETURNING [[kind]],[[authority_key]],[[principal_id]],[[organization_id]],[[workspace_id]],
			[[page_id]],[[membership_id]],[[revision]],[[phase]],[[cursor_principal]],
			[[cursor_workspace]],[[cursor_record]]
	`).WithContext(ctx).Bind(dbx.Params{
		"principalId": principalID, "prefix": prefix, "prefixEnd": prefix + "\x7f",
	}).One(&job)
	return job, err
}

func loadBootstrapVisibilityProjectionJob(
	ctx context.Context,
	app core.App,
) (bootstrapVisibilityProjectionJob, error) {
	job := bootstrapVisibilityProjectionJob{}
	err := app.DB().NewQuery(`
		UPDATE {{leafen_bootstrap_visibility_projection_jobs}}
		SET [[updated_at]]=[[updated_at]]+1
		WHERE ([[kind]],[[authority_key]]) = (
			SELECT [[kind]],[[authority_key]]
			FROM {{leafen_bootstrap_visibility_projection_jobs}}
			INDEXED BY [[idx_leafen_bootstrap_visibility_projection_jobs_ready]]
			ORDER BY [[updated_at]],[[kind]],[[authority_key]] LIMIT 1
		)
		RETURNING [[kind]],[[authority_key]],[[principal_id]],[[organization_id]],[[workspace_id]],
			[[page_id]],[[membership_id]],[[revision]],[[phase]],[[cursor_principal]],
			[[cursor_workspace]],[[cursor_record]]
	`).WithContext(ctx).One(&job)
	return job, err
}

// A job brings a person's workspaces in its scope up to date: the rows
// first let go, then written anew. The per-person page list it once kept
// too is read when asked since migration 613 (bootstrap_page_access.go): a
// page's job, or a phase of pages, has nothing to do.
func advanceBootstrapVisibilityProjectionJob(
	ctx context.Context,
	app core.App,
	job bootstrapVisibilityProjectionJob,
) (int, error) {
	if job.Kind == "page" || job.Phase == bootstrapVisibilityProjectionInsertPage {
		return 0, completeBootstrapVisibilityProjectionJob(ctx, app, job)
	}
	if job.Kind == "organization" {
		return splitBootstrapVisibilityOrganizationJob(ctx, app, job)
	}
	switch job.Phase {
	case bootstrapVisibilityProjectionDeletePage, bootstrapVisibilityProjectionDeleteWorkspace:
		return deleteBootstrapVisibilityProjectionRows(ctx, app, job, bootstrapWorkspaceVisibilityTable)
	case bootstrapVisibilityProjectionInsertWorkspace:
		return insertBootstrapWorkspaceVisibilityRows(ctx, app, job)
	default:
		return 0, errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
}

// splitBootstrapVisibilityOrganizationJob passes an organization's job —
// its owner changed — to a job for each of its workspaces, each read by the
// workspace's own index rather than every member of the organization a
// window at a time.
func splitBootstrapVisibilityOrganizationJob(
	ctx context.Context,
	app core.App,
	job bootstrapVisibilityProjectionJob,
) (int, error) {
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_bootstrap_visibility_projection_jobs}}
			([[kind]],[[authority_key]],[[workspace_id]],[[revision]],[[updated_at]])
		SELECT 'workspace',[[id]],[[id]],1,unixepoch('subsec')*1000
		FROM {{leafen_workspaces}} WHERE [[organization]] = {:organizationId}
		ON CONFLICT ([[kind]],[[authority_key]]) DO UPDATE SET
			[[workspace_id]]=excluded.[[workspace_id]], [[revision]]=[[revision]]+1,
			[[phase]]=0, [[cursor_principal]]='', [[cursor_workspace]]='',
			[[cursor_record]]='', [[updated_at]]=excluded.[[updated_at]]
	`).WithContext(ctx).Bind(dbx.Params{"organizationId": job.OrganizationID}).Execute()
	if err != nil {
		return 0, err
	}
	queued, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(queued), completeBootstrapVisibilityProjectionJob(ctx, app, job)
}

func deleteBootstrapVisibilityProjectionRows(
	ctx context.Context,
	app core.App,
	job bootstrapVisibilityProjectionJob,
	table string,
) (int, error) {
	predicate, params, err := bootstrapVisibilityProjectionScope(job)
	if err != nil {
		return 0, err
	}
	query := `DELETE FROM ` + app.DB().QuoteSimpleTableName(table) + ` WHERE rowid IN (
		SELECT rowid FROM ` + app.DB().QuoteSimpleTableName(table) + ` WHERE ` + predicate + `
		ORDER BY rowid LIMIT {:limit})`
	params["limit"] = bootstrapVisibilityProjectionWindow
	result, err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	mutations, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if mutations < bootstrapVisibilityProjectionWindow {
		if err := moveBootstrapVisibilityProjectionJob(ctx, app, job, bootstrapVisibilityProjectionInsertWorkspace, "", "", ""); err != nil {
			return 0, err
		}
	}
	return int(mutations), nil
}

func bootstrapVisibilityProjectionScope(
	job bootstrapVisibilityProjectionJob,
) (string, dbx.Params, error) {
	params := dbx.Params{
		"principalId": job.PrincipalID, "organizationId": job.OrganizationID,
		"workspaceId": job.WorkspaceID, "pageId": job.PageID,
		"membershipId": job.MembershipID,
	}
	switch job.Kind {
	case "workspace":
		return "[[workspace_id]]={:workspaceId}", params, nil
	case "page":
		return "[[page_id]]={:pageId}", params, nil
	case "workspace_member":
		return "[[principal_id]]={:principalId} AND [[workspace_id]]={:workspaceId}", params, nil
	case "organization_membership":
		return "[[organization_membership_id]]={:membershipId}", params, nil
	case "organization":
		return `[[organization_membership_id]] IN (
			SELECT [[id]] FROM {{leafen_organization_memberships}}
			WHERE [[organization]]={:organizationId})`, params, nil
	default:
		return "", nil, fmt.Errorf("%s: unknown projection kind %q", errorCodeSchemaMigrationHistoryInvalid, job.Kind)
	}
}

func moveBootstrapVisibilityProjectionJob(
	ctx context.Context,
	app core.App,
	job bootstrapVisibilityProjectionJob,
	phase int,
	principalID string,
	workspaceID string,
	recordID string,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_bootstrap_visibility_projection_jobs}}
		SET [[phase]]={:phase},[[cursor_principal]]={:principalId},
			[[cursor_workspace]]={:workspaceId},[[cursor_record]]={:recordId},
			[[updated_at]]=[[updated_at]]+1
		WHERE [[kind]]={:kind} AND [[authority_key]]={:authorityKey} AND [[revision]]={:revision}
	`).WithContext(ctx).Bind(dbx.Params{
		"phase": phase, "principalId": principalID, "workspaceId": workspaceID,
		"recordId": recordID, "kind": job.Kind, "authorityKey": job.AuthorityKey,
		"revision": job.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

func completeBootstrapVisibilityProjectionJob(
	ctx context.Context,
	app core.App,
	job bootstrapVisibilityProjectionJob,
) error {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_bootstrap_visibility_projection_jobs}}
		WHERE [[kind]]={:kind} AND [[authority_key]]={:authorityKey} AND [[revision]]={:revision}
	`).WithContext(ctx).Bind(dbx.Params{
		"kind": job.Kind, "authorityKey": job.AuthorityKey, "revision": job.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
