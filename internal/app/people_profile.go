package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's people profiles, as Notion's (seen 2026-10-12). A member's
// name shows a card where it is pointed at — their picture, name, a yellow
// "제한됨" for a restricted member, the teamspaces they are in, their local
// time, and for someone else a button to email them — and opens their
// profile: what they made and edited lately, their pages, "About", email,
// membership, the people they work with most and their teamspaces. Members
// see members' cards and profiles; a guest has none and sees none. What the
// profile lists of pages are only those the reader may open, and of
// teamspaces those the reader sees. The workspace's owner turns the people
// list and profiles, the recent activity and the cards off under 설정 ›
// 일반 › 사람 (schema_baseline.json).

const (
	personCardPath      = "/workspaces/{workspaceId}/people/{userId}/card"
	personProfilePath   = "/workspaces/{workspaceId}/people/{userId}/profile"
	peopleDirectoryPath = "/workspaces/{workspaceId}/people/directory"
	peopleSettingsPath  = "/workspaces/{workspaceId}/people/settings"
	personAboutMaxRunes = 2000
	personTeamspacePage = 100

	errorCodePersonProfileNotFound = "LEAFEN_PERSON_PROFILE_NOT_FOUND"
	errorCodePersonProfileInvalid  = "LEAFEN_PERSON_PROFILE_INVALID"
	errorCodePersonProfileFailed   = "LEAFEN_PERSON_PROFILE_FAILED"
)

const (
	personMembershipOwner      = "owner"
	personMembershipMember     = "member"
	personMembershipRestricted = "restricted"
	personMembershipGuest      = "guest"
)

type peopleProfileSettings struct {
	Directory  bool `json:"directory"`
	Activity   bool `json:"activity"`
	HoverCards bool `json:"hoverCards"`
}

type personTeamspace struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
	Icon string `json:"icon" db:"icon"`
}

type personGroup struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// personCardGroups is how many of a person's groups their card names.
const personCardGroups = 3

type personCard struct {
	UserID      string            `json:"userId"`
	DisplayName string            `json:"displayName"`
	Avatar      string            `json:"avatar"`
	Email       string            `json:"email"`
	Self        bool              `json:"self"`
	Membership  string            `json:"membership"`
	TimeZone    string            `json:"timeZone"`
	Teamspaces  []personTeamspace `json:"teamspaces"`
	// The person's groups here, smallest first as Notion's card lists them
	// (its code, 2026-11-09): the first personCardGroups, and how many in all.
	Groups     []personGroup `json:"groups"`
	GroupCount int           `json:"groupCount"`
}

type personProfile struct {
	personCard
	personProfileCover
	CoverEditable bool             `json:"coverEditable"`
	About         string           `json:"about"`
	ActivityShown bool             `json:"activityShown"`
	Activity      []personActivity `json:"activity"`
	Pages         []personPage     `json:"pages"`
	Collaborators []string         `json:"collaborators"`
	// RowPageID is the person's People row, their page, whose bar the profile shows.
	RowPageID string `json:"rowPageId,omitempty"`
}

func registerPeopleProfileRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+personCardPath, readPersonCard)
	event.Router.GET(apiRoot+personProfilePath, readPersonProfile)
	event.Router.GET(apiRoot+peopleDirectoryPath, readPeopleDirectoryList)
	event.Router.PATCH(apiRoot+personProfilePath, updatePersonProfile).Bind(apis.BodyLimit(16 << 10))
	event.Router.PATCH(apiRoot+peopleSettingsPath, updatePeopleSettings).Bind(apis.BodyLimit(4 << 10))
}

