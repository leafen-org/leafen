package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A guest invite link (2026-11-23, the user bringing a Notion page's guest
// into Leafen with the same access: "그냥 한번 주면 코드도 있어서 바로 접속되는
// 계정으로 줘야지", "응 개선도하고" — an approved difference: Notion has no such
// link) lets whoever opens it in as a guest of the pages it names, with the
// role it gives, until it expires, and once only unless it is made for
// several people. Someone without an account gives a name and is in on that
// device, as every invitation lets people in (the main loop
// 2026-11-23: no password asked, one is set later in settings); someone
// signed in takes it into their account, an account an administrator made
// among them once its first sign-in has set its password.
//
// A person with full access to each page makes one, from the share window's
// "고급" or through the API (leafen_call): a member of the workspace, not a
// restricted one, where the organization lets guests in and lets this
// person invite them, on pages whose teamspace keeps guests. The link's code
// is a secret: the server keeps only its hash, the link carries it after #
// (never sent in a request's address, nor logged), the app takes it out of
// the address once used, and a link made for one person is spent by its use.
// Taking it shares each page with the person as its maker would, under the
// rules as they are then (takePageGuestLink).

const (
	pageGuestLinkTokenPrefix = "pgl_"
	pageGuestLinkDefaultDays = 7
	// pageGuestLinksPerWindow is how many of a page's links one read lists;
	// the next window goes on from its nextCursor, to the last.
	pageGuestLinksPerWindow = 50
)

var errPageGuestLinkInvalid = errors.New(errorCodePageGuestLinkInvalid)

// asSignupInvitationInvalid is a guest invite link's refusal as an
// invitation's, keeping why it ended.
func asSignupInvitationInvalid(err error) error {
	if errors.Is(err, errPageGuestLinkInvalid) && invitationEndedCause(err) == "" {
		return errSignupInvitationInvalid
	}
	return err
}

// pageGuestLinkEndedFor is why the link a code's hash names lets no one in
// (invitation_ended.go): made for one person, who used it; past its end;
// turned off; or no link has its code.
func pageGuestLinkEndedFor(app core.App, hash string, now time.Time) error {
	link := struct {
		RevokedAt int64 `db:"revoked_at"`
		ExpiresAt int64 `db:"expires_at"`
		SingleUse bool  `db:"single_use"`
		Uses      int   `db:"uses"`
	}{}
	err := app.DB().NewQuery(`SELECT [[revoked_at]], [[expires_at]], [[single_use]], [[uses]] FROM {{leafen_page_guest_links}}
		WHERE [[token_hash]] = {:hash}`).Bind(dbx.Params{"hash": hash}).One(&link)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return invitationEnded{pageGuestLinkUnknown}
	case err != nil:
		return err
	case link.RevokedAt > 0:
		return invitationEnded{pageGuestLinkOff}
	case link.SingleUse && link.Uses > 0:
		return invitationEnded{pageGuestLinkUsed}
	case link.ExpiresAt <= now.UnixMilli():
		return invitationEnded{pageGuestLinkExpired}
	}
	return errPageGuestLinkInvalid
}

type storedPageGuestLink struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	CreatedBy   string `db:"created_by"`
	Role        string `db:"role"`
	PageIDs     string `db:"page_ids"`
	SingleUse   bool   `db:"single_use"`
	Uses        int    `db:"uses"`
	ExpiresAt   int64  `db:"expires_at"`
	CreatedAt   int64  `db:"created_at"`
	// Left is its pages still there to share, in its order, as
	// loadUsablePageGuestLink read them.
	Left []string `db:"-"`
}

func (link storedPageGuestLink) pages() []string {
	ids := []string{}
	_ = json.Unmarshal([]byte(link.PageIDs), &ids)
	return ids
}

const storedPageGuestLinkColumns = `[[id]], [[workspace_id]], [[created_by]], [[role]], [[page_ids]], [[single_use]], [[uses]],
	[[expires_at]], [[created_at]]`

