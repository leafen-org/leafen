package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A view link made with a secret opens only with it: an automated browser
// sends it in every request's Leafen-View-Secret header, and anyone else
// types it into the page, which keeps it in that tab's memory only. Each
// wrong secret counts against the link, and enough of them close it. Every
// open, and every wrong secret, goes into the organization's audit log
// under the link, where its maker and the organization's admins read it.

const viewLinkSecretHeader = "Leafen-View-Secret"

var (
	errViewLinkSecretRequired = errors.New(errorCodeViewLinkSecretRequired)
	errViewLinkSecretWrong    = errors.New(errorCodeViewLinkSecretWrong)
)

// Outcomes of opening a view link, as its audit events name them.
const (
	viewLinkOpened      = "opened"
	viewLinkSecretWrong = "secret_wrong"
	viewLinkClosed      = "closed"
)

const viewLinkAuditSubject = "view_link"

func viewLinkSecretDigest(secret string) string {
	digest := sha256.Sum256([]byte("leafen:view-link-secret:v1\x00" + secret))
	return hex.EncodeToString(digest[:])
}

func viewLinkSecretMatches(link storedViewLink, secret string) bool {
	return subtle.ConstantTimeCompare([]byte(viewLinkSecretDigest(secret)), []byte(link.SecretDigest)) == 1
}

// viewLinkFileKey is the key a view link's page loads its files with, given
// only to a browser that opened the link (with its secret), since an image
// can't send the secret's header.
func viewLinkFileKey(app core.App, linkID string) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-view-link-files-v1\x00" + linkID))
	return "lvf_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func viewLinkFileKeyDigest(key string) string {
	digest := sha256.Sum256([]byte("leafen:view-link-files:v1\x00" + key))
	return hex.EncodeToString(digest[:])
}

// viewLinkOpener is who opens a view link, as far as the server sees.
type viewLinkOpener struct {
	IP        string
	UserAgent string
}

func viewLinkOpenerOf(event *core.RequestEvent) viewLinkOpener {
	agent := event.Request.UserAgent()
	if len(agent) > 300 {
		agent = agent[:300]
	}
	return viewLinkOpener{IP: event.RealIP(), UserAgent: agent}
}

// viewLinkStillShows is whether a view link may show its page now: not
// revoked or expired, within its single use, its page out of the trash and
// still within its maker's (and its key's) reach.
func viewLinkStillShows(app core.App, link storedViewLink, now time.Time) (publicVisibilityAuthorization, error) {
	nowMillis := now.UnixMilli()
	if link.RevokedAt != 0 || link.ExpiresAt <= nowMillis ||
		(link.SingleUse && link.UsedAt != 0 && nowMillis > link.UsedAt+viewLinkSingleUseWindowSeconds*1000) {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	authorization, err := authorizePage(app, link.OwnerID, link.PageID, permissionWorkspaceRead)
	if err != nil {
		return publicVisibilityAuthorization{}, refusedUnlessStorage(errPublicVisibilityNotFound, err)
	}
	if trashed, err := pageInTrashPath(app, link.PageID); err != nil || trashed {
		return publicVisibilityAuthorization{}, refusedUnlessStorage(errPublicVisibilityNotFound, err)
	}
	if link.ConnectionID != "" {
		key, found, err := findUserKeyByID(app, link.ConnectionID)
		if err != nil {
			return publicVisibilityAuthorization{}, err
		}
		if !found || !containsString(key.WorkspaceIDs, link.WorkspaceID) {
			return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
		}
	}
	return publicVisibilityAuthorization{
		AuthorityID: link.ID, RootPageID: link.PageID, Workspace: authorization.Workspace.Workspace,
		WorkspaceID: link.WorkspaceID, ViewerID: link.OwnerID,
	}, nil
}

// admitViewLinkSecret checks the secret given to open a view link before the
// read, outside its transaction, so a wrong one stays counted: the count
// rises, the attempt is logged, and the link closes once wrong secrets reach
// the limit. A link that no longer shows, or has no secret, is left to the
// read to answer.
func admitViewLinkSecret(app core.App, token, secret string, opener viewLinkOpener, now time.Time) error {
	link := storedViewLink{}
	err := app.DB().NewQuery(`SELECT * FROM {{leafen_view_links}} WHERE [[token_digest]] = {:digest}`).
		Bind(dbx.Params{"digest": viewLinkDigest(token)}).One(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if link.SecretDigest == "" {
		return nil
	}
	if _, err := viewLinkStillShows(app, link, now); err != nil {
		return err
	}
	if secret == "" {
		return errViewLinkSecretRequired
	}
	if viewLinkSecretMatches(link, secret) {
		return nil
	}
	nowMillis := now.UnixMilli()
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_view_links}} SET [[failed_attempts]] = [[failed_attempts]] + 1,
		[[revoked_at]] = CASE WHEN [[failed_attempts]] + 1 >= {:limit} AND [[revoked_at]] = 0 THEN {:now} ELSE [[revoked_at]] END
		WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"limit": viewLinkMaxSecretFailures, "now": nowMillis, "id": link.ID,
	}).Execute(); err != nil {
		return err
	}
	recordViewLinkOpen(app, link, viewLinkSecretWrong, opener, now)
	if link.FailedAttempts+1 >= viewLinkMaxSecretFailures {
		recordViewLinkOpen(app, link, viewLinkClosed, opener, now)
		// The attempt that closes the link is answered as every later one is
		// (2026-11-07: its screen still warned that more wrong secrets would
		// close a link already closed).
		return errPublicVisibilityNotFound
	}
	return errViewLinkSecretWrong
}