// loadPeopleProfileSettings reads the workspace's three switches; the
// workspace's record doesn't carry them.
func loadPeopleProfileSettings(app core.App, workspaceID string) (peopleProfileSettings, error) {
	stored := struct {
		Directory  bool `db:"people_directory_disabled"`
		Activity   bool `db:"people_activity_disabled"`
		HoverCards bool `db:"people_hover_cards_disabled"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[people_directory_disabled]], [[people_activity_disabled]], [[people_hover_cards_disabled]]
		FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).One(&stored)
	return peopleProfileSettings{Directory: !stored.Directory, Activity: !stored.Activity, HoverCards: !stored.HoverCards}, err
}

// personProfileReader authorizes a member (not a guest) of the workspace to
// read its people's cards and profiles; ok false when it answered instead.
func personProfileReader(event *core.RequestEvent) (workspaceAuthorization, string, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return workspaceAuthorization{}, "", false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return workspaceAuthorization{}, "", false, apiError(event, http.StatusNotFound, errorCodePersonProfileNotFound, "Profile not found.")
	}
	if err != nil {
		return workspaceAuthorization{}, "", false, apiErrorWithCause(event, http.StatusInternalServerError, errorCodePersonProfileFailed,
			"Couldn't read the profile.", "people_profile", "authorize", errorCodePersonProfileFailed, err)
	}
	return authorization, user.Id, true, nil
}

// loadPersonCard reads a person of the workspace as the reader sees them:
// the workspace's owner, one of its members, a guest with an account or
// someone deactivated, as Notion's People lists them — not an app's
// account (people_profile_deactivated.go). Anyone else is not found.
func loadPersonCard(app core.App, authorization workspaceAuthorization, personID string) (personCard, error) {
	person := workspacePerson{}
	params := dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID}
	err := app.DB().NewQuery(workspacePeopleSQL() + `
		AND [[user]].[[id]] = {:personId} AND NOT ` + integrationAccountSQL("[[user]].[[id]]") + `
	`).Bind(params).One(&person)
	guest := false
	if errors.Is(err, sql.ErrNoRows) {
		err = app.DB().NewQuery(workspaceGuestSharesSQL + `
			SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]], [[user]].[[profile_avatar]] AS [[avatar]],
				` + organizationSuspendedSQL("[[user]].[[id]]") + ` AS [[suspended]]
			FROM {{users}} [[user]]
			WHERE [[user]].[[id]] = {:personId} AND NOT ` + integrationAccountSQL("[[user]].[[id]]") + `
				AND [[user]].[[id]] IN (SELECT [[account_id]] FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL)
		`).Bind(params).One(&person)
		guest = err == nil
	}
	deactivated := err == nil && person.Suspended
	if errors.Is(err, sql.ErrNoRows) {
		person, err = loadFormerPerson(app, authorization.WorkspaceID, personID)
		deactivated = err == nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return personCard{}, errPersonProfileNotFound
	}
	if err != nil {
		return personCard{}, err
	}
	user, err := app.FindRecordById("users", personID)
	if err != nil {
		return personCard{}, err
	}
	membership, err := personCardMembership(app, authorization, personID, guest, deactivated)
	if err != nil {
		return personCard{}, err
	}
	teamspaces, err := loadPersonTeamspaces(app, authorization, personID)
	if err != nil {
		return personCard{}, err
	}
	groups, groupCount, err := loadPersonGroups(app, authorization.WorkspaceID, personID)
	if err != nil {
		return personCard{}, err
	}
	return personCard{
		UserID: personID, DisplayName: person.DisplayName, Avatar: person.Avatar,
		Email: peopleEmailShown(authorization.Restricted, authorization.PrincipalID, personID, user.Email()),
		Self:  personID == authorization.PrincipalID, Membership: membership,
		TimeZone: loadAccountTimeZone(app, personID), Teamspaces: teamspaces, Groups: groups, GroupCount: groupCount,
	}, nil
}

// loadPersonGroups reads the person's permission groups in the workspace,
// the smallest first, then by name: the first personCardGroups of them and
// how many there are.
func loadPersonGroups(app core.App, workspaceID, personID string) ([]personGroup, int, error) {
	rows := []struct {
		personGroup
		Total int `db:"total"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[total]] FROM (
			SELECT [[group]].[[id]], [[group]].[[name]], COUNT(*) OVER () AS [[total]],
				(SELECT COUNT(*) FROM {{leafen_workspace_permission_group_members}} [[counted]]
					WHERE [[counted]].[[group_id]] = [[group]].[[id]]) AS [[members]],
				[[group]].[[normalized_name]] AS [[sort_name]]
			FROM {{leafen_workspace_permission_group_members}} [[member]]
			JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]] = [[member]].[[group_id]]
			WHERE [[group]].[[workspace_id]] = {:workspaceId} AND [[group]].[[deleted_at]] IS NULL
				AND [[member]].[[user_id]] = {:personId}
		) ORDER BY [[members]], [[sort_name]], [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "personId": personID, "limit": personCardGroups}).All(&rows); err != nil {
		return nil, 0, err
	}
	groups, total := make([]personGroup, 0, len(rows)), 0
	for _, row := range rows {
		groups, total = append(groups, row.personGroup), row.Total
	}
	return groups, total, nil
}

