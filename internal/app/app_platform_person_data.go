package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Each person's own data (docs/app-platform.md 6): data entries marked
// perPerson are made for a person the first time the app is reached for them
// in an organization (readyAppPersonData) — as that person, so a private
// page of theirs in the app's workspace holds it (titled with the app's
// name; {{root}} in its steps), named in their language, and only they see
// it. A new version's new per-person steps are made the next time; what was
// made stays as it is.

const appDataPlacePerson = "person"

// appDataFilePerPerson says a setup file holds per-person steps.
func appDataFilePerPerson(manifest appManifest, file string) bool {
	for _, data := range manifest.Data {
		if data.File == file && data.PerPerson {
			return true
		}
	}
	return false
}

func loadAppPersonDataSteps(app core.App, packageHash string, manifest appManifest) ([]appDataStep, error) {
	steps := []appDataStep{}
	done := map[string]bool{}
	for _, data := range manifest.Data {
		if !data.PerPerson || done[data.File] {
			continue
		}
		done[data.File] = true
		raw, err := loadAppPackageFile(app, packageHash, data.File)
		if err != nil {
			return nil, err
		}
		setup, err := parseAppDataSetup(raw)
		if err != nil {
			return nil, err
		}
		steps = append(steps, setup.Steps...)
	}
	return steps, nil
}

// runAppPersonData makes what is missing of the person's own data, and
// keeps whether it could for the app's administrators
// (app_platform_data_failure.go): a failure shows on the app's row until the
// next time the app is reached for them makes it.
func runAppPersonData(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace, userID, connectionID string) error {
	defer appDataTurn(workspace.WorkspaceID + "/" + userID)()
	err := makeAppPersonData(app, install, manifest, workspace, userID, connectionID)
	return errors.Join(err, noteAppPersonDataOutcome(app, install, workspace.TenancyKey, userID, err))
}

func makeAppPersonData(app core.App, install storedAppInstall, manifest appManifest, workspace storedAppWorkspace, userID, connectionID string) error {
	steps, err := loadAppPersonDataSteps(app, install.PackageHash, manifest)
	if err != nil || len(steps) == 0 {
		return err
	}
	rows := []struct {
		Key    string `db:"data_key"`
		Page   string `db:"page_id"`
		Source string `db:"data_source_id"`
		View   string `db:"view_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[data_key]], [[page_id]], [[data_source_id]], [[view_id]] FROM {{leafen_app_person_data}}
		WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy} AND [[user_id]] = {:user}`).
		Bind(dbx.Params{"install": install.ID, "tenancy": workspace.TenancyKey, "user": userID}).All(&rows); err != nil {
		return err
	}
	results := map[string]map[string]string{}
	for _, row := range rows {
		results[row.Key] = map[string]string{"id": row.Page, "data_source_id": row.Source, "view_id": row.View}
	}
	missing := false
	for _, step := range steps {
		if _, made := results[step.Key]; !made {
			missing = true
		}
	}
	if !missing {
		return nil
	}
	name := appNameFor(app, manifest, userID)
	personLanguage, _ := serverTextLanguage(app, userID, nil)
	_, rootTitles := appPlaceTitles(name, personLanguage)
	record := func(key, pageID, sourceID, viewID, spec string) error {
		_, err := app.DB().NewQuery(`INSERT OR REPLACE INTO {{leafen_app_person_data}}
			([[install_id]], [[tenancy_key]], [[user_id]], [[data_key]], [[page_id]], [[data_source_id]], [[view_id]], [[spec]], [[created]])
			VALUES ({:install}, {:tenancy}, {:user}, {:key}, {:page}, {:source}, {:view}, {:spec}, {:now})`).Bind(dbx.Params{
			"install": install.ID, "tenancy": workspace.TenancyKey, "user": userID, "key": key, "page": pageID,
			"source": sourceID, "view": viewID, "spec": spec, "now": time.Now().UnixMilli(),
		}).Execute()
		return err
	}
	// The person's private root page, made as them.
	root := results["root-person"]["id"]
	if root == "" {
		key := userKeyAuthority{
			ConnectionID: connectionID, Name: name, OwnerID: userID, Scope: userKeyScopeStructure,
			WorkspaceIDs: []string{workspace.WorkspaceID}, Created: time.Now().UnixMilli(), AppInstallID: install.ID,
		}
		digest := sha256.Sum256([]byte(install.ID + "\x00" + workspace.TenancyKey + "\x00" + userID + "\x00person-root"))
		body, _ := json.Marshal(map[string]any{"requestId": "app-person-root-" + hex.EncodeToString(digest[:12]), "workspaceId": workspace.WorkspaceID, "title": rootTitles[appDataPlacePerson]})
		result, err := callNativeOperation(context.Background(), key, "POST", "/pages", "", body, "")
		if err != nil {
			return err
		}
		if result.Status >= 300 {
			return fmt.Errorf("the person's page answered %d", result.Status)
		}
		answer, _ := result.Body.(map[string]any)
		page, _ := answer["page"].(map[string]any)
		if root, _ = page["id"].(string); root == "" {
			return errors.New("the person's page wasn't made")
		}
		if err := record("root-person", root, "", "", ""); err != nil {
			return err
		}
	}
	scoped := []mcpGovernanceWorkspace{{ID: workspace.WorkspaceID, OrganizationID: appTenancyOrganization(workspace.TenancyKey), Name: name}}
	session := appDataSession{connectionID: connectionID, mcp: context.WithValue(context.Background(), mcpAuthorityContextKey{}, mcpAuthority{
		ConnectionID: connectionID, Resource: "leafen-app", PrincipalID: userID,
		ClientID: appConnectionClientID + install.ID, ClientName: name, Scope: userKeyScopeStructure,
		ScopedWorkspaces: scoped, ApprovedWorkspaces: scoped, WorkspaceIDs: []string{workspace.WorkspaceID},
	})}
	for _, step := range steps {
		if _, made := results[step.Key]; made {
			continue
		}
		// Its names in the person's language, as their page's title.
		args, spec := appDataMakeArgs(step, personLanguage)
		made, err := session.runStep(app, step, fillAppDataPlaceholders(args, root, results))
		if err != nil {
			return err
		}
		results[step.Key] = made
		if err := record(step.Key, made["id"], made["data_source_id"], made["view_id"], spec); err != nil {
			return err
		}
	}
	return nil
}

