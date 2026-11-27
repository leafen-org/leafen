package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An app's screen acts on a person's data with a short-lived token its host
// page holds and never hands to the app (docs/app-platform.md 4). The token
// stands for one connection per person, app and organization, so what the
// app changes is the person's "through the app" and is undone with the
// connection; it reaches what the person may, within the scope the app was
// approved for and the workspaces of that organization. The app's files are
// served under a signed ticket that names the installed version.

const (
	appDelegationPrefix    = "lap_"
	appDelegationLifetime  = 10 * time.Minute
	appFrameTicketLifetime = 12 * time.Hour
	appConnectionClientID  = "leafen-app:"
)

var errAppNotUsable = errors.New(errorCodeAppInstallNotFound)

// errAppStopped is a call an app makes, or that reaches it, while an
// administrator has it stopped or its catalog has revoked its version. It
// isn't the person's key that is wrong.
var errAppStopped = errors.New(errorCodeAppStopped)

const appStoppedMessage = "This app is stopped on this server. Only an administrator can bring it back."

// An app reaches outside Leafen only through the addresses an administrator
// approved (docs/app-platform.md 7, T7): its token can't make the server
// fetch an address, deliver to one, or open content to people outside.
var appTokenClosedPaths = []*regexp.Regexp{
	regexp.MustCompile(`^/url-metadata$`),
	regexp.MustCompile(`^/workspaces/[^/]+/webhooks(/.*)?$`),
	regexp.MustCompile(`^/pages/[^/]+/databases/[^/]+/automations(/.*)?$`),
	regexp.MustCompile(`^/workspaces/[^/]+/(notion-import|onboarding/notion-import)(/.*)?$`),
	regexp.MustCompile(`^/pages/[^/]+/(view-links|public-mutations|share-inheritance|app-connections|agent-grant-scope)(/.*)?$`),
	regexp.MustCompile(`^/view-links/.*$`),
	regexp.MustCompile(`^/forms(/.*)?$`),
}