var errPersonProfileNotFound = errors.New(errorCodePersonProfileNotFound)

// loadPersonTeamspaces lists the teamspaces the person is in that the reader
// sees: those the reader is in too and, for a reader who isn't a restricted
// member, the workspace's open and closed ones.
func loadPersonTeamspaces(app core.App, authorization workspaceAuthorization, personID string) ([]personTeamspace, error) {
	restricted := 0
	if authorization.Restricted {
		restricted = 1
	}
	teamspaces := []personTeamspace{}
	afterName, afterID := "", ""
	for {
		window := []struct {
			personTeamspace
			SortName string `db:"sort_name"`
		}{}
		if err := app.DB().NewQuery(`
			WITH [[effective_members]]([[teamspace_id]],[[user_id]]) AS (` + teamspaceEffectiveMembersSQL() + `)
			SELECT [[teamspace]].[[id]], [[teamspace]].[[name]], [[teamspace]].[[icon]], lower([[teamspace]].[[name]]) AS [[sort_name]]
			FROM {{leafen_teamspaces}} [[teamspace]]
			WHERE [[teamspace]].[[workspace_id]] = {:workspaceId} AND [[teamspace]].[[deleted_at]] = 0
				AND [[teamspace]].[[archived_at]] = 0
				AND (lower([[teamspace]].[[name]]), [[teamspace]].[[id]]) > ({:afterName}, {:afterId})
				AND EXISTS (SELECT 1 FROM [[effective_members]] [[person]]
					WHERE [[person]].[[teamspace_id]] = [[teamspace]].[[id]] AND [[person]].[[user_id]] = {:personId})
				AND (EXISTS (SELECT 1 FROM [[effective_members]] [[reader]]
						WHERE [[reader]].[[teamspace_id]] = [[teamspace]].[[id]] AND [[reader]].[[user_id]] = {:readerId})
					OR ({:readerRestricted} = 0 AND [[teamspace]].[[access]] IN ('open', 'closed')))
			ORDER BY lower([[teamspace]].[[name]]), [[teamspace]].[[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": authorization.WorkspaceID, "personId": personID, "readerId": authorization.PrincipalID,
			"readerRestricted": restricted, "afterName": afterName, "afterId": afterID, "limit": personTeamspacePage,
		}).All(&window); err != nil {
			return nil, err
		}
		for _, row := range window {
			teamspaces = append(teamspaces, row.personTeamspace)
			afterName, afterID = row.SortName, row.ID
		}
		if len(window) < personTeamspacePage {
			return teamspaces, nil
		}
	}
}

func personProfileError(event *core.RequestEvent, err error, step string) error {
	if errors.Is(err, errPersonProfileNotFound) {
		return apiError(event, http.StatusNotFound, errorCodePersonProfileNotFound, "Profile not found.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePersonProfileFailed,
		"Couldn't read the profile.", "people_profile", step, errorCodePersonProfileFailed, err)
}

// readPersonCard answers the card a name shows, while the workspace shows cards.
func readPersonCard(event *core.RequestEvent) error {
	authorization, _, ok, err := personProfileReader(event)
	if !ok {
		return err
	}
	settings, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID)
	if err == nil && !settings.HoverCards {
		err = errPersonProfileNotFound
	}
	if err != nil {
		return personProfileError(event, err, "card")
	}
	card, err := loadPersonCard(event.App, authorization, strings.TrimSpace(event.Request.PathValue("userId")))
	if err == nil && card.Membership == personMembershipDeactivated {
		err = errPersonProfileNotFound // their name shows no card (people_profile_deactivated.go)
	}
	if err != nil {
		return personProfileError(event, err, "card")
	}
	return event.JSON(http.StatusOK, card)
}

// readPersonProfile answers a person's profile page, while the workspace
// has its people list and profiles.
func readPersonProfile(event *core.RequestEvent) error {
	authorization, readerID, ok, err := personProfileReader(event)
	if !ok {
		return err
	}
	settings, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID)
	if err == nil && !settings.Directory {
		err = errPersonProfileNotFound
	}
	if err != nil {
		return personProfileError(event, err, "profile")
	}
	personID := strings.TrimSpace(event.Request.PathValue("userId"))
	card, err := loadPersonCard(event.App, authorization, personID)
	if err == nil {
		err = peoplePageOpens(event.App, authorization, personID) // the profile is their page
	}
	if err != nil {
		return personProfileError(event, err, "profile")
	}
	profile := personProfile{personCard: card, ActivityShown: settings.Activity, personProfileCover: personProfileCover{CoverPosition: 50},
		CoverEditable: personCoverEditable(authorization, readerID, personID),
		Activity:      []personActivity{}, Pages: []personPage{}, Collaborators: []string{}}
	if err := event.App.DB().NewQuery(`
		SELECT [[about]], [[cover]], [[cover_position]] FROM {{leafen_workspace_person_profiles}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:personId}
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "personId": personID}).
		Row(&profile.About, &profile.Cover, &profile.CoverPosition); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return personProfileError(event, err, "about")
	}
	if profile.Pages, profile.Activity, profile.Collaborators, err = loadPersonWork(event.App, authorization, personID,
		settings.Activity); err != nil {
		return personProfileError(event, err, "work")
	}
	// Their row is made as People's own read makes it, where no one has opened People yet; the
	// profile shows without its page's bar while that waits.
	if profile.RowPageID, err = peopleRowOfPerson(event.App, authorization.WorkspaceID, personID); err != nil {
		return personProfileError(event, err, "row")
	} else if profile.RowPageID == "" {
		if _, err := syncPeopleDatabase(event.App, authorization.WorkspaceID); err != nil {
			event.App.Logger().Warn("leafen people database waits", "workspace", authorization.WorkspaceID, "error", err)
		} else if profile.RowPageID, err = peopleRowOfPerson(event.App, authorization.WorkspaceID, personID); err != nil {
			return personProfileError(event, err, "row")
		}
	}
	return event.JSON(http.StatusOK, answerPersonProfile(event, profile)) // people_profile_summary.go
}

