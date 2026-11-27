package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Lists of another server's workspaces in the switcher (the
// multi-server decision's 2026-10-07 addition). A page another Leafen server
// serves holds a list key for this server: it reads the person's workspaces
// and apps — names, icons, whether they are a guest, whether something is
// unread — and nothing else; no other route knows these keys. A person's full
// sign-in never leaves this server's own pages. People see and take back the
// pages that hold their keys; the administrator can turn the lists off.

const (
	switcherKeyPrefix     = "lsk_"
	switcherKeySecretSize = 43
	switcherKeyUseEvery   = int64(time.Minute / time.Millisecond)
)

var errSwitcherKeyUnknown = errors.New(errorCodeSwitcherKeyUnknown)

func registerSwitcherListRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/account/switcher-keys", issueSwitcherKeyRoute)
	event.Router.GET(apiRoot+"/account/switcher-keys", listSwitcherKeysRoute)
	event.Router.DELETE(apiRoot+"/account/switcher-keys/{keyId}", revokeSwitcherKeyRoute)
	event.Router.GET(apiRoot+"/switcher/list", readSwitcherListRoute)
}

func switcherKeyHash(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

// switcherHolder is a page's origin, as the key is given to it.
func switcherHolder(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || utf8.RuneCountInString(raw) > maxSwitcherHolderRunes || parsed.Host == "" ||
		parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil ||
		strings.Trim(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return parsed.Scheme + "://" + strings.ToLower(parsed.Host), true
}

func switcherListsEnabled(app core.App) (bool, error) {
	settings, err := loadAdministratorSettings(app)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return err == nil && settings.SwitcherListsEnabled, err
}

func switcherFailure(event *core.RequestEvent, err error) error {
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeSwitcherFailed,
		"Couldn't read the workspace lists.", "switcher", "lists", errorCodeSwitcherFailed, err)
}

// issueSwitcherKeyRoute gives a page of another server a list key; a key
// given to the same page before is taken back.
func issueSwitcherKeyRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := switcherKeyIssueInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeSwitcherKeyInvalid, "Name the page's address.")
	}
	if strings.TrimSpace(input.Holder) == "" {
		return refuseMissingInput(event, errorCodeSwitcherKeyInvalid, "holder", "the address of the server whose page shows the list, as https://host.")
	}
	holder, valid := switcherHolder(input.Holder)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeSwitcherKeyInvalid, "Name the page's address.")
	}
	enabled, err := switcherListsEnabled(event.App)
	if err != nil {
		return switcherFailure(event, err)
	}
	if !enabled {
		return apiError(event, http.StatusForbidden, errorCodeSwitcherListsOff, "This server doesn't share workspace lists with other servers.")
	}
	key := switcherKeyPrefix + security.RandomStringWithAlphabet(switcherKeySecretSize, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	entry := switcherKeyResponse{ID: core.GenerateDefaultRandomId(), Holder: holder, CreatedAt: time.Now().UTC().UnixMilli()}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_switcher_keys}} SET [[revoked_at]] = {:now}
			WHERE [[user_id]] = {:user} AND [[holder]] = {:holder} AND [[revoked_at]] = 0`).
			Bind(dbx.Params{"now": entry.CreatedAt, "user": user.Id, "holder": holder}).Execute(); err != nil {
			return err
		}
		var active int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_switcher_keys}} WHERE [[user_id]] = {:user} AND [[revoked_at]] = 0`).
			Bind(dbx.Params{"user": user.Id}).Row(&active); err != nil {
			return err
		}
		if active >= switcherKeyLimit {
			// The least recently used key makes room.
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_switcher_keys}} SET [[revoked_at]] = {:now}
				WHERE [[id]] = (SELECT [[id]] FROM {{leafen_switcher_keys}} WHERE [[user_id]] = {:user} AND [[revoked_at]] = 0
					ORDER BY MAX([[used_at]], [[created]]) LIMIT 1)`).
				Bind(dbx.Params{"now": entry.CreatedAt, "user": user.Id}).Execute(); err != nil {
				return err
			}
		}
		_, err := txApp.DB().Insert("leafen_switcher_keys", dbx.Params{
			"id": entry.ID, "user_id": user.Id, "holder": holder, "key_hash": switcherKeyHash(key),
			"created": entry.CreatedAt, "used_at": 0, "revoked_at": 0,
		}).Execute()
		return err
	})
	if err != nil {
		return switcherFailure(event, err)
	}
	return event.JSON(http.StatusOK, switcherKeyIssuedResponse{Key: key, Entry: entry})
}

// listSwitcherKeysRoute lists the pages that hold the person's list keys.
func listSwitcherKeysRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	enabled, err := switcherListsEnabled(event.App)
	if err != nil {
		return switcherFailure(event, err)
	}
	rows := []struct {
		ID      string `db:"id"`
		Holder  string `db:"holder"`
		Created int64  `db:"created"`
		UsedAt  int64  `db:"used_at"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[id]], [[holder]], [[created]], [[used_at]]
		FROM {{leafen_switcher_keys}} WHERE [[user_id]] = {:user} AND [[revoked_at]] = 0 ORDER BY [[created]] DESC`).
		Bind(dbx.Params{"user": user.Id}).All(&rows); err != nil {
		return switcherFailure(event, err)
	}
	keys := []switcherKeyResponse{}
	for _, row := range rows {
		keys = append(keys, switcherKeyResponse{ID: row.ID, Holder: row.Holder, CreatedAt: row.Created, UsedAt: row.UsedAt})
	}
	return event.JSON(http.StatusOK, switcherKeyListResponse{Keys: keys, Enabled: enabled})
}

func revokeSwitcherKeyRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	result, err := event.App.DB().NewQuery(`UPDATE {{leafen_switcher_keys}} SET [[revoked_at]] = {:now}
		WHERE [[id]] = {:id} AND [[user_id]] = {:user} AND [[revoked_at]] = 0`).
		Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "id": event.Request.PathValue("keyId"), "user": user.Id}).Execute()
	if err != nil {
		return switcherFailure(event, err)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return apiError(event, http.StatusNotFound, errorCodeSwitcherKeyUnknown, "No such key.")
	}
	return event.NoContent(http.StatusNoContent)
}

// switcherListUser is the person a list key reads for, or the person signed
// in on this server (a packaged client asks with its sign-in).
func switcherListUser(event *core.RequestEvent) (*core.Record, error) {
	header := strings.TrimSpace(event.Request.Header.Get("Authorization"))
	key, bearer := strings.CutPrefix(header, "Bearer ")
	if !bearer || !strings.HasPrefix(key, switcherKeyPrefix) {
		if user, ok := authenticatedUser(event); ok {
			return user, nil
		}
		return nil, errSwitcherKeyUnknown
	}
	stored := struct {
		ID     string `db:"id"`
		UserID string `db:"user_id"`
		UsedAt int64  `db:"used_at"`
	}{}
	err := event.App.DB().NewQuery(`SELECT [[id]], [[user_id]], [[used_at]] FROM {{leafen_switcher_keys}}
		WHERE [[key_hash]] = {:hash} AND [[revoked_at]] = 0`).Bind(dbx.Params{"hash": switcherKeyHash(key)}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errSwitcherKeyUnknown
	}
	if err != nil {
		return nil, err
	}
	user, err := event.App.FindRecordById("users", stored.UserID)
	if err != nil {
		return nil, refusedUnlessStorage(errSwitcherKeyUnknown, err)
	}
	var status string
	if err := event.App.DB().NewQuery(`SELECT [[status]] FROM {{leafen_administrator_account_states}} WHERE [[user_id]] = {:user}`).
		Bind(dbx.Params{"user": user.Id}).Row(&status); err == nil && status != administratorAccountStatusActive {
		return nil, errSwitcherKeyUnknown
	}
	if now := time.Now().UTC().UnixMilli(); now-stored.UsedAt > switcherKeyUseEvery {
		_, _ = event.App.DB().NewQuery(`UPDATE {{leafen_switcher_keys}} SET [[used_at]] = {:now} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"now": now, "id": stored.ID}).Execute()
	}
	return user, nil
}

