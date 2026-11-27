package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// runAppDataSetup makes an app's data in its workspace once, as the app's
// account through its own connection: the places the data goes, then the
// steps in order. A failure is kept and the next opening tries again.
func runAppDataSetup(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace) error {
	defer appDataTurn(workspace.WorkspaceID)()
	var state string
	if err := app.DB().NewQuery(`SELECT [[setup_state]] FROM {{leafen_app_workspaces}} WHERE [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"workspace": workspace.WorkspaceID}).Row(&state); err != nil {
		return err
	}
	if state == appDataSetupDone {
		// A workspace made from an earlier version catches up; a failure is
		// kept as a first making's is, and the next opening tries again.
		if err := catchUpAppData(app, install, manifest, workspace); err != nil {
			return errors.Join(err, noteAppDataFailure(app, install, err, appDataFailureUpdate, ""))
		}
		return nil
	}
	shared := false
	for _, data := range manifest.Data {
		shared = shared || !data.PerPerson
	}
	if state == appDataSetupNotNeeded && !shared {
		return nil
	}
	if !shared {
		return setAppDataSetupState(app, workspace, appDataSetupNotNeeded)
	}
	err := makeAppData(app, install, manifest, workspace)
	if err != nil {
		_ = setAppDataSetupState(app, workspace, appDataSetupFailed)
		// A reason that couldn't be kept goes with the failure, not lost.
		return errors.Join(err, noteAppDataFailure(app, install, err, appDataFailureSetup, ""))
	}
	return setAppDataSetupState(app, workspace, appDataSetupDone)
}

// loadAppDataRows reads what has been made of an app's data in a tenancy.
func loadAppDataRows(app core.App, installID, tenancyKey string) ([]appDataRow, error) {
	rows := []appDataRow{}
	err := app.DB().NewQuery(`SELECT [[data_key]], [[page_id]], [[data_source_id]], [[view_id]], [[spec]] FROM {{leafen_app_data}}
		WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"install": installID, "tenancy": tenancyKey}).All(&rows)
	return rows, err
}

// appDataTurns lets one at a time make an app's data in a place: two openings
// at once each found nothing made and made it all (2026-11-04: a screen's first
// opening left two of every root page and database).
var appDataTurns sync.Map

