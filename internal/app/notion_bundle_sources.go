package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A migration from another tool (docs/bundle.md): the person names a new,
// empty workspace of their own as the place for the tool's bundle, and a
// migration connection for it. Leafen makes a token that reads only that
// workspace, for a week, and the connection reads it through Leafen's
// Notion API — so the Notion migration's plan, check, run, report and run
// again move it as they move Notion. The bundle then goes into that
// workspace with the archive import, its IDs made from the connection; the
// tool's next bundle, put in another new workspace the source moves to
// once the first is deleted, keeps them, and the same plan runs again.

const (
	bundleSourcesPath       = "/workspaces/{workspaceId}/imports/bundle-sources"
	bundleSourcePath        = bundleSourcesPath + "/{connectionId}"
	bundleSourceLabelRunes  = 60
	bundleSourceToolRunes   = 200
	bundleSourceOriginRunes = 255
	bundleSourceTokenWindow = "7d"
	// The list of sources gives this many at a time.
	bundleSourcesWindow = 50
)

type bundleSourceInput struct {
	RequestID          string `json:"requestId"`
	StagingWorkspaceID string `json:"stagingWorkspaceId"`
	Label              string `json:"label"`
	// Optional: the tool that made the bundle and the source in it the
	// bundle comes from, to find this source again by.
	Tool   string `json:"tool"`
	Origin string `json:"origin"`
}

type bundleSourceWire struct {
	ConnectionID       string `json:"connectionId"`
	StagingWorkspaceID string `json:"stagingWorkspaceId"`
	// The workspace's name while it is there, to confirm deleting it.
	StagingWorkspaceName string                `json:"stagingWorkspaceName,omitempty"`
	Label                string                `json:"label"`
	Tool                 string                `json:"tool,omitempty"`
	Origin               string                `json:"origin,omitempty"`
	Status               string                `json:"status"`
	ImportJobID          string                `json:"importJobId,omitempty"`
	Losses               []workspaceBundleLoss `json:"losses"`
}

type storedBundleSource struct {
	ConnectionID       string `db:"connection_id"`
	StagingWorkspaceID string `db:"staging_workspace_id"`
	TokenID            string `db:"token_id"`
	ImportJobID        string `db:"import_job_id"`
	Label              string `db:"label"`
	Tool               string `db:"tool"`
	Origin             string `db:"origin"`
	Status             string `db:"status"`
	Losses             string `db:"losses"`
	Active             int    `db:"active"`
}

var (
	errBundleSourceNotFresh   = errors.New("bundle source workspace not empty")
	errBundleSourceNotFound   = errors.New("bundle source not found")
	errBundleSourceEarlier    = errors.New("bundle source earlier workspace remains")
	errBundleSourceDeleting   = errors.New("bundle source earlier workspace deleting")
	errBundleSourceNotStaged  = errors.New("bundle source not staged")
	errBundleSourceStagingRef = errors.New("bundle source workspace taken")
)

func registerBundleSourceRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+bundleSourcesPath, createBundleSourceRoute)
	event.Router.GET(apiRoot+bundleSourcesPath, listBundleSourcesRoute)
	event.Router.GET(apiRoot+bundleSourcePath, readBundleSourceRoute)
	event.Router.PUT(apiRoot+bundleSourcePath, moveBundleSourceRoute)
	event.Router.DELETE(apiRoot+bundleSourcePath, closeBundleSourceRoute)
}

func createBundleSourceRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := bundleSourceInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the bundle source.")
	}
	input.RequestID, input.StagingWorkspaceID, input.Label = strings.TrimSpace(input.RequestID), strings.TrimSpace(input.StagingWorkspaceID), strings.TrimSpace(input.Label)
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 255 || input.StagingWorkspaceID == "" || input.StagingWorkspaceID == workspaceID ||
		input.Label == "" || utf8.RuneCountInString(input.Label) > bundleSourceLabelRunes || strings.ContainsAny(input.Label, "\r\n") {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Name the tool, and give the empty workspace its bundle will go into.")
	}
	if !bundleSourceKey(input.Tool, bundleSourceToolRunes) || !bundleSourceKey(input.Origin, bundleSourceOriginRunes) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the source's tool and origin.")
	}
	digest := sha256.Sum256([]byte("bundle\x00" + workspaceID + "\x00" + input.StagingWorkspaceID + "\x00" + input.Label +
		"\x00" + input.Tool + "\x00" + input.Origin))
	requestHash := hex.EncodeToString(digest[:])
	if existing, err := loadNotionConnectionByRequest(event.App, user.Id, input.RequestID); err == nil {
		if existing.RequestHash != requestHash {
			return apiError(event, http.StatusConflict, errorCodeJobIdentityReused, "The request ID was used for a different source.")
		}
		return bundleSourceAnswer(event, http.StatusOK, user.Id, workspaceID, existing.ID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't read the source.", err)
	}
	token, err := newBundleSourceToken(event.App, input.Label)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't make the source.", err)
	}
	connection := storedNotionImportConnection{ID: security.RandomString(15), Owner: user.Id, WorkspaceID: workspaceID,
		RequestID: input.RequestID, RequestHash: requestHash, Label: input.Label, TokenCipher: token.cipher, Active: 1}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
			return err
		}
		if err := admitBundleSourceStaging(txApp, user.Id, input.StagingWorkspaceID); err != nil {
			return err
		}
		tokenID, err := token.issue(txApp, user.Id, input.StagingWorkspaceID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_notion_import_connections}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[label]], [[token_cipher]], [[active]], [[created]], [[updated]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:label}, {:tokenCipher}, 1, {:now}, {:now})
		`).Bind(dbx.Params{"id": connection.ID, "owner": connection.Owner, "workspaceId": connection.WorkspaceID,
			"requestId": connection.RequestID, "requestHash": connection.RequestHash, "label": connection.Label,
			"tokenCipher": connection.TokenCipher, "now": now}).Execute(); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_bundle_import_sources}}
				([[connection_id]], [[staging_workspace_id]], [[token_id]], [[import_job_id]], [[label]], [[tool]], [[origin]], [[status]], [[losses]], [[page_ids]], [[created]], [[updated]])
			VALUES ({:connection}, {:staging}, {:token}, '', {:label}, {:tool}, {:origin}, 'importing', '[]', 1, {:now}, {:now})
		`).Bind(dbx.Params{"connection": connection.ID, "staging": input.StagingWorkspaceID, "token": tokenID,
			"label": input.Label, "tool": input.Tool, "origin": input.Origin, "now": now}).Execute()
		return err
	})
	if err != nil {
		return bundleSourceError(event, err, "create")
	}
	return bundleSourceAnswer(event, http.StatusCreated, user.Id, workspaceID, connection.ID)
}