// recordViewLinkOpen logs an open of a view link, or a wrong secret, in its
// organization's audit log under its maker. A failure to log doesn't stop
// the page; it is reported as a product error.
func recordViewLinkOpen(app core.App, link storedViewLink, outcome string, opener viewLinkOpener, now time.Time) {
	workspace, err := app.FindRecordById(workspacesCollection, link.WorkspaceID)
	if err == nil {
		err = recordOrganizationAuditEvent(app, organizationAuditEvent{
			OrganizationID: workspace.GetString("organization"), WorkspaceID: link.WorkspaceID, ActorID: link.OwnerID,
			Action: "view_link." + outcome, TargetType: viewLinkAuditSubject, TargetID: link.ID,
			Metadata:   map[string]any{"pageId": link.PageID, "ip": opener.IP, "userAgent": opener.UserAgent},
			OccurredAt: now.UnixMilli(),
		})
	}
	if err != nil {
		app.Logger().Warn("view link audit skipped", "viewLink", link.ID, "error", err)
	}
}

// recordViewLinkOpenOf logs a view link's page opening: its first read, not
// the further windows of the same page.
func recordViewLinkOpenOf(app core.App, token string, opener viewLinkOpener, now time.Time) {
	link := storedViewLink{}
	if err := app.DB().NewQuery(`SELECT * FROM {{leafen_view_links}} WHERE [[token_digest]] = {:digest}`).
		Bind(dbx.Params{"digest": viewLinkDigest(token)}).One(&link); err == nil {
		recordViewLinkOpen(app, link, viewLinkOpened, opener, now)
	}
}

// authorizeViewLinkFiles lets a view link's page load its files with the key
// its open gave it, while the link still shows its page.
func authorizeViewLinkFiles(app core.App, fileKey string, now time.Time) (publicVisibilityAuthorization, error) {
	if !strings.HasPrefix(fileKey, "lvf_") {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	link := storedViewLink{}
	err := app.DB().NewQuery(`SELECT * FROM {{leafen_view_links}} WHERE [[file_key_digest]] = {:digest}`).
		Bind(dbx.Params{"digest": viewLinkFileKeyDigest(fileKey)}).One(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	return viewLinkStillShows(app, link, now)
}

type storedViewLinkOpen struct {
	Action   string `db:"action"`
	Created  int64  `db:"created"`
	Metadata string `db:"metadata_json"`
}

// listViewLinkOpens is a view link's opens and wrong secrets, newest first,
// for its maker.
func listViewLinkOpens(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	linkID := strings.TrimSpace(event.Request.PathValue("viewLinkId"))
	var owned int
	if err := event.App.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_view_links}} WHERE [[id]] = {:id} AND [[owner_id]] = {:owner}`).
		Bind(dbx.Params{"id": linkID, "owner": user.Id}).Row(&owned); err != nil || owned != 1 {
		return apiError(event, http.StatusNotFound, errorCodeViewLinkNotFound, "View link not found.")
	}
	rows := []storedViewLinkOpen{}
	if err := event.App.DB().NewQuery(`SELECT [[action]], [[created]], [[metadata_json]] FROM {{leafen_organization_audit_events}}
		WHERE [[subject_type]] = {:subject} AND [[subject_id]] = {:id}
		ORDER BY [[created]] DESC LIMIT {:limit}`).Bind(dbx.Params{
		"subject": viewLinkAuditSubject, "id": linkID, "limit": viewLinkOpensLimit,
	}).All(&rows); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeViewLinkFailed,
			"Couldn't read the view link's opens.", "view_link", "opens", errorCodeViewLinkFailed, err)
	}
	response := viewLinkOpensResponse{Opens: []viewLinkOpen{}}
	for _, row := range rows {
		metadata := struct {
			IP        string `json:"ip"`
			UserAgent string `json:"userAgent"`
		}{}
		_ = json.Unmarshal([]byte(row.Metadata), &metadata)
		response.Opens = append(response.Opens, viewLinkOpen{
			At: row.Created, Outcome: strings.TrimPrefix(row.Action, "view_link."), IP: metadata.IP, UserAgent: metadata.UserAgent,
		})
	}
	return event.JSON(http.StatusOK, response)
}
