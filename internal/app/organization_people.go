package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errOrganizationPeopleQueryInvalid = errors.New(errorCodeOrganizationPeopleQueryInvalid)

type storedOrganizationPerson struct {
	UserID           string `db:"user_id"`
	DisplayName      string `db:"display_name"`
	Email            string `db:"email"`
	Avatar           string `db:"avatar"`
	OrganizationRole string `db:"organization_role"`
	Status           string `db:"status"`
	Revision         int    `db:"revision"`
	// SortName and SortEmail are the name and email as the read sorts them
	// (lowered by the database), for a read that goes on past this person.
	SortName  string `db:"sort_name"`
	SortEmail string `db:"sort_email"`
}

func registerOrganizationPeopleRoutes(event *core.ServeEvent) {
	event.Router.Route(
		organizationPeopleSearchMethod,
		apiRoot+organizationPeopleSearchPath,
		searchOrganizationPeople,
	)
}

func searchOrganizationPeople(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	query := strings.ToLower(strings.TrimSpace(event.Request.URL.Query().Get("q")))
	// "kind=people" leaves out the accounts installed apps act as, as a
	// search's "생성자" lists people only.
	kind := event.Request.URL.Query().Get("kind")
	// "workspace=" keeps to that workspace's people (organization_people_workspace.go);
	// "invitees=1" adds, for its members, the addresses it invited that no
	// account proves yet, as a person picker and an "@" menu offer them
	// (people_value_invitees.go).
	workspaceID := strings.TrimSpace(event.Request.URL.Query().Get("workspace"))
	withInvitees := workspaceID != "" && kind == "" && event.Request.URL.Query().Get("invitees") == "1"
	limit, err := organizationPeopleLimit(event.Request.URL.Query().Get("limit"))
	if err != nil || organizationID == "" || (kind != "" && kind != "people") ||
		utf8.RuneCountInString(organizationID) > organizationIdentifierMaxRunes ||
		len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		utf8.RuneCountInString(query) > organizationPeopleQueryRunes {
		return organizationPeopleError(event, errOrganizationPeopleQueryInvalid, "validate")
	}
	rows := []storedOrganizationPerson{}
	unregistered := []string{}
	// A guest, and a restricted member picking in a workspace, see others'
	// names but not their addresses (peopleEmailShown).
	narrow := false
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizeOrganization(
			txApp, user.Id, organizationID, permissionOrganizationActive,
		)
		if err != nil {
			return err
		}
		var within []string
		if workspaceID != "" {
			if within, err = workspacePickPeople(txApp, user.Id, organizationID, workspaceID); err != nil {
				return err
			}
		}
		if authorization.Role == organizationRoleGuest {
			narrow = true
			rows, err = searchGuestOrganizationPeople(txApp, user, organizationID, query, limit, within, kind == "people")
			return err
		}
		restricted := false
		if workspaceID != "" {
			member, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) {
				return err
			}
			restricted = err != nil || member.Restricted
			narrow = err == nil && member.Restricted
		}
		if rows, err = loadOrganizationPeopleWindow(txApp, organizationID, query, within, kind == "people", nil, limit); err != nil || !withInvitees || len(rows) >= limit {
			return err
		}
		// A restricted member is offered only the people they share a place with.
		if restricted {
			return nil
		}
		invitees, missing, err := peopleValueInviteesToPick(txApp, workspaceID, query, limit-len(rows))
		rows, unregistered = append(rows, invitees...), missing
		return err
	})
	if err == nil {
		err = registerOfferedPeopleValueInvitees(event.App, workspaceID, user.Id, unregistered)
	}
	if err != nil {
		return organizationPeopleError(event, err, "read")
	}
	people := make([]organizationPerson, 0, len(rows))
	for _, row := range rows {
		people = append(people, organizationPerson{
			UserID: row.UserID, DisplayName: row.DisplayName,
			Email: peopleEmailShown(narrow, user.Id, row.UserID, row.Email), Avatar: row.Avatar,
			OrganizationRole: row.OrganizationRole, Status: row.Status,
			Revision: row.Revision,
		})
	}
	return event.JSON(http.StatusOK, organizationPeopleResponse{
		OrganizationID: organizationID, Query: query, Limit: limit, People: people,
	})
}

// loadOrganizationPeople reads the organization's active people whose name
// or email holds the query, by name, or only those among ids when given. A
// name holds a query ending in a syllable being typed letter by letter
// (hangulTypingPattern).
func loadOrganizationPeople(
	app core.App, organizationID, query string, ids []string, limit int,
) ([]storedOrganizationPerson, error) {
	return loadOrganizationPeopleWindow(app, organizationID, query, ids, false, nil, limit)
}