// listBundleSourcesRoute lists the person's sources for a workspace that
// aren't closed, the latest first — with ?tool=, that tool's — so a tool's
// screen finds the one to bring its next bundle to: a window at a time from
// ?cursor= on, its nextCursor "" once the last is given.
func listBundleSourcesRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		return bundleSourceError(event, err, "list")
	}
	tool := strings.TrimSpace(event.Request.URL.Query().Get("tool"))
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceImportInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	rows := []struct {
		ID      string `db:"connection_id"`
		Updated int64  `db:"updated"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[source]].[[connection_id]], [[source]].[[updated]] FROM {{leafen_bundle_import_sources}} AS [[source]]
		JOIN {{leafen_notion_import_connections}} AS [[connection]] ON [[connection]].[[id]] = [[source]].[[connection_id]]
		WHERE [[connection]].[[owner]] = {:owner} AND [[connection]].[[workspace_id]] = {:workspace}
			AND [[connection]].[[active]] = 1 AND [[source]].[[status]] != 'closed'
			AND ({:tool} = '' OR [[source]].[[tool]] = {:tool})
			AND ({:afterId} = '' OR [[source]].[[updated]] < {:afterAt}
				OR ([[source]].[[updated]] = {:afterAt} AND [[source]].[[connection_id]] > {:afterId}))
		ORDER BY [[source]].[[updated]] DESC, [[source]].[[connection_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": user.Id, "workspace": workspaceID, "tool": tool, "afterAt": after.At, "afterId": after.ID, "limit": bundleSourcesWindow + 1,
	}).All(&rows); err != nil {
		return bundleSourceError(event, err, "list")
	}
	next := ""
	if len(rows) > bundleSourcesWindow {
		rows = rows[:bundleSourcesWindow]
		next = encodeTimeIDCursor(rows[len(rows)-1].Updated, rows[len(rows)-1].ID)
	}
	items := []bundleSourceWire{}
	for _, row := range rows {
		source, err := loadBundleSource(event.App, user.Id, workspaceID, row.ID)
		if err != nil {
			return bundleSourceError(event, err, "list")
		}
		items = append(items, bundleSourceAnswerOf(event.App, source))
	}
	return event.JSON(http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func readBundleSourceRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		return bundleSourceError(event, err, "read")
	}
	return bundleSourceAnswer(event, http.StatusOK, user.Id, workspaceID, strings.TrimSpace(event.Request.PathValue("connectionId")))
}

// moveBundleSourceRoute points the source at a workspace: a new, empty one
// for the tool's next bundle, once the one before is deleted, or the same
// one again for a new week's token.
func moveBundleSourceRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	connectionID := strings.TrimSpace(event.Request.PathValue("connectionId"))
	input := bundleSourceInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the bundle source.")
	}
	staging := strings.TrimSpace(input.StagingWorkspaceID)
	if staging == "" || staging == workspaceID {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Give the workspace the bundle goes into.")
	}
	source, err := loadBundleSource(event.App, user.Id, workspaceID, connectionID)
	if err != nil {
		return bundleSourceError(event, err, "move-read")
	}
	token, err := newBundleSourceToken(event.App, source.Label)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't move the source.", err)
	}
	release := acquireNotionPersonalAccessTokenLifecycleLock(source.TokenID)
	defer release()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
			return err
		}
		current, err := loadBundleSource(txApp, user.Id, workspaceID, connectionID)
		if err != nil {
			return err
		}
		if current.Status == "closed" || current.Active == 0 {
			return errBundleSourceNotFound
		}
		moving := staging != current.StagingWorkspaceID
		if moving {
			// The earlier workspace holds the IDs the next bundle makes: it
			// goes first, to its end.
			var deletion string
			err := txApp.DB().NewQuery(`SELECT [[deletion_job_id]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspace}`).
				Bind(dbx.Params{"workspace": current.StagingWorkspaceID}).Row(&deletion)
			switch {
			case err == nil && deletion != "":
				return errBundleSourceDeleting
			case err == nil:
				return errBundleSourceEarlier
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
			if err := admitBundleSourceStaging(txApp, user.Id, staging); err != nil {
				return err
			}
		} else if bundleSourceStatus(txApp, current) != "ready" {
			return errBundleSourceNotStaged
		}
		if err := revokeBundleSourceToken(txApp, user.Id, current.TokenID); err != nil {
			return err
		}
		tokenID, err := token.issue(txApp, user.Id, staging)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_connections}} SET [[token_cipher]] = {:cipher}, [[updated]] = {:now}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[active]] = 1
		`).Bind(dbx.Params{"cipher": token.cipher, "now": now, "id": connectionID, "owner": user.Id}).Execute(); err != nil {
			return err
		}
		if !moving {
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_bundle_import_sources}} SET [[token_id]] = {:token}, [[updated]] = {:now}
				WHERE [[connection_id]] = {:id}`).Bind(dbx.Params{"token": tokenID, "now": now, "id": connectionID}).Execute()
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_bundle_import_sources}}
			SET [[staging_workspace_id]] = {:staging}, [[token_id]] = {:token}, [[import_job_id]] = '', [[status]] = 'importing',
				[[losses]] = '[]', [[updated]] = {:now}
			WHERE [[connection_id]] = {:id}
		`).Bind(dbx.Params{"staging": staging, "token": tokenID, "now": now, "id": connectionID}).Execute()
		return err
	})
	if err != nil {
		return bundleSourceError(event, err, "move")
	}
	return bundleSourceAnswer(event, http.StatusOK, user.Id, workspaceID, connectionID)
}

