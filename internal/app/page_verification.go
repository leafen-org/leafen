package app

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page is verified by its own verification as Notion verifies one, unless
// its database's Verification property holds it (page_verification_way.go):
// someone who can edit it verifies it until a time or indefinitely, takes
// the verification away, or names its owners, whom an expiring verification
// reminds to verify it again. A page needs an owner to be verified, so a
// page verified without owners is owned by whoever verifies it, and a
// verified page keeps at least one. A wiki's page, or another database's
// page with a Verification property, is verified by that property instead,
// and its owners are its Owner values. A database opened as its own page is
// verified the same way (schema_baseline.json).

func registerPageVerificationRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageVerificationReadPath, readPageVerification)
	event.Router.PUT(apiRoot+pageVerificationWritePath, writePageVerification)
	event.Router.GET(apiRoot+databaseVerificationReadPath, readPageVerification)
	event.Router.PUT(apiRoot+databaseVerificationWritePath, writePageVerification)
}

// verificationTarget is what a verification is of: a page, or a database on
// a page, named by its block and kept on its data source.
type verificationTarget struct {
	PageID       string
	BlockID      string
	DataSourceID string
}

// requestVerificationTarget reads the page, or the page and database block,
// a request names: a database is verified on its first data source, a linked
// one's being its source's, on the page that holds that source.
func requestVerificationTarget(app core.App, event *core.RequestEvent) (verificationTarget, error) {
	target := verificationTarget{
		PageID:  strings.TrimSpace(event.Request.PathValue("pageId")),
		BlockID: strings.TrimSpace(event.Request.PathValue("blockId")),
	}
	if target.BlockID == "" {
		return target, nil
	}
	source := struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[page_id]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_database_container_sources}} [[membership]] ON [[membership]].[[container_id]] = [[container]].[[id]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[membership]].[[data_source_id]]
		WHERE [[container]].[[page_id]] = {:pageId} AND [[container]].[[block_id]] = {:blockId}
		ORDER BY [[membership]].[[position]], [[source]].[[id]]
		LIMIT 1
	`).Bind(dbx.Params{"pageId": target.PageID, "blockId": target.BlockID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return target, errWikiNotFound
	}
	if err != nil {
		return target, err
	}
	return verificationTarget{PageID: source.PageID, BlockID: target.BlockID, DataSourceID: source.ID}, nil
}

// authorizedVerificationTarget is what a verification names, held to the
// person's access, and refused saying which isn't there: the page the path
// names, or the database its block holds there — one whose source is on a
// page they can't open is none to them (2026-11-26: both were "Wiki or page
// not found.", whose target named the block for a page no one could open).
func authorizedVerificationTarget(
	app core.App, event *core.RequestEvent, userID string, requirement permissionRequirement,
) (verificationTarget, error) {
	if _, err := authorizePageBody(app, userID, strings.TrimSpace(event.Request.PathValue("pageId")), requirement); err != nil {
		if isPermissionDenied(err) {
			err = refusedFor(http.StatusNotFound, errorCodePageNotFound, "Page not found.")
		}
		return verificationTarget{}, err
	}
	target, err := requestVerificationTarget(app, event)
	if err == nil && target.DataSourceID != "" {
		if _, err = authorizePageBody(app, userID, target.PageID, requirement); isPermissionDenied(err) {
			err = errWikiNotFound
		}
	}
	if errors.Is(err, errWikiNotFound) {
		err = refusedFor(http.StatusNotFound, errorCodeDatabaseNotFound, "Check blockId: this page holds no database "+
			"with that block ID that you can open.")
	}
	return target, err
}

func readPageVerification(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	target, err := authorizedVerificationTarget(event.App, event, user.Id, permissionWorkspaceRead)
	if err != nil {
		return wikiAPIError(event, err, "authorize-verification")
	}
	wire, err := loadVerification(event.App, target)
	if err != nil {
		return wikiAPIError(event, err, "read-verification")
	}
	return event.JSON(http.StatusOK, wire)
}

func writePageVerification(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := pageVerificationInput{}
	if err := event.BindBody(&input); err != nil {
		return wikiAPIError(event, bodyRefusal(errWikiInvalid, "page verification change", err), "bind-verification")
	}
	var wire pageVerificationWire
	err := event.App.RunInTransaction(func(txApp core.App) error {
		target, err := authorizedVerificationTarget(txApp, event, user.Id, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		if err := applyVerification(txApp, user.Id, target, input, time.Now().UTC()); err != nil {
			return err
		}
		wire, err = loadVerification(txApp, target)
		return err
	})
	if err != nil {
		return wikiAPIError(event, err, "write-verification")
	}
	return event.JSON(http.StatusOK, wire)
}

type storedPageVerification struct {
	WorkspaceID  string `db:"workspace"`
	DataSourceID string `db:"data_source_id"`
	Revision     int    `db:"wiki_revision"`
	Generation   int    `db:"wiki_verification_generation"`
	VerifiedAt   string `db:"verified_at"`
	VerifiedBy   string `db:"verified_by"`
	ExpiresAt    string `db:"verification_expires_at"`
}

// loadStoredVerification reads a page's verification and how it is verified
// (page_verification_way.go), or a database's from its data source; a
// database is verified by its own verification.
func loadStoredVerification(app core.App, target verificationTarget) (storedPageVerification, pageVerificationWay, error) {
	stored := storedPageVerification{}
	if target.DataSourceID != "" {
		err := app.DB().NewQuery(`SELECT [[workspace_id]] AS [[workspace]], '' AS [[data_source_id]],
			[[verification_revision]] AS [[wiki_revision]], [[verification_generation]] AS [[wiki_verification_generation]],
			[[verified_at]], [[verified_by]], [[verification_expires_at]]
			FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": target.DataSourceID}).One(&stored)
		return stored, pageVerificationWay{}, err
	}
	err := app.DB().NewQuery(`SELECT [[workspace]], [[data_source_id]], [[wiki_revision]],
		[[wiki_verification_generation]], [[verified_at]], [[verified_by]], [[verification_expires_at]]
		FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": target.PageID}).One(&stored)
	if err != nil {
		return stored, pageVerificationWay{}, err
	}
	way, err := loadPageVerificationWay(app, target.PageID)
	return stored, way, err
}

// loadVerification reads a verification and its owners in their order: a
// page verified by its Verification property has its Owner values, another
// page or a database those it names.
func loadVerification(app core.App, target verificationTarget) (pageVerificationWire, error) {
	pageID := target.PageID
	stored, way, err := loadStoredVerification(app, target)
	if err != nil {
		return pageVerificationWire{}, err
	}
	owners := []pageVerificationOwner{}
	if way.ByProperty {
		err = app.DB().NewQuery(`
			SELECT [[owner]].[[value]] AS [[user_id]], COALESCE([[user]].[[name]], '') AS [[name]],
				COALESCE([[user]].[[email]], '') AS [[email]]
			FROM {{leafen_database_row_values}} AS [[value]]
			JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = {:dataSourceId}
			JOIN json_each(CASE WHEN json_valid([[value]].[[value]]) THEN [[value]].[[value]] ELSE '[]' END) AS [[owner]]
			LEFT JOIN {{users}} AS [[user]] ON [[user]].[[id]] = [[owner]].[[value]]
			WHERE [[value]].[[row_id]] = {:pageId} AND [[value]].[[property_id]] = [[source]].[[wiki_owner_property_id]]
				AND [[owner]].[[type]] = 'text'
			ORDER BY [[owner]].[[key]] LIMIT {:limit}
		`).Bind(dbx.Params{"pageId": pageID, "dataSourceId": stored.DataSourceID, "limit": maxWikiOwnersPerPage}).All(&owners)
	} else {
		err = app.DB().NewQuery(`
			SELECT [[owner]].[[user_id]], COALESCE([[user]].[[name]], '') AS [[name]],
				COALESCE([[user]].[[email]], '') AS [[email]]
			FROM {{leafen_wiki_page_owners}} AS [[owner]]
			LEFT JOIN {{users}} AS [[user]] ON [[user]].[[id]] = [[owner]].[[user_id]]
			WHERE [[owner]].[[page_id]] = {:pageId} AND [[owner]].[[data_source_id]] = {:dataSourceId}
			ORDER BY [[owner]].[[position]], [[owner]].[[user_id]] LIMIT {:limit}
		`).Bind(dbx.Params{"pageId": pageID, "dataSourceId": target.DataSourceID, "limit": maxWikiOwnersPerPage}).All(&owners)
	}
	if err != nil {
		return pageVerificationWire{}, err
	}
	return pageVerificationWire{
		PageID: pageID, BlockID: target.BlockID, Wiki: way.Wiki, ByProperty: way.ByProperty, Revision: stored.Revision, VerifiedAt: stored.VerifiedAt,
		VerifiedBy: stored.VerifiedBy, VerificationExpiresAt: stored.ExpiresAt, Owners: owners,
	}, nil
}

// applyVerification verifies a page by its own verification, or a
// database, takes its verification away or names its owners, as its
// revision stood.
func applyVerification(
	app core.App,
	principalID string,
	target verificationTarget,
	input pageVerificationInput,
	now time.Time,
) error {
	pageID, dataSourceID := target.PageID, target.DataSourceID
	stored, way, err := loadStoredVerification(app, target)
	if err != nil {
		return err
	}
	if way.ByProperty {
		return errPageVerifiedByProperty
	}
	if stored.Revision != input.ExpectedRevision {
		return errWikiRevisionChanged
	}
	current := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_wiki_page_owners}}
		WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
		ORDER BY [[position]], [[user_id]]`).Bind(dbx.Params{"pageId": pageID, "dataSourceId": dataSourceID}).Column(&current); err != nil {
		return err
	}
	owners, verified := current, stored.VerifiedAt != ""
	verifiedAt, verifiedBy, expiresAt := stored.VerifiedAt, stored.VerifiedBy, stored.ExpiresAt
	generation := stored.Generation
	switch input.Operation {
	case pageVerificationVerify:
		// The workspace's guest edits what is shared with them but verifies
		// nothing, as Notion offers its guests no "인증 추가"; a verifier
		// becomes the page's owner, and owners are the workspace's members.
		if member, err := pageVerificationWorkspaceMember(app, stored.WorkspaceID, principalID); err != nil {
			return err
		} else if !member {
			return wikiRefused("the one verifying is a guest of the workspace: its members verify its pages")
		}
		expiresAt = ""
		if strings.TrimSpace(input.ExpiresAt) != "" {
			until, err := time.Parse(time.RFC3339, strings.TrimSpace(input.ExpiresAt))
			if err != nil {
				return wikiRefused("expiresAt is left out, or a time as RFC 3339 writes it, not %q", input.ExpiresAt)
			}
			if !until.After(now) {
				return wikiRefused("expiresAt is a time still to come")
			}
			expiresAt = until.UTC().Format(time.RFC3339Nano)
		}
		if len(owners) == 0 {
			owners = []string{principalID}
		}
		verified, verifiedAt, verifiedBy = true, now.Format(time.RFC3339Nano), principalID
		generation++
	case pageVerificationUnverify:
		if !verified {
			return wikiStateRefused("the page isn't verified")
		}
		verified, verifiedAt, verifiedBy, expiresAt = false, "", "", ""
		generation++
	case pageVerificationSetOwners:
		owners = []string{}
		for _, owner := range input.Owners {
			if owner = strings.TrimSpace(owner); owner != "" && !slices.Contains(owners, owner) {
				owners = append(owners, owner)
			}
		}
		if len(owners) > maxWikiOwnersPerPage {
			return wikiRefused("owners names %d people, over %d", len(owners), maxWikiOwnersPerPage)
		}
		if verified && len(owners) == 0 {
			return wikiRefused("owners is empty: a verified page keeps at least one owner")
		}
		if err := requirePageVerificationOwnersInWorkspace(app, stored.WorkspaceID, owners); err != nil {
			return err
		}
	default:
		return wikiRefused("operation is %s, %s or %s, not %q", pageVerificationVerify, pageVerificationUnverify,
			pageVerificationSetOwners, input.Operation)
	}

	if !slices.Equal(owners, current) {
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_wiki_page_owners}}
			WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}`).
			Bind(dbx.Params{"pageId": pageID, "dataSourceId": dataSourceID}).Execute(); err != nil {
			return err
		}
		for position, owner := range owners {
			// A page outside a wiki, or a database's page, is its owners' root.
			if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_wiki_page_owners}}
				([[page_id]], [[data_source_id]], [[root_page_id]], [[user_id]], [[position]], [[created_at]])
				VALUES ({:pageId}, {:dataSourceId}, {:pageId}, {:userId}, {:position}, {:now})`).Bind(dbx.Params{
				"pageId": pageID, "dataSourceId": dataSourceID, "userId": owner, "position": position, "now": now.UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		}
	}
	if generation != stored.Generation {
		// A new verification ends what the last one scheduled: its turn in
		// the queue and the reminders it has still to send.
		millis := now.UnixMilli()
		cancelled := dbx.Params{"pageId": pageID, "dataSourceId": dataSourceID, "now": millis}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_wiki_verification_queue}}
			SET [[status]] = 'cancelled', [[lease_token]] = '', [[lease_until]] = 0,
				[[completed_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
				AND [[status]] NOT IN ('completed', 'cancelled', 'failed')
		`).Bind(cancelled).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_wiki_verification_email_deliveries}}
			SET [[status]] = 'cancelled', [[lease_token]] = '', [[lease_until]] = 0, [[updated_at]] = {:now}
			WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
				AND [[status]] NOT IN ('sent', 'skipped', 'failed', 'cancelled')
		`).Bind(cancelled).Execute(); err != nil {
			return err
		}
		if verified && expiresAt != "" {
			until, _ := time.Parse(time.RFC3339Nano, expiresAt)
			due := until.UnixMilli()
			if _, err := app.DB().NewQuery(`
				INSERT INTO {{leafen_wiki_verification_queue}}
					([[page_id]], [[data_source_id]], [[generation]], [[workspace_id]], [[due_at]], [[status]], [[attempts]],
					 [[next_attempt_at]], [[lease_token]], [[lease_until]], [[error_code]], [[expired_at]],
					 [[completed_at]], [[created_at]], [[updated_at]])
				VALUES ({:pageId}, {:dataSourceId}, {:generation}, {:workspaceId}, {:due}, 'ready', 0, {:due}, '', 0, '', 0, 0, {:now}, {:now})
			`).Bind(dbx.Params{
				"pageId": pageID, "dataSourceId": dataSourceID, "generation": generation, "workspaceId": stored.WorkspaceID,
				"due": due, "now": millis,
			}).Execute(); err != nil {
				return err
			}
			notifyWikiVerificationAfterCommit(app)
		}
	}
	update := `UPDATE {{leafen_pages}}
		SET [[verified_at]] = {:verifiedAt}, [[verified_by]] = {:verifiedBy},
			[[verification_expires_at]] = {:expiresAt}, [[wiki_verification_generation]] = {:generation},
			[[wiki_revision]] = [[wiki_revision]] + 1
		WHERE [[id]] = {:pageId} AND [[wiki_revision]] = {:revision}`
	if dataSourceID != "" {
		update = `UPDATE {{leafen_data_sources}}
		SET [[verified_at]] = {:verifiedAt}, [[verified_by]] = {:verifiedBy},
			[[verification_expires_at]] = {:expiresAt}, [[verification_generation]] = {:generation},
			[[verification_revision]] = [[verification_revision]] + 1
		WHERE [[id]] = {:dataSourceId} AND [[verification_revision]] = {:revision}`
	}
	result, err := app.DB().NewQuery(update).Bind(dbx.Params{
		"verifiedAt": verifiedAt, "verifiedBy": verifiedBy, "expiresAt": expiresAt, "generation": generation,
		"pageId": pageID, "dataSourceId": dataSourceID, "revision": stored.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errWikiRevisionChanged
	}
	return nil
}

// requirePageVerificationOwnersInWorkspace holds a page's owners to the
// people of its workspace, as the reminders its verification sends reach.
func requirePageVerificationOwnersInWorkspace(app core.App, workspaceID string, owners []string) error {
	for _, owner := range owners {
		member, err := pageVerificationWorkspaceMember(app, workspaceID, owner)
		if err != nil {
			return err
		}
		if !member {
			return wikiRefused("owners names %s, who isn't a member of the workspace", owner)
		}
	}
	return nil
}

// pageVerificationWorkspaceMember says whether someone is the workspace's
// owner or member — not its guest — who may verify its pages and own them.
func pageVerificationWorkspaceMember(app core.App, workspaceID, userID string) (bool, error) {
	var member int
	err := app.DB().NewQuery(`SELECT EXISTS (
			SELECT 1 FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId} AND [[workspace_owner]] = {:userId}
		) OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND [[role]] = 'member'
		)`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&member)
	return member == 1, err
}