// pageGuestLinkUsable is the SQL that keeps to links that still let someone in.
const pageGuestLinkUsable = `[[revoked_at]] = 0 AND [[expires_at]] > {:now} AND ([[single_use]] = 0 OR [[uses]] = 0)`

func pageGuestLinkTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func registerPageGuestLinkRoutes(event *core.ServeEvent) {
	event.Router.Route(pageGuestLinkCreateMethod, apiRoot+pageGuestLinkCreatePath, createPageGuestLinkRoute)
	event.Router.Route(pageGuestLinkListMethod, apiRoot+pageGuestLinkListPath, listPageGuestLinksRoute)
	event.Router.Route(pageGuestLinkRevokeMethod, apiRoot+pageGuestLinkRevokePath, revokePageGuestLinkRoute)
}

func createPageGuestLinkRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := pageGuestLinkInput{}
	if err := event.BindBody(&input); err != nil {
		return pageGuestLinkRouteError(event, bodyRefusal(errPublicMutationInvalid, "guest invite link", err), "Couldn't make the guest invite link.")
	}
	if len(input.PageIDs) == 0 {
		return refuseMissingInput(event, errorCodePageGuestLinkRequestInvalid, "pageIds", "the pages the link invites a guest to.")
	}
	if strings.TrimSpace(input.Role) == "" {
		return refuseMissingInput(event, errorCodePageGuestLinkRequestInvalid, "role", "the guest's access: edit, comment or view.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	// A key's call asks its person only for a link that would be made
	// (user_key_acting_ask.go): its pages, their workspace and rights first.
	created := pageGuestLinkCreated{}
	err := rehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := createPageGuestLink(txApp, user.Id, workspaceID, input, time.Now())
		return err
	})
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		created, err = createPageGuestLink(event.App, user.Id, workspaceID, input, time.Now())
	}
	if err != nil {
		return pageGuestLinkRouteError(event, err, "Couldn't make the guest invite link.")
	}
	// The address others open: the server's public one, else the one this request came to.
	origin := configuredPublicOrigin()
	if origin == "" {
		origin = requestOrigin(event.Request)
	}
	created.URL = origin + created.Path
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, created)
}