// closeBundleSourceRoute ends the source: its token, its connection and
// the connection's migrations. The workspace the bundle went into stays
// until the person deletes it, as any workspace.
func closeBundleSourceRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	connectionID := strings.TrimSpace(event.Request.PathValue("connectionId"))
	source, err := loadBundleSource(event.App, user.Id, workspaceID, connectionID)
	if err != nil {
		return bundleSourceError(event, err, "close-read")
	}
	release := acquireNotionPersonalAccessTokenLifecycleLock(source.TokenID)
	defer release()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
			return err
		}
		current, err := loadBundleSource(txApp, user.Id, workspaceID, connectionID)
		if err != nil || current.Status == "closed" {
			return err
		}
		if err := revokeBundleSourceToken(txApp, user.Id, current.TokenID); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_connections}} SET [[active]] = 0, [[token_cipher]] = '', [[updated]] = {:now}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner}
		`).Bind(dbx.Params{"now": now, "id": connectionID, "owner": user.Id}).Execute(); err != nil {
			return err
		}
		if err := endNotionMigrationsOfConnection(txApp, connectionID, now); err != nil {
			return err
		}
		for _, table := range []string{"leafen_bundle_import_ids", "leafen_bundle_import_versions"} {
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{` + table + `}} WHERE [[connection_id]] = {:id}`).
				Bind(dbx.Params{"id": connectionID}).Execute(); err != nil {
				return err
			}
		}
		_, err = txApp.DB().NewQuery(`UPDATE {{leafen_bundle_import_sources}} SET [[status]] = 'closed', [[updated]] = {:now}
			WHERE [[connection_id]] = {:id}`).Bind(dbx.Params{"now": now, "id": connectionID}).Execute()
		return err
	})
	if err != nil {
		return bundleSourceError(event, err, "close")
	}
	return bundleSourceAnswer(event, http.StatusOK, user.Id, workspaceID, connectionID)
}

// admitBundleSourceStaging checks the workspace a bundle will go into: the
// person's own, empty, and no other source's.
func admitBundleSourceStaging(txApp core.App, userID, stagingID string) error {
	if _, err := authorizeWorkspace(txApp, userID, stagingID, permissionWorkspaceOwner); err != nil {
		return err
	}
	fresh, err := workspaceImportArchiveTargetFresh(txApp, stagingID)
	if err != nil {
		return err
	}
	if !fresh {
		return errBundleSourceNotFresh
	}
	var taken int
	if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_bundle_import_sources}}
		WHERE [[staging_workspace_id]] = {:staging} AND [[status]] != 'closed'`).
		Bind(dbx.Params{"staging": stagingID}).Row(&taken); err != nil {
		return err
	}
	if taken > 0 {
		return errBundleSourceStagingRef
	}
	return nil
}

func loadBundleSource(app core.App, userID, workspaceID, connectionID string) (storedBundleSource, error) {
	source := storedBundleSource{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[connection_id]], [[source]].[[staging_workspace_id]], [[source]].[[token_id]],
			[[source]].[[import_job_id]], [[source]].[[label]], [[source]].[[tool]], [[source]].[[origin]], [[source]].[[status]],
			[[source]].[[losses]], [[connection]].[[active]]
		FROM {{leafen_bundle_import_sources}} AS [[source]]
		JOIN {{leafen_notion_import_connections}} AS [[connection]] ON [[connection]].[[id]] = [[source]].[[connection_id]]
		WHERE [[source]].[[connection_id]] = {:id} AND [[connection]].[[owner]] = {:owner} AND [[connection]].[[workspace_id]] = {:workspace}
	`).Bind(dbx.Params{"id": connectionID, "owner": userID, "workspace": workspaceID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return source, errBundleSourceNotFound
	}
	return source, err
}