// loadOrganizationPeopleWindow reads as loadOrganizationPeople does, past
// the person after (by the order it sorts in: lowered name, lowered email,
// id; nil reads from the first), and with peopleOnly without the accounts
// installed apps act as: left out in the read, a list cut to its size and
// then rid of them came back short while more people answered (2026-11-23).
func loadOrganizationPeopleWindow(
	app core.App, organizationID, query string, ids []string, peopleOnly bool, after *storedOrganizationPerson, limit int,
) ([]storedOrganizationPerson, error) {
	among := ""
	params := dbx.Params{"organizationId": organizationID, "query": query, "limit": limit,
		"typing": hangulTypingPattern(query), "afterName": "", "afterEmail": "", "afterId": ""}
	if after != nil {
		params["afterName"], params["afterEmail"], params["afterId"] = after.SortName, after.SortEmail, after.UserID
	}
	if peopleOnly {
		among = ` AND NOT ` + integrationAccountSQL("[[membership]].[[user]]")
	}
	if ids != nil {
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		among += ` AND [[membership]].[[user]] IN (SELECT [[value]] FROM json_each({:ids}))`
		params["ids"] = string(encoded)
	}
	rows := []storedOrganizationPerson{}
	err := app.DB().NewQuery(`
		SELECT [[membership]].[[user]] AS [[user_id]],
			[[user]].[[name]] AS [[display_name]], [[user]].[[email]],
			[[user]].[[profile_avatar]] AS [[avatar]],
			[[membership]].[[role]] AS [[organization_role]],
			[[membership]].[[status]], [[profile]].[[revision]],
			lower([[user]].[[name]]) AS [[sort_name]], lower([[user]].[[email]]) AS [[sort_email]]
		FROM {{leafen_organization_memberships}} [[membership]]
			INDEXED BY [[idx_leafen_organization_memberships_org_user]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user]]
		JOIN {{leafen_account_profile_state}} [[profile]]
			ON [[profile]].[[user_id]]=[[membership]].[[user]]
		WHERE [[membership]].[[organization]]={:organizationId}
			AND [[membership]].[[status]]='active'
			AND ({:afterId}='' OR (lower([[user]].[[name]]), lower([[user]].[[email]]), [[membership]].[[user]])
				> ({:afterName}, {:afterEmail}, {:afterId}))
			AND ({:query}='' OR instr(lower([[user]].[[name]]), {:query})>0
				OR ({:typing}<>'' AND lower([[user]].[[name]]) GLOB {:typing})
				OR EXISTS (SELECT 1 FROM {{leafen_account_emails}} [[address]] WHERE [[address]].[[user_id]] = [[user]].[[id]] AND ` + provenAccountEmailSQL("[[address]].") + ` AND instr([[address]].[[email]], {:query})>0))` + among + `
		ORDER BY lower([[user]].[[name]]), lower([[user]].[[email]]), [[membership]].[[user]]
		LIMIT {:limit}
	`).Bind(params).All(&rows)
	return rows, err
}

func organizationPeopleLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return organizationPeopleDefaultResults, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > organizationPeopleMaxResults {
		return 0, errOrganizationPeopleQueryInvalid
	}
	return limit, nil
}

func organizationPeopleError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errOrganizationPeopleQueryInvalid):
		return apiErrorWithCause(
			event, http.StatusBadRequest, errorCodeOrganizationPeopleQueryInvalid,
			"Check the organization user search scope.", "organization_people", stage,
			errorCodeOrganizationPeopleQueryInvalid, err,
		)
	case isPermissionDenied(err):
		return apiErrorWithCause(
			event, http.StatusNotFound, errorCodeOrganizationNotFound,
			"Organization not found.", "organization_people", stage,
			errorCodeOrganizationNotFound, err,
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeOrganizationAuthorityInvalid,
			"Couldn't search organization users.", "organization_people", stage,
			errorCodeOrganizationAuthorityInvalid, err,
		)
	}
}

// hangulLeads are the lone consonants an input method shows while a syllable
// is begun, in the order of the syllables' first letters.
const hangulLeads = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ"

// hangulTypingPattern is a GLOB finding the text that holds a query ending
// in a Hangul syllable being typed, as Notion's "@" menu finds people letter
// by letter (seen 2026-11-12: "ㅇ" finds a name holding "음"): a lone consonant stands for
// itself and every syllable it begins, a syllable with no last letter for
// itself with any. Another query has none ("").
func hangulTypingPattern(query string) string {
	runes := []rune(query)
	if len(runes) == 0 {
		return ""
	}
	last := runes[len(runes)-1]
	class := ""
	if lead := strings.IndexRune(hangulLeads, last); lead >= 0 {
		first := rune(0xac00 + utf8.RuneCountInString(hangulLeads[:lead])*588)
		class = "[" + string(last) + string(first) + "-" + string(first+587) + "]"
	} else if last >= 0xac00 && last <= 0xd7a3 && (last-0xac00)%28 == 0 {
		class = "[" + string(last) + "-" + string(last+27) + "]"
	} else {
		return ""
	}
	var pattern strings.Builder
	pattern.WriteString("*")
	for _, r := range runes[:len(runes)-1] {
		switch r {
		case '*', '?', '[':
			pattern.WriteString("[" + string(r) + "]")
		default:
			pattern.WriteRune(r)
		}
	}
	pattern.WriteString(class + "*")
	return pattern.String()
}