// createPageGuestLink checks that the person may let a guest into each page
// and keeps the link, its code's hash only.
func createPageGuestLink(app core.App, actorID, workspaceID string, input pageGuestLinkInput, now time.Time) (pageGuestLinkCreated, error) {
	role := strings.TrimSpace(input.Role)
	if role != publicMutationRoleEdit && role != publicMutationRoleComment && role != publicMutationRoleView {
		return pageGuestLinkCreated{}, publicMutationRefused("role is edit, comment or view, not %q", role)
	}
	days := input.ExpiresInDays
	if days == 0 {
		days = pageGuestLinkDefaultDays
	}
	if days < 1 || days > pageGuestLinkMaxDays {
		return pageGuestLinkCreated{}, publicMutationRefused("expiresInDays is from 1 to %d", pageGuestLinkMaxDays)
	}
	pageIDs := []string{}
	for _, id := range input.PageIDs {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(pageIDs, id) {
			pageIDs = append(pageIDs, id)
		}
	}
	if len(pageIDs) == 0 || len(pageIDs) > pageGuestLinkMaxPages {
		return pageGuestLinkCreated{}, publicMutationRefused("pageIds names 1 to %d pages of the workspace", pageGuestLinkMaxPages)
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return pageGuestLinkCreated{}, err
	}
	token := pageGuestLinkTokenPrefix + base64.RawURLEncoding.EncodeToString(random)
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return pageGuestLinkCreated{}, err
	}
	stored := storedPageGuestLink{
		ID: security.RandomString(15), WorkspaceID: workspaceID, CreatedBy: actorID, Role: role, PageIDs: string(encoded),
		SingleUse: !input.Reusable, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour).UnixMilli(), CreatedAt: now.UnixMilli(),
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		if err := checkPageGuestLinkMaker(txApp, actorID, workspaceID, pageIDs); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_page_guest_links}}
			([[id]], [[token_hash]], [[workspace_id]], [[created_by]], [[role]], [[page_ids]], [[single_use]], [[expires_at]], [[created_at]])
			VALUES ({:id}, {:hash}, {:workspace}, {:actor}, {:role}, {:pages}, {:single}, {:expires}, {:now})`).Bind(dbx.Params{
			"id": stored.ID, "hash": pageGuestLinkTokenHash(token), "workspace": workspaceID, "actor": actorID, "role": role,
			"pages": stored.PageIDs, "single": stored.SingleUse, "expires": stored.ExpiresAt, "now": stored.CreatedAt,
		}).Execute()
		return err
	})
	if err != nil {
		return pageGuestLinkCreated{}, err
	}
	link, err := pageGuestLinkView(app, actorID, stored, true)
	if err != nil {
		return pageGuestLinkCreated{}, err
	}
	return pageGuestLinkCreated{Link: link, Path: "/p/" + url.PathEscape(pageIDs[0]) + "#invitation=" + token}, nil
}

// checkPageGuestLinkMaker refuses a link its maker may not give: someone
// not a member of the workspace, or a restricted one; an organization that
// keeps guests out, or lets only owners invite them; a page they haven't
// full access to, in the trash, of another workspace or of a teamspace that
// keeps guests out.
func checkPageGuestLinkMaker(app core.App, actorID, workspaceID string, pageIDs []string) error {
	workspace, err := authorizeWorkspace(app, actorID, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return pageGuestLinkMissingIf(err, pageGuestLinkWorkspaceKind, workspaceID)
	}
	if workspace.Restricted {
		return errOrganizationSharingSecurityBlocked
	}
	organizationID := workspace.Organization.OrganizationID
	options, err := sharingSecurityShareOptions(app, organizationID)
	if err != nil {
		return err
	}
	if !options.Guests {
		return errOrganizationSharingSecurityBlocked
	}
	if organizationID != "" {
		if err := enforceOrganizationDLPOutbound(app, organizationID, dlpOutboundExternalSharing); err != nil {
			return err
		}
	}
	if workspace.Role != workspaceRoleOwner {
		policy, err := loadEnforcedSharingSecurityPolicy(app, organizationID)
		if err != nil {
			return err
		}
		if policy.GuestInvites == sharingSecurityGuestInvitesOwners {
			return guestInviteRefusal(policy)
		}
	}
	for _, pageID := range pageIDs {
		page, err := authorizePublicMutationFor(app, actorID, pageID, publicMutationInput{Action: publicMutationActionGrantPermission})
		if err != nil {
			return pageGuestLinkMissingIf(err, pageGuestLinkPageKind, pageID)
		}
		if page.Workspace.WorkspaceID != workspaceID {
			// Where it is, for the call to go there (2026-11-24).
			name := ""
			if page.Workspace.Workspace != nil {
				name = page.Workspace.Workspace.GetString("name")
			}
			return publicMutationRefused("page %s is in another workspace, %q: its link is made with POST /workspaces/%s/page-guest-links",
				pageID, name, page.Workspace.WorkspaceID)
		}
		security, err := teamspacePageSecurity(app, pageID)
		if err != nil {
			return err
		}
		if security.DisableGuests {
			return errOrganizationSharingSecurityBlocked
		}
	}
	return nil
}

// pageGuestLinkView is a link as its window and the API show it, with
// whether its reader may turn it off (pageGuestLinkTurnOff).
func pageGuestLinkView(app core.App, viewerID string, link storedPageGuestLink, revocable bool) (pageGuestLinkResponse, error) {
	view := pageGuestLinkResponse{
		ID: link.ID, Role: link.Role, Pages: []pageGuestLinkPage{}, ExpiresAt: link.ExpiresAt, Reusable: !link.SingleUse,
		Uses: link.Uses, CreatedBy: link.CreatedBy, Mine: link.CreatedBy == viewerID, Revocable: revocable,
	}
	for _, pageID := range link.pages() {
		page, err := app.FindRecordById(pagesCollection, pageID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return pageGuestLinkResponse{}, err
		}
		view.Pages = append(view.Pages, pageGuestLinkPage{ID: pageID, Title: page.GetString("title")})
	}
	if maker, err := app.FindRecordById("users", link.CreatedBy); err == nil {
		view.CreatedByName = strings.TrimSpace(maker.GetString("name"))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return pageGuestLinkResponse{}, err
	}
	return view, nil
}

// listPageGuestLinksRoute is the links still usable that let someone into a
// page, for whoever may share it, newest first, a window at a time from the
// request's cursor on: every such link is listed, as each can be turned off
// only by its ID (2026-11-23: past fifty, the oldest let people in till they
// expired while neither shown nor turned off).
func listPageGuestLinksRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	const failed = "Couldn't list the guest invite links."
	page, err := authorizePublicMutationFor(event.App, user.Id, pageID, publicMutationInput{Action: publicMutationActionGrantPermission})
	if err != nil {
		return pageGuestLinkRouteError(event, pageGuestLinkMissingIf(err, pageGuestLinkPageKind, pageID), failed)
	}
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Give the nextCursor of the last list, or none to start.")
	}
	links := []storedPageGuestLink{}
	if err := event.App.DB().NewQuery(`SELECT ` + storedPageGuestLinkColumns + ` FROM {{leafen_page_guest_links}}
		WHERE [[workspace_id]] = {:workspace} AND ` + pageGuestLinkUsable + `
			AND EXISTS (SELECT 1 FROM json_each([[page_ids]]) WHERE [[value]] = {:page})
			AND ({:afterId} = '' OR [[created_at]] < {:afterAt} OR ([[created_at]] = {:afterAt} AND [[id]] > {:afterId}))
		ORDER BY [[created_at]] DESC, [[id]] LIMIT {:limit}`).Bind(dbx.Params{
		"workspace": page.Workspace.WorkspaceID, "page": pageID, "now": time.Now().UnixMilli(),
		"afterAt": after.At, "afterId": after.ID, "limit": pageGuestLinksPerWindow + 1,
	}).All(&links); err != nil {
		return pageGuestLinkRouteError(event, err, failed)
	}
	links, next := timeIDWindowOf(links, pageGuestLinksPerWindow, func(link storedPageGuestLink) (int64, string) { return link.CreatedAt, link.ID })
	response := pageGuestLinkListResponse{Links: []pageGuestLinkResponse{}, NextCursor: next}
	turnOff := newPageGuestLinkTurnOff(event.App, user.Id)
	for _, link := range links {
		revocable, err := turnOff.allows(link)
		if err != nil {
			return pageGuestLinkRouteError(event, err, failed)
		}
		view, err := pageGuestLinkView(event.App, user.Id, link, revocable)
		if err != nil {
			return pageGuestLinkRouteError(event, err, failed)
		}
		response.Links = append(response.Links, view)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// revokePageGuestLinkRoute ends a link for its maker, or for an owner of its
// workspace: no one gets in by it after.
func revokePageGuestLinkRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	link := storedPageGuestLink{}
	linkID := strings.TrimSpace(event.Request.PathValue("linkId"))
	err := event.App.DB().NewQuery(`SELECT ` + storedPageGuestLinkColumns + ` FROM {{leafen_page_guest_links}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": linkID}).One(&link)
	if err == nil {
		var allowed bool
		if allowed, err = newPageGuestLinkTurnOff(event.App, user.Id).allows(link); err == nil && !allowed {
			err = sql.ErrNoRows
		}
	}
	if err == nil {
		_, err = event.App.DB().NewQuery(`UPDATE {{leafen_page_guest_links}} SET [[revoked_at]] = {:now} WHERE [[id]] = {:id} AND [[revoked_at]] = 0`).
			Bind(dbx.Params{"id": link.ID, "now": time.Now().UnixMilli()}).Execute()
	}
	var view pageGuestLinkResponse
	if err == nil {
		view, err = pageGuestLinkView(event.App, user.Id, link, true)
	}
	if err != nil {
		// One that isn't there and one the person may not turn off say the same.
		return pageGuestLinkRouteError(event, pageGuestLinkMissingIf(err, pageGuestLinkKind, linkID), "Couldn't turn off the guest invite link.")
	}
	return event.JSON(http.StatusOK, view)
}