// appDataTurn waits for the place's turn and answers how to end it.
func appDataTurn(place string) func() {
	turn, _ := appDataTurns.LoadOrStore(place, &sync.Mutex{})
	lock := turn.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func setAppDataSetupState(app core.App, workspace storedAppWorkspace, state string) error {
	_, err := app.DB().Update(appWorkspacesTable, dbx.Params{"setup_state": state}, dbx.HashExp{"workspace_id": workspace.WorkspaceID}).Execute()
	return err
}

// appDataSession is the app account's way to make and change data in the
// workspace: native calls with the account's key and Leafen's MCP calls
// with its authority, both through the account's own connection.
type appDataSession struct {
	connectionID string
	native       func(method, path string, body any) (map[string]any, error)
	mcp          context.Context
}

func newAppDataSession(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace) (appDataSession, error) {
	connectionID, err := appAccountConnection(app, install, manifest, workspace)
	if err != nil {
		return appDataSession{}, err
	}
	name := appOrganizationName(app, manifest, workspace.TenancyKey)
	key := userKeyAuthority{
		ConnectionID: connectionID, Name: name, OwnerID: workspace.AccountID, Scope: userKeyScopeStructure,
		WorkspaceIDs: []string{workspace.WorkspaceID}, Created: time.Now().UnixMilli(), AppInstallID: install.ID, AppAccount: true,
	}
	ctx := context.Background()
	native := func(method, path string, body any) (map[string]any, error) {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		result, err := callNativeOperation(ctx, key, method, path, "", encoded, "")
		if err != nil {
			return nil, err
		}
		if result.Status >= 300 {
			return nil, fmt.Errorf("%s %s answered %d", method, path, result.Status)
		}
		answer, _ := result.Body.(map[string]any)
		return answer, nil
	}
	scoped := []mcpGovernanceWorkspace{{ID: workspace.WorkspaceID, OrganizationID: appTenancyOrganization(workspace.TenancyKey), Name: name}}
	return appDataSession{connectionID: connectionID, native: native, mcp: context.WithValue(ctx, mcpAuthorityContextKey{}, mcpAuthority{
		ConnectionID: connectionID, Resource: "leafen-app", PrincipalID: workspace.AccountID,
		ClientID: appConnectionClientID + install.ID, ClientName: name, Scope: userKeyScopeStructure,
		ScopedWorkspaces: scoped, ApprovedWorkspaces: scoped, WorkspaceIDs: []string{workspace.WorkspaceID},
	})}, nil
}

// runStep makes what one setup step says, as Leafen's MCP creation call.
func (session appDataSession) runStep(app core.App, step appDataStep, args json.RawMessage) (map[string]string, error) {
	switch step.Tool {
	case appDataToolDatabase:
		input := notionMCPCreateDatabaseInput{}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
		_, output, err := createMCPDatabase(session.mcp, nil, app, input)
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", step.Key, err)
		}
		return map[string]string{"id": output.ID, "data_source_id": output.DataSourceID, "view_id": output.ViewID}, nil
	case appDataToolView:
		input := notionMCPCreateViewInput{}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
		_, output, err := createMCPView(session.mcp, nil, app, input)
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", step.Key, err)
		}
		return map[string]string{"id": output.ID}, nil
	case appDataToolPages:
		input := notionMCPCreatePagesInput{}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
		_, output, err := createMCPPagesIn(session.mcp, nil, app, input)
		if err != nil || len(output.Pages) == 0 {
			return nil, fmt.Errorf("step %s: %w", step.Key, err)
		}
		return map[string]string{"id": output.Pages[0].ID}, nil
	}
	return nil, fmt.Errorf("step %s: unknown tool %s", step.Key, step.Tool)
}