// bundleSourceStatus is where the source stands: waiting for its bundle
// (importing), ready, failed, detached (its workspace deleted) or closed —
// here or by disconnecting its connection on the import screen.
func bundleSourceStatus(app core.App, source storedBundleSource) string {
	if source.Status == "closed" || source.Active == 0 {
		return "closed"
	}
	var workspaces int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}} WHERE [[id]] = {:id} AND [[deletion_job_id]] = ''`).
		Bind(dbx.Params{"id": source.StagingWorkspaceID}).Row(&workspaces); err != nil || workspaces == 0 {
		return "detached"
	}
	if source.ImportJobID == "" {
		return "importing"
	}
	var status string
	if err := app.DB().NewQuery(`SELECT [[status]] FROM {{leafen_jobs}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": source.ImportJobID}).Row(&status); err != nil {
		return "failed"
	}
	switch status {
	case durableJobStatusCompleted:
		return "ready"
	case durableJobStatusFailed, durableJobStatusCanceled:
		return "failed"
	}
	return "importing"
}

func bundleSourceAnswer(event *core.RequestEvent, status int, userID, workspaceID, connectionID string) error {
	source, err := loadBundleSource(event.App, userID, workspaceID, connectionID)
	if err != nil {
		return bundleSourceError(event, err, "answer")
	}
	return event.JSON(status, bundleSourceAnswerOf(event.App, source))
}

func bundleSourceAnswerOf(app core.App, source storedBundleSource) bundleSourceWire {
	losses := []workspaceBundleLoss{}
	_ = json.Unmarshal([]byte(source.Losses), &losses)
	var name string
	_ = app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id} AND [[deletion_job_id]] = ''`).
		Bind(dbx.Params{"id": source.StagingWorkspaceID}).Row(&name)
	return bundleSourceWire{ConnectionID: source.ConnectionID, StagingWorkspaceID: source.StagingWorkspaceID, StagingWorkspaceName: name,
		Label: source.Label, Tool: source.Tool, Origin: source.Origin, Status: bundleSourceStatus(app, source),
		ImportJobID: source.ImportJobID, Losses: losses}
}

// bundleSourceKey reports whether a source's optional tool or origin is
// one line of at most limit characters, kept as given to be found by.
func bundleSourceKey(value string, limit int) bool {
	return utf8.RuneCountInString(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}

func bundleSourceError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errBundleSourceNotFresh):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportTargetNotFresh,
			"The bundle goes into a new, empty workspace.")
	case errors.Is(err, errBundleSourceStagingRef):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportTargetNotFresh,
			"Another migration's bundle goes into that workspace.")
	case errors.Is(err, errBundleSourceEarlier):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportStateInvalid,
			"Delete the workspace the last bundle went into first.")
	case errors.Is(err, errBundleSourceDeleting):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportStateInvalid,
			"The workspace the last bundle went into is still being deleted. Try again in a moment.")
	case errors.Is(err, errBundleSourceNotStaged):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportStateInvalid,
			"Import the tool's bundle into that workspace first, and wait for the import to finish.")
	case errors.Is(err, errBundleSourceNotFound):
		return apiError(event, http.StatusNotFound, errorCodeNotionImportConnectionNotFound, "Source not found.")
	case isPermissionDenied(err), errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	return personalAccessTokenError(event, err, "bundle-source-"+stage)
}