// updatePersonProfile saves the "About" a member writes in their own profile, or the workspace's
// owner in anyone's as Notion's (seen 2026-11-15 twice), or a profile's cover (people_profile_cover.go).
func updatePersonProfile(event *core.RequestEvent) error {
	authorization, actorID, ok, err := personProfileReader(event)
	if !ok {
		return err
	}
	personID := strings.TrimSpace(event.Request.PathValue("userId"))
	input := struct {
		About         *string `json:"about"`
		Cover         *string `json:"cover"`
		CoverPosition *int    `json:"coverPosition"`
	}{}
	if err := event.BindBody(&input); err != nil || input.About == nil && input.Cover == nil && input.CoverPosition == nil {
		return apiError(event, http.StatusBadRequest, errorCodePersonProfileInvalid, "Send at least one of about, cover and coverPosition to change.")
	}
	if input.About == nil {
		settings, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID)
		if err == nil && !settings.Directory {
			err = errPersonProfileNotFound
		}
		if err != nil {
			return personProfileError(event, err, "cover-settings")
		}
		return saveProfileCover(event, authorization, actorID, personID, input.Cover, input.CoverPosition)
	}
	about := strings.TrimSpace(*input.About)
	if !utf8.ValidString(about) || utf8.RuneCountInString(about) > personAboutMaxRunes {
		return apiError(event, http.StatusBadRequest, errorCodePersonProfileInvalid, "The about text is too long.")
	}
	if settings, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID); err != nil {
		return personProfileError(event, err, "about-settings")
	} else if personID != actorID && authorization.Role != workspaceRoleOwner || !settings.Directory {
		return apiError(event, http.StatusForbidden, errorCodePersonProfileNotFound, "Only you write your profile.")
	}
	if _, err := loadPersonCard(event.App, authorization, personID); personID != actorID && err != nil {
		return personProfileError(event, err, "about-person")
	}
	if err := savePersonAbout(event.App, authorization.WorkspaceID, personID, actorID, about); err != nil {
		return personProfileError(event, err, "about-save")
	}
	return event.JSON(http.StatusOK, map[string]string{"about": about})
}