// readSwitcherListRoute answers the person's workspaces and apps here.
func readSwitcherListRoute(event *core.RequestEvent) error {
	enabled, err := switcherListsEnabled(event.App)
	if err != nil {
		return switcherFailure(event, err)
	}
	user, err := switcherListUser(event)
	if errors.Is(err, errSwitcherKeyUnknown) {
		return apiError(event, http.StatusUnauthorized, errorCodeSwitcherKeyUnknown, "The list key is unknown or was revoked.")
	}
	if err != nil {
		return switcherFailure(event, err)
	}
	if !enabled {
		return apiError(event, http.StatusForbidden, errorCodeSwitcherListsOff, "This server doesn't share workspace lists with other servers.")
	}
	// The list reads the workspaces the person sees as the app's own start
	// does, once the jobs touching them are done (2026-11-11: a workspace
	// just made or joined was left out until the projection caught up).
	if ready, err := ensureBootstrapVisibilityProjectionReadiness(
		event, user.Id, errorCodeSwitcherFailed, "Couldn't read the workspace lists.", "switcher",
	); !ready {
		return err
	}
	settings, _ := loadAdministratorSettings(event.App)
	response := switcherListResponse{
		Origin:     requestOrigin(event.Request),
		ServerName: settings.ServerName, ServerAlias: settings.ServerAlias,
		Account:    switcherListAccount{Name: user.GetString("name"), Email: user.Email(), Avatar: user.GetString("profile_avatar")},
		Workspaces: []switcherListWorkspace{}, Apps: []switcherListApp{},
	}
	rows, guests, err := loadSwitcherWorkspaces(event.App, user.Id)
	if err != nil {
		return switcherFailure(event, err)
	}
	ids := []string{}
	for index, row := range append(rows, guests...) {
		if row.AppInstallID != "" {
			continue
		}
		response.Workspaces = append(response.Workspaces, switcherListWorkspace{
			ID: row.ID, Name: row.Name, Icon: row.Icon, Guest: index >= len(rows),
		})
		ids = append(ids, row.ID)
	}
	unread, err := readWorkspaceUnread(event.App, user.Id, ids)
	if err != nil {
		return switcherFailure(event, err)
	}
	marked := map[string]bool{}
	for _, id := range unread.Unread {
		marked[id] = true
	}
	for index := range response.Workspaces {
		response.Workspaces[index].Unread = marked[response.Workspaces[index].ID]
	}
	apps, err := usableApps(event.App, user.Id)
	if err != nil {
		return switcherFailure(event, err)
	}
	for _, usable := range apps {
		// An app that only opens files has no page to switch to (2026-11-05).
		if usable.Manifest.Surfaces.FullPage == nil {
			continue
		}
		response.Apps = append(response.Apps, switcherListApp{Slug: usable.Install.Slug, Name: usable.Manifest.Name, TenancyKey: usable.TenancyKey})
	}
	return event.JSON(http.StatusOK, response)
}

// loadSwitcherWorkspaces reads every workspace the person is in, a window at a
// time, and every one they are a guest in.
func loadSwitcherWorkspaces(app core.App, userID string) ([]storedBootstrapWorkspace, []storedBootstrapWorkspace, error) {
	rows := []storedBootstrapWorkspace{}
	for cursor := (bootstrapCursor{}); ; {
		window, done, err := loadBootstrapWorkspaces(app, userID, cursor, switcherListWorkspaceLimit)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, window...)
		if done || len(window) == 0 {
			break
		}
		cursor.WorkspaceCreated, cursor.WorkspaceID = window[len(window)-1].Created, window[len(window)-1].ID
	}
	guests, err := loadBootstrapGuestWorkspaces(app, userID)
	return rows, guests, err
}
