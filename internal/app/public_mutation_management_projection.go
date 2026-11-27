package app

import (
	"context"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedPublicMutationAccessOwner struct {
	PrincipalID string `db:"principal_id"`
	Label       string `db:"label"`
	Email       string `db:"email"`
}

func populatePublicMutationPrincipalDirectory(
	ctx context.Context,
	app core.App,
	authorization pageAuthorization,
	principalCursor string,
	includePrincipals bool,
	state *publicMutationState,
) error {
	ancestorIDs, err := loadPageAccessAncestorIDs(app, state.WorkspaceID, state.PageID)
	if err != nil {
		return err
	}
	holderID, private, err := publicMutationFullAccessHolder(ctx, app, authorization, ancestorIDs)
	if err != nil {
		return err
	}
	owner, err := loadPublicMutationAccessOwner(ctx, app, holderID)
	if err != nil {
		return err
	}
	state.AccessOwner = publicMutationAccessOwner{
		PrincipalID: owner.PrincipalID, Label: owner.Label, Email: owner.Email,
		IsCurrent: owner.PrincipalID == authorization.Workspace.PrincipalID, PrivatePage: private,
	}
	if !includePrincipals {
		return nil
	}
	authority, err := readWorkspacePrincipalDirectoryAuthority(ctx, app, state.WorkspaceID)
	if err != nil {
		return err
	}
	cursor, err := decodePublicMutationPrincipalCursor(
		principalCursor, authorization.Workspace.PrincipalID, state.WorkspaceID, state.PageID,
		state.Revision, authority,
	)
	if err != nil {
		return err
	}
	// A guest who manages a page is offered no one and reaches others by their
	// address, as Notion's (seen 2026-11-05).
	_, guest, err := publicMutationReaderSeesPeople(app, authorization)
	if err != nil || guest {
		return err
	}
	options, err := loadWorkspacePrincipalDirectory(
		ctx, app, state.WorkspaceID, authorization.Workspace.PrincipalID, cursor.ID, publicMutationPrincipalWindow+1, true,
	)
	if err != nil {
		if errors.Is(err, errWorkspacePrincipalDirectoryInvalid) {
			return errPublicMutationCursor
		}
		return err
	}
	hasMore := len(options) > publicMutationPrincipalWindow
	if hasMore {
		options = options[:publicMutationPrincipalWindow]
	}
	restricted, err := workspaceRestrictedMembers(app, state.WorkspaceID)
	if err != nil {
		return err
	}
	for _, option := range options {
		state.PrincipalOptions = append(state.PrincipalOptions, publicMutationPrincipalOption{
			PrincipalType: option.PrincipalType, PrincipalID: option.PrincipalID, Label: option.Label,
			Email: option.Email, Guest: option.Guest, WorkspaceOwner: option.WorkspaceOwner,
			Restricted: option.PrincipalType == publicMutationPrincipalUser && restricted[option.PrincipalID],
		})
	}
	if !hasMore {
		return nil
	}
	last := options[len(options)-1]
	next, err := encodePublicMutationPrincipalCursor(publicMutationPrincipalCursor{
		Version:     publicMutationPrincipalCursorVersion,
		PrincipalID: authorization.Workspace.PrincipalID,
		WorkspaceID: state.WorkspaceID, PageID: state.PageID,
		MutationRevision: state.Revision, PermissionRevision: authority.PermissionRevision,
		PersonRevision: authority.PersonRevision, GroupRevision: authority.GroupRevision,
		ID: last.PrincipalType + ":" + last.PrincipalID,
	})
	if err != nil {
		return err
	}
	state.PrincipalOptionsNextCursor = &next
	return nil
}

// publicMutationFullAccessHolder is who holds a page's full access, shown
// first in its share and left out of the people it lists: a private page's
// owner, else the workspace's owner. The workspace's owner can't open a
// member's private page, so they are offered there like anyone (2026-10-29:
// the row named the workspace's owner, who then couldn't be shared with).
// People's pages, private to the server's own author (people_database.go),
// are no person's: the workspace's owner holds them, as Notion's person
// page names "<workspace>의 소유자 — 전체 허용" (seen 2026-11-16) — named
// the server's author, the share of a person's page read no one and was
// refused, so their profile's share button said "shared with people".
func publicMutationFullAccessHolder(
	ctx context.Context, app core.App, authorization pageAuthorization, ancestorIDs []string,
) (holderID string, private bool, err error) {
	owner, teamspace, err := publicMutationPagePlace(ctx, app, authorization, authorization.Workspace.WorkspaceID, ancestorIDs)
	if err != nil {
		return "", false, err
	}
	if teamspace == "" && owner != "" && owner != leafenSystemAuthorID {
		return owner, true, nil
	}
	return authorization.Workspace.OwnerID, false, nil
}

func loadPublicMutationAccessOwner(
	ctx context.Context, app core.App, ownerID string,
) (storedPublicMutationAccessOwner, error) {
	owner := storedPublicMutationAccessOwner{}
	err := app.DB().NewQuery(`
		SELECT [[id]] AS [[principal_id]],
			COALESCE(NULLIF(trim([[name]]),''),NULLIF(trim([[email]]),''),[[id]]) AS [[label]],
			[[email]] FROM {{users}} WHERE [[id]]={:ownerId}
	`).WithContext(ctx).Bind(dbx.Params{"ownerId": ownerID}).One(&owner)
	return owner, err
}