// loadUsablePageGuestLink is the link a code opens while it still lets
// someone in, where its organization still lets guests in, with a page left
// to share.
func loadUsablePageGuestLink(app core.App, token string, now time.Time) (storedPageGuestLink, error) {
	link := storedPageGuestLink{}
	hash := pageGuestLinkTokenHash(strings.TrimSpace(token))
	err := app.DB().NewQuery(`SELECT ` + storedPageGuestLinkColumns + ` FROM {{leafen_page_guest_links}}
		WHERE [[token_hash]] = {:hash} AND ` + pageGuestLinkUsable).
		Bind(dbx.Params{"hash": hash, "now": now.UnixMilli()}).One(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return link, pageGuestLinkEndedFor(app, hash, now)
	}
	if err != nil {
		return link, err
	}
	workspace, err := app.FindRecordById(workspacesCollection, link.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspace.GetString("deletion_job_id") != "" {
		return link, invitationEnded{pageGuestLinkOff}
	}
	if err != nil {
		return link, err
	}
	options, err := sharingSecurityShareOptions(app, workspace.GetString("organization"))
	if err != nil {
		return link, err
	}
	if !options.Guests {
		return link, invitationEnded{pageGuestLinkOff}
	}
	if link.Left, err = pageGuestLinkPagesLeft(app, link.pages()); err != nil {
		return link, err
	}
	if len(link.Left) == 0 {
		return link, invitationEnded{pageGuestLinkNoPages}
	}
	return link, nil
}

// pageGuestLinkPagesLeft is which of a link's pages are still there to
// share: not deleted, nor in the trash or under a page in it (2026-11-23: a
// link whose pages were all in the trash read as usable, then let no one
// in with no reason). Each one of them, the link lets in to those alone; with
// none left it lets no one in until one is restored from the trash, its end
// and its one use as they were — the lookup, the name step, the password
// sign-up and a signed-in account's take decide it alike here.
func pageGuestLinkPagesLeft(app core.App, pageIDs []string) ([]string, error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	ids := make([]any, len(pageIDs))
	for index, pageID := range pageIDs {
		ids[index] = pageID
	}
	found := []string{}
	if err := app.DB().Select("id").From(pagesCollection).Where(dbx.In("id", ids...)).Column(&found); err != nil {
		return nil, err
	}
	trashed, err := loadPageTrashEffects(app, found)
	if err != nil {
		return nil, err
	}
	left := []string{}
	for _, pageID := range pageIDs {
		if _, inTrash := trashed[pageID]; slices.Contains(found, pageID) && !inTrash {
			left = append(left, pageID)
		}
	}
	return left, nil
}

// describePageGuestLink is what the first screen of a guest invite link says:
// who made it, in which workspace, and its first page left, which opens once
// the person is in — the link's own address names its first page, which may
// be in the trash now.
func describePageGuestLink(app core.App, token string, response *signupInvitationResponse) error {
	link, err := loadUsablePageGuestLink(app, token, time.Now())
	if err != nil {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, link.Left[0])
	if err != nil {
		return err
	}
	response.PageTitle, response.PageID = page.GetString("title"), page.Id
	return nil
}