func makeAppData(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace) error {
	session, err := newAppDataSession(app, install, manifest, workspace)
	if err != nil {
		return err
	}
	// A try that stopped partway left what it made: the next one goes on from
	// there, so people opening the app don't each add a copy.
	rows, err := loadAppDataRows(app, install.ID, workspace.TenancyKey)
	if err != nil {
		return err
	}
	made := make(map[string]appDataRow, len(rows))
	for _, row := range rows {
		made[row.Key] = row
	}
	name, language := appOrganizationName(app, manifest, workspace.TenancyKey), appOrganizationLanguage(app, workspace.TenancyKey)
	viewTeamspace, rootTitles := appPlaceTitles(name, language)
	pageID := func(answer map[string]any) string {
		page, _ := answer["page"].(map[string]any)
		id, _ := page["id"].(string)
		return id
	}

	// The places the data goes: a teamspace the workspace's people edit, one
	// they only view, and the account's own pages, each with a root page.
	roots := map[string]string{}
	needed := map[string]bool{}
	for _, data := range manifest.Data {
		if !data.PerPerson {
			needed[appDataPlace(manifest, data.Key)] = true
		}
	}
	needed[appDataPlaceEdit] = true
	for _, place := range []string{appDataPlaceEdit, appDataPlaceView, appDataPlaceHidden} {
		if !needed[place] {
			continue
		}
		if root, found := made[appRootKey(place)]; found {
			roots[place] = root.Page
			continue
		}
		input := map[string]any{"requestId": security.RandomString(20), "workspaceId": workspace.WorkspaceID, "title": rootTitles[place]}
		if teamspace, found := made[appTeamspaceKey(place)]; found {
			input["teamspaceId"] = teamspace.Page
		} else if place != appDataPlaceHidden {
			// The view-only one says so in its name and wears a lock.
			role, icon, teamspaceName := "edit", "", name
			if place == appDataPlaceView {
				role, icon, teamspaceName = "view", "🔒", viewTeamspace
			}
			answer, err := session.native("POST", "/workspaces/"+workspace.WorkspaceID+"/teamspace-mutations", map[string]any{
				"requestId": security.RandomString(20), "action": "create", "expectedRevision": 0, "name": teamspaceName, "icon": icon,
				"access": "open", "openPageRole": role, "memberPageRole": role,
				"membersCanInvite": false, "membersCanEditSidebar": false,
			})
			if err != nil {
				return err
			}
			teamspace, _ := answer["teamspace"].(map[string]any)
			teamspaceID, _ := teamspace["id"].(string)
			if teamspaceID == "" {
				return errors.New("the app's teamspace wasn't made")
			}
			input["teamspaceId"] = teamspaceID
			if err := recordAppData(app, install, workspace, appTeamspaceKey(place), teamspaceID, "", "", ""); err != nil {
				return err
			}
		}
		answer, err := session.native("POST", "/pages", input)
		if err != nil {
			return err
		}
		if roots[place] = pageID(answer); roots[place] == "" {
			return errors.New("the app's root page wasn't made")
		}
		if err := recordAppData(app, install, workspace, appRootKey(place), roots[place], "", "", ""); err != nil {
			return err
		}
	}

	// The steps, as Leafen's MCP creation calls with the account's authority,
	// their names in the organization's language.
	steps, err := loadAppDataSteps(app, install.PackageHash, manifest)
	if err != nil {
		return err
	}
	results := map[string]map[string]string{}
	for _, step := range steps {
		if row, found := made[step.Key]; found {
			results[step.Key] = map[string]string{"id": row.Page, "data_source_id": row.Source, "view_id": row.View}
			continue
		}
		args, spec := appDataMakeArgs(step, language)
		result, err := session.runStep(app, step, fillAppDataPlaceholders(args, roots[appDataPlace(manifest, step.Key)], results))
		if err != nil {
			return err
		}
		results[step.Key] = result
		if err := recordAppData(app, install, workspace, step.Key, result["id"], result["data_source_id"], result["view_id"], spec); err != nil {
			return err
		}
	}
	if err := setAppDataPackage(app, workspace, install.PackageHash); err != nil {
		return err
	}
	// The people already in the workspace join its teamspaces.
	members := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]] = {:workspace} AND [[role]] = 'member' AND [[user_id]] != {:account}`).
		Bind(dbx.Params{"workspace": workspace.WorkspaceID, "account": workspace.AccountID}).Column(&members); err != nil {
		return err
	}
	for _, userID := range members {
		if err := joinAppTeamspaces(app, workspace, session.connectionID, userID); err != nil {
			return err
		}
	}
	return nil
}

func appTeamspaceKey(place string) string {
	return "teamspace-" + place
}

func appRootKey(place string) string {
	return "root-" + place
}

// recordAppData keeps what a step made and, for an update to compare
// against, the arguments it was made from.
func recordAppData(app core.App, install storedAppInstall, workspace storedAppWorkspace, key, pageID, sourceID, viewID, spec string) error {
	_, err := app.DB().NewQuery(`INSERT OR REPLACE INTO {{leafen_app_data}}
		([[install_id]], [[tenancy_key]], [[data_key]], [[page_id]], [[data_source_id]], [[view_id]], [[created]], [[spec]])
		VALUES ({:install}, {:tenancy}, {:key}, {:page}, {:source}, {:view}, {:now}, {:spec})`).Bind(dbx.Params{
		"install": install.ID, "tenancy": workspace.TenancyKey, "key": key, "page": pageID,
		"source": sourceID, "view": viewID, "now": time.Now().UnixMilli(), "spec": spec,
	}).Execute()
	return err
}

// joinAppTeamspaces makes a person a member of the app's teamspaces in the
// workspace, as the app's account adds them, so the app's pages are in
// their sidebar with the role each teamspace gives.
func joinAppTeamspaces(app core.App, workspace storedAppWorkspace, connectionID, userID string) error {
	teamspaces := []string{}
	if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_app_data}} WHERE [[install_id]] = {:install}
		AND [[tenancy_key]] = {:tenancy} AND [[data_key]] LIKE 'teamspace-%'`).Bind(dbx.Params{
		"install": workspace.InstallID, "tenancy": workspace.TenancyKey,
	}).Column(&teamspaces); err != nil {
		return err
	}
	key := userKeyAuthority{
		ConnectionID: connectionID, OwnerID: workspace.AccountID, Scope: userKeyScopeStructure,
		WorkspaceIDs: []string{workspace.WorkspaceID}, Created: time.Now().UnixMilli(), AppInstallID: workspace.InstallID, AppAccount: true,
	}
	for _, teamspaceID := range teamspaces {
		var revision, member int
		if err := app.DB().NewQuery(`SELECT [[revision]], (SELECT COUNT(*) FROM {{leafen_teamspace_members}}
			WHERE [[teamspace_id]] = {:teamspace} AND [[principal_type]] = 'user' AND [[principal_id]] = {:user})
			FROM {{leafen_teamspaces}} WHERE [[id]] = {:teamspace}`).
			Bind(dbx.Params{"teamspace": teamspaceID, "user": userID}).Row(&revision, &member); err != nil {
			return err
		}
		if member > 0 {
			continue
		}
		body, _ := json.Marshal(map[string]any{
			"requestId": security.RandomString(20), "action": "add_member", "teamspaceId": teamspaceID,
			"expectedRevision": revision, "principalType": "user", "principalId": userID, "memberRole": "member",
		})
		result, err := callNativeOperation(context.Background(), key, "POST", "/workspaces/"+workspace.WorkspaceID+"/teamspace-mutations", "", body, "")
		if err != nil {
			return err
		}
		if result.Status >= 300 {
			return fmt.Errorf("adding to the app's teamspace answered %d", result.Status)
		}
	}
	return nil
}