func appTokenClosedPath(path string) bool {
	for _, pattern := range appTokenClosedPaths {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

type usableApp struct {
	Install     storedAppInstall
	Manifest    appManifest
	TenancyKey  string
	TenancyName string
}

// appPersonWorkspaces lists the workspaces of a tenancy the person owns or
// is a member of.
func appPersonWorkspaces(app core.App, userID, tenancyKey string) ([]string, error) {
	organizationID := ""
	if strings.HasPrefix(tenancyKey, "org:") {
		organizationID = strings.TrimPrefix(tenancyKey, "org:")
	} else if tenancyKey != appTenancyServer {
		return nil, nil
	}
	ids := []string{}
	query := strings.ReplaceAll(`
		SELECT [[workspace]].[[id]] FROM {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_workspace_memberships}} [[membership]]
			ON [[membership]].[[workspace_id]] = [[workspace]].[[id]] AND [[membership]].[[user_id]] = {:userId}
			AND [[membership]].[[role]] = 'member'
		WHERE [[workspace]].[[deletion_job_id]] = '' AND [[workspace]].[[organization]] = {:organizationId}
			AND (__OWNER__ = {:userId} OR [[membership]].[[user_id]] IS NOT NULL)
		ORDER BY [[workspace]].[[id]]`, "__OWNER__", workspacePermissionOwnerSQL(app, "[[workspace]]"))
	err := app.DB().NewQuery(query).Bind(dbx.Params{"userId": userID, "organizationId": organizationID}).Column(&ids)
	return ids, err
}

// appPersonOrdinaryWorkspaces are the person's workspaces in a tenancy
// other than apps' own: what makes them one of the organization's people
// for its apps. An app's workspace follows them and never stands in for them.
func appPersonOrdinaryWorkspaces(app core.App, userID, tenancyKey string) ([]string, error) {
	workspaces, err := appPersonWorkspaces(app, userID, tenancyKey)
	if err != nil || len(workspaces) == 0 {
		return workspaces, err
	}
	encoded, _ := json.Marshal(workspaces)
	ordinary := []string{}
	err = app.DB().NewQuery(`SELECT [[value]] FROM json_each({:ids}) WHERE [[value]] NOT IN (SELECT [[workspace_id]] FROM {{leafen_app_workspaces}})
		ORDER BY [[value]]`).Bind(dbx.Params{"ids": string(encoded)}).Column(&ordinary)
	return ordinary, err
}

// appGranted reports whether a grant covers the person: everyone, the
// person, or a group they are in now.
func appGranted(app core.App, installID, tenancyKey, userID string) (bool, error) {
	subjects, err := appGrantSubjects(app, tenancyKey, userID)
	if err != nil {
		return false, err
	}
	var count int
	err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_grants}} WHERE [[install_id]] = {:installId}
		AND [[tenancy_key]] = {:tenancy} AND [[subject]] IN (SELECT [[value]] FROM json_each({:subjects}))`).
		Bind(dbx.Params{"installId": installID, "tenancy": tenancyKey, "subjects": subjects}).Row(&count)
	return count > 0, err
}

// usableApps lists the running apps the person may use, once for each
// organization they have workspaces in and were granted the app.
func usableApps(app core.App, userID string) ([]usableApp, error) {
	return grantedApps(app, userID, "[[status]] = 'active'")
}

// appStoppedFor says whether an app the person was granted is stopped: they
// hear so, as an open app and an agent do, and anyone else that there is no
// such app.
func appStoppedFor(app core.App, userID, installID string) bool {
	stopped, err := grantedApps(app, userID, "[[status]] != 'active' AND [[removed_at]] = 0")
	return err == nil && slices.ContainsFunc(stopped, func(usable usableApp) bool { return usable.Install.ID == installID })
}

// grantedApps lists the installs that where names which the person was
// granted, once for each organization they have workspaces in.
func grantedApps(app core.App, userID, where string) ([]usableApp, error) {
	installs, err := loadAppInstalls(app, where, dbx.Params{})
	if err != nil || len(installs) == 0 {
		return nil, err
	}
	tenancies, err := appTenancies(app)
	if err != nil {
		return nil, err
	}
	reachable := []appTenancyWire{}
	for _, tenancy := range tenancies {
		workspaces, err := appPersonOrdinaryWorkspaces(app, userID, tenancy.Key)
		if err != nil {
			return nil, err
		}
		if len(workspaces) > 0 {
			reachable = append(reachable, tenancy)
		}
	}
	apps := []usableApp{}
	for _, install := range installs {
		_, manifest, err := loadAppPackage(app, install.PackageHash)
		if err != nil {
			return nil, err
		}
		for _, tenancy := range reachable {
			granted, err := appGranted(app, install.ID, tenancy.Key, userID)
			if err != nil {
				return nil, err
			}
			if granted {
				apps = append(apps, usableApp{Install: install, Manifest: manifest, TenancyKey: tenancy.Key, TenancyName: tenancy.Name})
			}
		}
	}
	return apps, nil
}

// appConnection is the person's connection for the app in the tenancy,
// made on first use (again after a revoke) and pointed at their workspaces
// there each time a token is made.
func appConnection(app core.App, install storedAppInstall, manifest appManifest, tenancyKey, userID string, workspaceIDs []string) (string, error) {
	var connectionID string
	err := app.RunInTransaction(func(tx core.App) error {
		err := tx.DB().NewQuery(`SELECT [[connection]].[[id]] FROM {{leafen_app_connections}} [[link]]
			JOIN {{leafen_mcp_connections}} [[connection]] ON [[connection]].[[id]] = [[link]].[[connection_id]]
			WHERE [[link]].[[install_id]] = {:installId} AND [[link]].[[tenancy_key]] = {:tenancy} AND [[link]].[[user_id]] = {:userId}
				AND [[connection]].[[revoked_at]] = 0`).
			Bind(dbx.Params{"installId": install.ID, "tenancy": tenancyKey, "userId": userID}).Row(&connectionID)
		now := time.Now().UnixMilli()
		if errors.Is(err, sql.ErrNoRows) {
			connectionID = security.RandomString(15)
			if _, err := tx.DB().Insert("leafen_mcp_connections", dbx.Params{
				// Named as the person reads the app, which the versions it makes show (2026-10-19: "Drive" beside "드라이브").
				"id": connectionID, "owner_id": userID, "client_name": appNameFor(tx, manifest, userID),
				// The connection is reached only through the app's tokens; its own key is never handed out.
				"token_hash": mcpTokenHash(appDelegationPrefix + security.RandomString(48)), "created": now,
				"access_scope": appConnectionScope(manifest), "oauth_client_id": appConnectionClientID + install.ID,
				// Lists of a person's MCP connections and keys leave app connections out.
				"app_install_id": install.ID,
			}).Execute(); err != nil {
				return err
			}
			if _, err := tx.DB().NewQuery(`INSERT OR REPLACE INTO {{leafen_app_connections}}
				([[install_id]], [[tenancy_key]], [[user_id]], [[connection_id]], [[created]])
				VALUES ({:installId}, {:tenancy}, {:userId}, {:connectionId}, {:now})`).Bind(dbx.Params{
				"installId": install.ID, "tenancy": tenancyKey, "userId": userID, "connectionId": connectionID, "now": now,
			}).Execute(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if _, err := tx.DB().Update("leafen_mcp_connections", dbx.Params{
			"client_name": appNameFor(tx, manifest, userID), "access_scope": appConnectionScope(manifest),
		}, dbx.HashExp{"id": connectionID}).Execute(); err != nil {
			return err
		}
		if _, err := tx.DB().Delete("leafen_mcp_connection_workspaces", dbx.HashExp{"connection_id": connectionID}).Execute(); err != nil {
			return err
		}
		for _, workspaceID := range workspaceIDs {
			if _, err := tx.DB().Insert("leafen_mcp_connection_workspaces", dbx.Params{
				"connection_id": connectionID, "workspace_id": workspaceID, "created": now,
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	return connectionID, err
}

type appSession struct {
	App       usableApp
	Workspace storedAppWorkspace
	Token     string
	ExpiresAt int64
	Ticket    string
}

// mintAppSession opens the app for the person in a tenancy: a ticket for its
// files and, when it may reach their data, a token for the host to use. An
// opened-file app gets a token only on the file it opened, held to it.
func mintAppSession(app core.App, userID, installID, tenancyKey string, opened appOpenedFile) (appSession, error) {
	apps, err := usableApps(app, userID)
	if err != nil {
		return appSession{}, err
	}
	session := appSession{}
	found := false
	for _, candidate := range apps {
		if candidate.Install.ID == installID && (tenancyKey == "" || candidate.TenancyKey == tenancyKey) {
			session.App, found = candidate, true
			break
		}
	}
	if !found {
		return appSession{}, errAppNotUsable
	}
	now := time.Now()
	if session.Ticket, err = appFrameTicketFor(app, installID, session.App.Install.PackageHash, now.Add(appFrameTicketLifetime).Unix(),
		appStrictFor(app, session.App.TenancyKey)); err != nil {
		return appSession{}, err
	}
	if session.Workspace, err = prepareAppForPerson(app, session.App, userID); err != nil {
		return appSession{}, err
	}
	if session.App.Manifest.PersonAccess == "" {
		return session, nil
	}
	filePageID, fileBlocks := "", "[]"
	if session.App.Manifest.PersonAccess == appOpenedFileAccess {
		blockID, ok := appOpenedFileBlock(app, userID, opened)
		if opened.PageID == "" || !ok {
			return session, nil
		}
		filePageID, fileBlocks = opened.PageID, `["`+blockID+`"]`
	}
	workspaces, err := appPersonWorkspaces(app, userID, session.App.TenancyKey)
	if err != nil {
		return appSession{}, err
	}
	connectionID, err := appConnection(app, session.App.Install, session.App.Manifest, session.App.TenancyKey, userID, workspaces)
	if err != nil {
		return appSession{}, err
	}
	// The person's own data is made at their first opening; a failure is
	// kept for the app's administrators (runAppPersonData) and tried again
	// at the next one, and the app still opens.
	if err := runAppPersonData(app, session.App.Install, session.App.Manifest, session.Workspace, userID, connectionID); err != nil {
		app.Logger().Warn("leafen app person data failed", "install", installID, "error", err)
	}
	session.Token = appDelegationPrefix + security.RandomString(48)
	session.ExpiresAt = now.Add(appDelegationLifetime).UnixMilli()
	if _, err := app.DB().Delete(appDelegationsTable, dbx.NewExp("[[expires_at]] < {:now}", dbx.Params{"now": now.UnixMilli()})).Execute(); err != nil {
		return appSession{}, err
	}
	_, err = app.DB().Insert(appDelegationsTable, dbx.Params{
		"token_hash": mcpTokenHash(session.Token), "connection_id": connectionID, "install_id": installID,
		"tenancy_key": session.App.TenancyKey, "user_id": userID, "expires_at": session.ExpiresAt, "created": now.UnixMilli(),
		"file_page_id": filePageID, "file_block_ids": fileBlocks,
	}).Execute()
	return session, err
}

// prepareAppForPerson readies the app's workspace in the tenancy, with the
// person among its members, its data and the app's account, as the app's
// screen or an agent first reaches it.
func prepareAppForPerson(app core.App, usable usableApp, userID string) (storedAppWorkspace, error) {
	workspace, err := ensureAppWorkspace(app, usable.Install, usable.Manifest, usable.TenancyKey, userID)
	if err != nil {
		return storedAppWorkspace{}, err
	}
	if err := ensureAppWorkspaceMember(app, workspace, userID); err != nil {
		return storedAppWorkspace{}, err
	}
	// The app's data is made once; a failure is kept and tried again at the
	// next opening, and the app still opens.
	if err := runAppDataSetup(app, usable.Install, usable.Manifest, workspace); err != nil {
		app.Logger().Warn("leafen app data setup failed", "install", usable.Install.ID, "error", err)
	} else if connectionID, err := appAccountConnection(app, usable.Install, usable.Manifest, workspace); err != nil {
		return storedAppWorkspace{}, err
	} else if err := joinAppTeamspaces(app, workspace, connectionID, userID); err != nil {
		app.Logger().Warn("leafen app teamspace join failed", "install", usable.Install.ID, "error", err)
	}
	return workspace, nil
}

// appDelegationAuthority reads an app token as a key of the person's app
// connection, while the token is fresh, the app runs, the person is still
// granted it and may act, and has workspaces in the tenancy.
func appDelegationAuthority(app core.App, token string) (userKeyAuthority, bool, error) {
	stored := struct {
		ConnectionID string `db:"connection_id"`
		InstallID    string `db:"install_id"`
		TenancyKey   string `db:"tenancy_key"`
		UserID       string `db:"user_id"`
		ExpiresAt    int64  `db:"expires_at"`
		Created      int64  `db:"created"`
		Name         string `db:"client_name"`
		Scope        string `db:"access_scope"`
		Status       string `db:"status"`
		RemovedAt    int64  `db:"removed_at"`
		FilePageID   string `db:"file_page_id"`
		FileBlocks   string `db:"file_block_ids"`
	}{}
	err := app.DB().NewQuery(`SELECT [[delegation]].[[connection_id]], [[delegation]].[[install_id]], [[delegation]].[[tenancy_key]],
			[[delegation]].[[user_id]], [[delegation]].[[expires_at]], [[connection]].[[created]], [[connection]].[[client_name]],
			[[connection]].[[access_scope]], [[install]].[[status]], [[install]].[[removed_at]], [[delegation]].[[file_page_id]], [[delegation]].[[file_block_ids]]
		FROM {{leafen_app_delegations}} [[delegation]]
		JOIN {{leafen_mcp_connections}} [[connection]] ON [[connection]].[[id]] = [[delegation]].[[connection_id]] AND [[connection]].[[revoked_at]] = 0
		JOIN {{leafen_app_installs}} [[install]] ON [[install]].[[id]] = [[delegation]].[[install_id]]
		WHERE [[delegation]].[[token_hash]] = {:hash}`).Bind(dbx.Params{"hash": mcpTokenHash(token)}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return userKeyAuthority{}, false, nil
	}
	if err != nil {
		return userKeyAuthority{}, false, err
	}
	scope, known := parseUserKeyScope(stored.Scope)
	// A token held to an opened file saves it, and reaches nothing else.
	var openedBlocks []string
	if stored.FilePageID != "" {
		known = known && json.Unmarshal([]byte(stored.FileBlocks), &openedBlocks) == nil && len(openedBlocks) > 0
	}
	if !known || stored.RemovedAt != 0 || stored.ExpiresAt <= time.Now().UnixMilli() {
		return userKeyAuthority{}, false, nil
	}
	if stored.Status != "active" {
		return userKeyAuthority{}, false, errAppStopped
	}
	if granted, err := appGranted(app, stored.InstallID, stored.TenancyKey, stored.UserID); err != nil || !granted {
		return userKeyAuthority{}, false, err
	}
	if active, err := accountMayAct(app, stored.UserID); err != nil || !active {
		return userKeyAuthority{}, false, err
	}
	workspaces, err := appPersonWorkspaces(app, stored.UserID, stored.TenancyKey)
	if err != nil || len(workspaces) == 0 {
		return userKeyAuthority{}, false, err
	}
	authority := userKeyAuthority{
		ConnectionID: stored.ConnectionID, Name: stored.Name, OwnerID: stored.UserID, Scope: scope, WorkspaceIDs: workspaces,
		Created: stored.Created, ExpiresAt: stored.ExpiresAt, AppInstallID: stored.InstallID, TenancyKey: stored.TenancyKey,
	}
	if stored.FilePageID != "" {
		authority.OpenedPageID, authority.OpenedBlockIDs, authority.DelegationHash = stored.FilePageID, openedBlocks, mcpTokenHash(token)
	}
	return authority, true, nil
}

// appFrameTicket signs which installed version's files a frame may load and
// until when; the files' paths stay under it.
func appFrameTicket(app core.App, installID, packageHash string, expires int64) (string, error) {
	return appFrameTicketFor(app, installID, packageHash, expires, false)
}

// appFrameTicketFor also signs whether the frame is under strict mode, so
// its policy can't be loosened by loading its files at another address.
func appFrameTicketFor(app core.App, installID, packageHash string, expires int64, strict bool) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	payload := installID + "." + packageHash + "." + strconv.FormatInt(expires, 10)
	if strict {
		payload += ".s"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-app-frame\n" + payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18]), nil
}

// readAppFrameTicket returns the install a valid, unexpired ticket names,
// while that install still runs the version it names.
func readAppFrameTicket(app core.App, ticket string) (storedAppInstall, error) {
	install, _, err := readAppFrameTicketStrict(app, ticket)
	return install, err
}

// readAppFrameTicketStrict also says whether the frame is under strict mode.
func readAppFrameTicketStrict(app core.App, ticket string) (storedAppInstall, bool, error) {
	encodedPayload, encodedMAC, ok := strings.Cut(ticket, ".")
	payload, payloadErr := base64.RawURLEncoding.DecodeString(encodedPayload)
	if !ok || payloadErr != nil {
		return storedAppInstall{}, false, errAppNotUsable
	}
	parts := strings.Split(string(payload), ".")
	strict := len(parts) == 4 && parts[3] == "s"
	if len(parts) != 3 && !strict {
		return storedAppInstall{}, false, errAppNotUsable
	}
	expires, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || expires < time.Now().Unix() {
		return storedAppInstall{}, false, errAppNotUsable
	}
	expected, err := appFrameTicketFor(app, parts[0], parts[1], expires, strict)
	if err != nil {
		return storedAppInstall{}, false, err
	}
	if !hmac.Equal([]byte(expected), []byte(encodedPayload+"."+encodedMAC)) {
		return storedAppInstall{}, false, errAppNotUsable
	}
	install, err := loadAppInstall(app, parts[0])
	if err != nil || install.Status != "active" || install.PackageHash != parts[1] {
		return storedAppInstall{}, false, refusedUnlessStorage(errAppNotUsable, err)
	}
	return install, strict, nil
}

// appDisplayName is the app's name for lists outside it: English, else the
// first language it names.
func appDisplayName(manifest appManifest) string {
	if name := manifest.Name["en"]; name != "" {
		return name
	}
	first := ""
	for locale := range manifest.Name {
		if first == "" || locale < first {
			first = locale
		}
	}
	return manifest.Name[first]
}