// readyAppPersonData makes what is missing of the person's own data wherever
// the app is reached for them — an endpoint they sign in to, an agent's tool
// — as their first opening of its screen does: made only there, a clip sent
// before the Web Clips app was ever opened was refused for want of the
// person's database (2026-10-16). A failure is kept for the app's
// administrators (runAppPersonData) and tried again the next time.
func readyAppPersonData(app core.App, usable usableApp, userID string) {
	if userID == "" || usable.Manifest.PersonAccess == "" || !appManifestHasPersonData(usable.Manifest) {
		return
	}
	err := func() error {
		// Only what is missing goes on: readying the connection writes, and a
		// WebDAV client sends many requests a minute.
		if missing, err := appPersonDataMissing(app, usable, userID); err != nil || !missing {
			return err
		}
		workspace, err := appWorkspaceForPerson(app, usable, userID)
		if err != nil {
			return err
		}
		workspaces, err := appPersonWorkspaces(app, userID, usable.TenancyKey)
		if err != nil {
			return err
		}
		connectionID, err := appConnection(app, usable.Install, usable.Manifest, usable.TenancyKey, userID, workspaces)
		if err != nil {
			return err
		}
		return runAppPersonData(app, usable.Install, usable.Manifest, workspace, userID, connectionID)
	}()
	if err != nil {
		app.Logger().Warn("leafen app person data failed", "install", usable.Install.ID, "error", err)
	}
}

// appPersonStepKeys are each package's per-person steps' keys, by its hash:
// a package's files never change.
var appPersonStepKeys sync.Map

// appPersonDataMissing says a per-person step has made nothing for the person yet.
func appPersonDataMissing(app core.App, usable usableApp, userID string) (bool, error) {
	keys, known := appPersonStepKeys.Load(usable.Install.PackageHash)
	if !known {
		steps, err := loadAppPersonDataSteps(app, usable.Install.PackageHash, usable.Manifest)
		if err != nil {
			return false, err
		}
		names := make([]string, 0, len(steps))
		for _, step := range steps {
			if !slices.Contains(names, step.Key) {
				names = append(names, step.Key)
			}
		}
		keys, _ = appPersonStepKeys.LoadOrStore(usable.Install.PackageHash, names)
	}
	names := keys.([]string)
	if len(names) == 0 {
		return false, nil
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return false, err
	}
	var made int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_person_data}}
		WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy} AND [[user_id]] = {:user}
			AND [[data_key]] IN (SELECT [[value]] FROM json_each({:keys}))`).
		Bind(dbx.Params{"install": usable.Install.ID, "tenancy": usable.TenancyKey, "user": userID, "keys": string(encoded)}).Row(&made); err != nil {
		return false, err
	}
	return made < len(names), nil
}

// appManifestHasPersonData says the app keeps data for each person.
func appManifestHasPersonData(manifest appManifest) bool {
	for _, data := range manifest.Data {
		if data.PerPerson {
			return true
		}
	}
	return false
}

// appWorkspaceForPerson is the app's workspace in the tenancy, readied for
// the person when it isn't there yet, with the person among its members as
// opening the app makes them (2026-10-18: a member who used the drive only
// through WebDAV had a connection but no place in its workspace — no "My
// files", the shared folder unreadable, a file refused).
func appWorkspaceForPerson(app core.App, usable usableApp, userID string) (storedAppWorkspace, error) {
	workspaces, err := loadAppWorkspaces(app, usable.Install.ID)
	if err != nil {
		return storedAppWorkspace{}, err
	}
	for _, workspace := range workspaces {
		if workspace.TenancyKey == usable.TenancyKey {
			return workspace, ensureAppWorkspaceMember(app, workspace, userID)
		}
	}
	return prepareAppForPerson(app, usable, userID)
}
