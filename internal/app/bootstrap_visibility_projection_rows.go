package app

import (
	"context"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type bootstrapWorkspaceVisibilityProjectionRow struct {
	PrincipalID              string `db:"principal_id"`
	WorkspaceID              string `db:"workspace_id"`
	WorkspaceCreated         string `db:"workspace_created"`
	OrganizationMembershipID string `db:"organization_membership_id"`
	WorkspaceMembershipID    string `db:"workspace_membership_id"`
	Role                     string `db:"role"`
}

func insertBootstrapWorkspaceVisibilityRows(
	ctx context.Context,
	app core.App,
	job bootstrapVisibilityProjectionJob,
) (int, error) {
	predicate, params, err := bootstrapVisibilityProjectionScope(job)
	if err != nil {
		return 0, err
	}
	params["cursorPrincipal"] = job.CursorPrincipalID
	params["cursorWorkspace"] = job.CursorWorkspaceID
	params["limit"] = bootstrapVisibilityProjectionWindow
	rows := []bootstrapWorkspaceVisibilityProjectionRow{}
	if err := app.DB().NewQuery(`
		SELECT [[principal_id]],[[workspace_id]],[[workspace_created]],
			[[organization_membership_id]],[[workspace_membership_id]],[[role]]
		FROM [[leafen_bootstrap_workspace_visibility_source]]
		WHERE (` + predicate + `) AND (
			[[principal_id]]>{:cursorPrincipal} OR
			([[principal_id]]={:cursorPrincipal} AND [[workspace_id]]>{:cursorWorkspace}))
		ORDER BY [[principal_id]],[[workspace_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	mutations, err := writeBootstrapWorkspaceVisibilityRows(ctx, app, rows)
	if err != nil {
		return 0, err
	}
	if len(rows) < bootstrapVisibilityProjectionWindow {
		err = completeBootstrapVisibilityProjectionJob(ctx, app, job)
	} else {
		last := rows[len(rows)-1]
		err = moveBootstrapVisibilityProjectionJob(
			ctx, app, job, bootstrapVisibilityProjectionInsertWorkspace,
			last.PrincipalID, last.WorkspaceID, "",
		)
	}
	return mutations, err
}

func writeBootstrapWorkspaceVisibilityRows(
	ctx context.Context,
	app core.App,
	rows []bootstrapWorkspaceVisibilityProjectionRow,
) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	params := dbx.Params{}
	values := make([]string, 0, len(rows))
	for index, row := range rows {
		prefix := fmt.Sprintf("w%d", index)
		params[prefix+"p"] = row.PrincipalID
		params[prefix+"w"] = row.WorkspaceID
		params[prefix+"c"] = row.WorkspaceCreated
		params[prefix+"o"] = row.OrganizationMembershipID
		params[prefix+"m"] = row.WorkspaceMembershipID
		params[prefix+"r"] = row.Role
		values = append(values, fmt.Sprintf(
			"({:%sp},{:%sw},{:%sc},{:%so},{:%sm},{:%sr})",
			prefix, prefix, prefix, prefix, prefix, prefix,
		))
	}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_bootstrap_workspace_visibility}}
			([[principal_id]],[[workspace_id]],[[workspace_created]],[[organization_membership_id]],
			 [[workspace_membership_id]],[[role]])
		VALUES ` + joinSQL(values) + `
		ON CONFLICT ([[principal_id]],[[workspace_id]]) DO UPDATE SET
			[[workspace_created]]=excluded.[[workspace_created]],
			[[organization_membership_id]]=excluded.[[organization_membership_id]],
			[[workspace_membership_id]]=excluded.[[workspace_membership_id]],
			[[role]]=excluded.[[role]]
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}
