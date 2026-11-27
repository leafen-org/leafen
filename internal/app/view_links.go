package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// View links: a short-lived, read-only address of one page, at a screen
// size, theme and language (and, for a database, one of its views), for an agent to open in its own browser and
// capture — the server runs no browser. The link's token rides after the
// address's "#", so it never reaches a server log. Unless made without one,
// it opens only with the secret made with it, given apart from the link, so
// a leaked link alone opens nothing (view_link_access.go). It shows only
// what its maker may read: each time it is used, the maker (and the key that
// made it) must still reach the page. It ends when it expires, is revoked,
// after too many wrong secrets, or — for a single-use link — a short while
// after it is first opened.

var viewLinkLanguagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

type storedViewLink struct {
	ID           string `db:"id"`
	TokenDigest  string `db:"token_digest"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	OwnerID      string `db:"owner_id"`
	ConnectionID string `db:"connection_id"`
	Viewport     string `db:"viewport"`
	Theme        string `db:"theme"`
	Language     string `db:"language"`
	SingleUse    bool   `db:"single_use"`
	ExpiresAt    int64  `db:"expires_at"`
	UsedAt       int64  `db:"used_at"`
	RevokedAt    int64  `db:"revoked_at"`
	Created      int64  `db:"created"`
	ViewID       string `db:"view_id"`
	// SecretDigest is empty for a link made without a secret.
	SecretDigest   string `db:"secret_digest"`
	FailedAttempts int    `db:"failed_attempts"`
	FileKeyDigest  string `db:"file_key_digest"`
}

func registerViewLinkRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+viewLinkCreatePath, createViewLink).Bind(apis.BodyLimit(4096))
	event.Router.DELETE(apiRoot+viewLinkRevokePath, revokeViewLink)
	event.Router.GET(apiRoot+viewLinkOpensPath, listViewLinkOpens)
}

// viewLinkSize is the screen a view link's viewport stands for, which an
// agent sizes its browser to; a wider browser shows it in a frame that size.
func viewLinkSize(viewport string) (int, int) {
	switch viewport {
	case "tablet":
		return viewLinkTabletWidth, viewLinkTabletHeight
	case "mobile":
		return viewLinkMobileWidth, viewLinkMobileHeight
	}
	return viewLinkDesktopWidth, viewLinkDesktopHeight
}

func viewLinkDigest(token string) string {
	digest := sha256.Sum256([]byte("leafen:view-link:v1\x00" + token))
	return hex.EncodeToString(digest[:])
}

func createViewLink(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := viewLinkCreateRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeViewLinkInvalid, "Check the view link.")
	}
	viewport, theme, language, viewID := input.Viewport, input.Theme, strings.TrimSpace(input.Language), strings.TrimSpace(input.ViewID)
	if viewport == "" {
		viewport = "desktop"
	}
	if theme == "" {
		theme = "system"
	}
	seconds := input.Seconds
	if seconds == 0 {
		seconds = viewLinkDefaultSeconds
	}
	singleUse := input.SingleUse == nil || *input.SingleUse
	withoutSecret := input.WithoutSecret != nil && *input.WithoutSecret
	if (viewport != "desktop" && viewport != "tablet" && viewport != "mobile") ||
		(theme != "system" && theme != "light" && theme != "dark") ||
		(language != "" && (len(language) > 16 || !viewLinkLanguagePattern.MatchString(language))) ||
		seconds < 60 || seconds > viewLinkMaxSeconds {
		return apiError(event, http.StatusBadRequest, errorCodeViewLinkInvalid,
			"Choose desktop, tablet or mobile, a theme, a language tag and 60 to 3600 seconds.")
	}
	authorization, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if err != nil {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if trashed, err := pageInTrashPath(event.App, pageID); err != nil || trashed {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	// The organization's view link rules: whether they are made at all, how
	// long one lasts, whether each opens once, and whether one may open
	// without a secret.
	policy, err := loadEnforcedSharingSecurityPolicy(event.App, authorization.Workspace.Organization.OrganizationID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeViewLinkFailed,
			"Couldn't make the view link.", "view_link", "policy", errorCodeViewLinkFailed, err)
	}
	switch {
	case policy.ViewLinks != 1:
		return apiError(event, http.StatusForbidden, errorCodeViewLinkRefused, "Your organization has turned view links off.")
	case seconds > policy.ViewLinkMaxSeconds:
		return apiError(event, http.StatusBadRequest, errorCodeViewLinkRefused,
			fmt.Sprintf("Your organization lets a view link last at most %d seconds.", policy.ViewLinkMaxSeconds))
	case policy.ViewLinkSingleUseOnly == 1 && !singleUse:
		return apiError(event, http.StatusBadRequest, errorCodeViewLinkRefused, "Your organization opens each view link only once.")
	case withoutSecret && policy.ViewLinkWithoutSecret != 1:
		return apiError(event, http.StatusBadRequest, errorCodeViewLinkRefused, "Your organization makes every view link open with its secret.")
	}
	// A view names one of the databases' views on this page.
	if viewID != "" {
		var found int
		if err := event.App.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_views}}
			WHERE [[id]] = {:view} AND [[page_id]] = {:page} AND [[workspace_id]] = {:workspace}`).Bind(dbx.Params{
			"view": viewID, "page": pageID, "workspace": authorization.Workspace.WorkspaceID,
		}).Row(&found); err != nil || found != 1 {
			return apiError(event, http.StatusBadRequest, errorCodeViewLinkInvalid, "Choose a view of a database on this page.")
		}
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeViewLinkFailed, "Couldn't make the view link.", err)
	}
	token, err := newAuthProviderToken("lvl_", 32)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeViewLinkFailed, "Couldn't make the view link.", err)
	}
	connectionID := ""
	if key, keyed := userKeyOf(event); keyed {
		connectionID = key.ConnectionID
	}
	now := time.Now().UTC()
	link := storedViewLink{
		ID: eventWebhookID(), TokenDigest: viewLinkDigest(token), WorkspaceID: authorization.Workspace.WorkspaceID,
		PageID: pageID, OwnerID: user.Id, ConnectionID: connectionID, Viewport: viewport, Theme: theme,
		Language: language, SingleUse: singleUse, ExpiresAt: now.Add(time.Duration(seconds) * time.Second).UnixMilli(),
		Created: now.UnixMilli(), ViewID: viewID,
	}
	secret := ""
	if !withoutSecret {
		if secret, err = newAuthProviderToken("lvs_", 24); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeViewLinkFailed, "Couldn't make the view link.", err)
		}
		link.SecretDigest = viewLinkSecretDigest(secret)
	}
	fileKey, err := viewLinkFileKey(event.App, link.ID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeViewLinkFailed, "Couldn't make the view link.", err)
	}
	single := 0
	if singleUse {
		single = 1
	}
	if _, err := event.App.DB().Insert("leafen_view_links", dbx.Params{
		"id": link.ID, "token_digest": link.TokenDigest, "workspace_id": link.WorkspaceID, "page_id": pageID,
		"owner_id": user.Id, "connection_id": connectionID, "viewport": viewport, "theme": theme, "language": language,
		"single_use": single, "expires_at": link.ExpiresAt, "created": link.Created, "view_id": viewID,
		"secret_digest": link.SecretDigest, "file_key_digest": viewLinkFileKeyDigest(fileKey),
	}).Execute(); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeViewLinkFailed,
			"Couldn't make the view link.", "view_link", "create", errorCodeViewLinkFailed, err)
	}
	_, _ = event.App.DB().NewQuery(`DELETE FROM {{leafen_view_links}} WHERE [[expires_at]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-24 * time.Hour).UnixMilli()}).Execute()
	width, height := viewLinkSize(viewport)
	// Its page's shape and its screen ride beside the token, for the screen shown while the page comes.
	address := origin + "/view#token=" + token + publicOpeningShapeViewSuffix(event.App, pageID, viewport)
	return event.JSON(http.StatusCreated, viewLinkResponse{
		ID: link.ID, URL: address, PageID: pageID, Viewport: viewport, Width: width, Height: height, Theme: theme,
		Language: language, SingleUse: singleUse, ExpiresAt: link.ExpiresAt, ViewID: viewID,
		Secret: secret, SecretRequired: secret != "",
	})
}

func revokeViewLink(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	result, err := event.App.DB().NewQuery(`UPDATE {{leafen_view_links}} SET [[revoked_at]] = {:now}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:owner} AND [[revoked_at]] = 0`).Bind(dbx.Params{
		"now": time.Now().UTC().UnixMilli(), "id": strings.TrimSpace(event.Request.PathValue("viewLinkId")), "owner": user.Id,
	}).Execute()
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeViewLinkFailed,
			"Couldn't revoke the view link.", "view_link", "revoke", errorCodeViewLinkFailed, err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return apiError(event, http.StatusNotFound, errorCodeViewLinkNotFound, "View link not found.")
	}
	return event.NoContent(http.StatusNoContent)
}

// authorizeViewLink lets a view link show its page while it holds — given
// its secret, when it was made with one (a wrong one is counted before this,
// in admitViewLinkSecret, outside the read's transaction) — and starts its
// single use. It shows its one page only, and names the key its files load
// with.
func authorizeViewLink(app core.App, token, secret string, pageIDs []string, now time.Time) (publicVisibilityAuthorization, *publicViewPresentation, error) {
	link := storedViewLink{}
	err := app.DB().NewQuery(`SELECT * FROM {{leafen_view_links}} WHERE [[token_digest]] = {:digest}`).
		Bind(dbx.Params{"digest": viewLinkDigest(token)}).One(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return publicVisibilityAuthorization{}, nil, errPublicVisibilityNotFound
	}
	if err != nil {
		return publicVisibilityAuthorization{}, nil, err
	}
	for _, pageID := range pageIDs {
		if pageID != link.PageID {
			return publicVisibilityAuthorization{}, nil, errPublicVisibilityNotFound
		}
	}
	authorization, err := viewLinkStillShows(app, link, now)
	if err != nil {
		return publicVisibilityAuthorization{}, nil, err
	}
	if link.SecretDigest != "" {
		if secret == "" {
			return publicVisibilityAuthorization{}, nil, errViewLinkSecretRequired
		}
		if !viewLinkSecretMatches(link, secret) {
			return publicVisibilityAuthorization{}, nil, errViewLinkSecretWrong
		}
	}
	nowMillis := now.UnixMilli()
	if link.SingleUse && link.UsedAt == 0 {
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_view_links}} SET [[used_at]] = {:now} WHERE [[id]] = {:id} AND [[used_at]] = 0`).
			Bind(dbx.Params{"now": nowMillis, "id": link.ID}).Execute(); err != nil {
			return publicVisibilityAuthorization{}, nil, err
		}
	}
	fileKey, err := viewLinkFileKey(app, link.ID)
	if err != nil {
		return publicVisibilityAuthorization{}, nil, err
	}
	return authorization, &publicViewPresentation{
		Viewport: link.Viewport, Theme: link.Theme, Language: link.Language, ExpiresAt: link.ExpiresAt, ViewID: link.ViewID,
		FileKey: fileKey,
	}, nil
}