// appAccountConnection is the app account's own connection, the source of
// what the account makes; like the people's app connections it stays out of
// connection lists.
func appAccountConnection(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace) (string, error) {
	var connectionID string
	err := app.RunInTransaction(func(tx core.App) error {
		err := tx.DB().NewQuery(`SELECT [[account_connection_id]] FROM {{leafen_app_workspaces}} WHERE [[workspace_id]] = {:workspace}`).
			Bind(dbx.Params{"workspace": workspace.WorkspaceID}).Row(&connectionID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if connectionID != "" {
			return nil
		}
		connectionID = security.RandomString(15)
		now := time.Now().UnixMilli()
		if _, err := tx.DB().Insert("leafen_mcp_connections", dbx.Params{
			"id": connectionID, "owner_id": workspace.AccountID, "client_name": appDisplayName(manifest),
			"token_hash": mcpTokenHash(appDelegationPrefix + security.RandomString(48)), "created": now,
			"access_scope": "structure", "oauth_client_id": appConnectionClientID + install.ID, "app_install_id": install.ID,
		}).Execute(); err != nil {
			return err
		}
		if _, err := tx.DB().Insert("leafen_mcp_connection_workspaces", dbx.Params{
			"connection_id": connectionID, "workspace_id": workspace.WorkspaceID, "created": now,
		}).Execute(); err != nil {
			return err
		}
		_, err = tx.DB().Update(appWorkspacesTable, dbx.Params{"account_connection_id": connectionID},
			dbx.HashExp{"workspace_id": workspace.WorkspaceID}).Execute()
		return err
	})
	return connectionID, err
}

func appTenancyOrganization(tenancyKey string) string {
	if len(tenancyKey) > 4 && tenancyKey[:4] == "org:" {
		return tenancyKey[4:]
	}
	return ""
}