// updatePeopleSettings changes the workspace's three switches; only its
// owner does.
func updatePeopleSettings(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return personProfileError(event, err, "settings-authorize")
	}
	input := struct {
		Directory  *bool `json:"directory"`
		Activity   *bool `json:"activity"`
		HoverCards *bool `json:"hoverCards"`
	}{}
	if err := event.BindBody(&input); err != nil || input.Directory == nil && input.Activity == nil && input.HoverCards == nil {
		return apiError(event, http.StatusBadRequest, errorCodePersonProfileInvalid, "Send at least one of directory, activity and hoverCards to change.")
	}
	settings, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID)
	if err != nil {
		return personProfileError(event, err, "settings-read")
	}
	for _, change := range []struct {
		value  *bool
		target *bool
	}{{input.Directory, &settings.Directory}, {input.Activity, &settings.Activity}, {input.HoverCards, &settings.HoverCards}} {
		if change.value != nil {
			*change.target = *change.value
		}
	}
	if _, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_workspaces}} SET [[people_directory_disabled]] = {:directory},
			[[people_activity_disabled]] = {:activity}, [[people_hover_cards_disabled]] = {:hoverCards}
		WHERE [[id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "directory": !settings.Directory, "activity": !settings.Activity,
		"hoverCards": !settings.HoverCards}).Execute(); err != nil {
		return personProfileError(event, err, "settings-save")
	}
	return event.JSON(http.StatusOK, settings)
}

type peopleDirectoryRow struct {
	UserID      string `json:"userId" db:"user_id"`
	DisplayName string `json:"displayName" db:"display_name"`
	Avatar      string `json:"avatar" db:"avatar"`
	Email       string `json:"email" db:"email"`
	About       string `json:"about" db:"about"`
	Membership  string `json:"membership" db:"membership"`
}

// readPeopleDirectoryList answers the people list, as Notion's "People"
// (seen 2026-10-12): the workspace's owner, members, guests with an account
// and deactivated people — suspended, or former guests — by name, with
// their email, "About" and membership — its views leave guests and the
// deactivated out until their filter is changed; a restricted reader sees
// the people they share a place with. A page holds a thousand; ?cursor=
// reads the next one.
func readPeopleDirectoryList(event *core.RequestEvent) error {
	authorization, _, ok, err := personProfileReader(event)
	if !ok {
		return err
	}
	settings, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID)
	if err == nil && !settings.Directory {
		err = errPersonProfileNotFound
	}
	if err != nil {
		return personProfileError(event, err, "directory")
	}
	after, ok := readWorkspacePeopleCursor(event)
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodePersonProfileNotFound, "The people cursor isn't one this server gave.")
	}
	people := []peopleDirectoryRow{}
	shown := restrictedPeopleFilterSQL(authorization.Restricted) + ` AND NOT ` + integrationAccountSQL("[[user]].[[id]]")
	if err := event.App.DB().NewQuery(peopleDirectorySQL(shown) + `
		WHERE {:afterId} = '' OR lower([[person]].[[display_name]]) > {:afterName}
			OR (lower([[person]].[[display_name]]) = {:afterName} AND [[person]].[[user_id]] > {:afterId})
		ORDER BY lower([[person]].[[display_name]]), [[person]].[[user_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "ownerId": authorization.WorkspaceOwnerID,
		"readerId": authorization.PrincipalID, "limit": workspacePeopleDirectoryLimit + 1,
		"afterName": after.Name, "afterId": after.ID}).All(&people); err != nil {
		return personProfileError(event, err, "directory")
	}
	for index := range people {
		people[index].Email = peopleEmailShown(authorization.Restricted, authorization.PrincipalID, people[index].UserID, people[index].Email)
	}
	answer := map[string]any{}
	if len(people) > workspacePeopleDirectoryLimit {
		people = people[:workspacePeopleDirectoryLimit]
		last := people[len(people)-1]
		answer["nextCursor"] = workspacePeopleCursor{Name: sqliteLower(last.DisplayName), ID: last.UserID}.encode()
	}
	reach, err := loadPeoplePageReach(event.App, authorization)
	if err != nil {
		return personProfileError(event, err, "directory-reach")
	}
	answer["people"] = reach.keep(people)
	return event.JSON(http.StatusOK, answer)
}
