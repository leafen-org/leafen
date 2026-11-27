package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A guest of an organization finds the people who share access with them,
// as Notion's restricted members mention only people who share a page or
// teamspace with them: the guest and whoever may open a page shared with
// the guest. The search read the twenty pages most recently shared with the
// guest and the organization's first two hundred people by name, and kept
// those who could open one of those pages: someone who reached only an
// older page, or whose name came after two hundred others, was never
// offered (2026-11-23). It now reads, for every page shared with the guest,
// who it may open to from the page's own sources (guestPagePeopleSQL), and
// the organization's people among them by name a window at a time, keeping
// each whom authorizePage lets open a page that named them — the one rule —
// until it holds what it shows.

// guestPageNominee is someone a page shared with a guest may open to, or
// with Workspace set everyone a share with that workspace reaches.
type guestPageNominee struct {
	PageID    string `db:"page_id"`
	UserID    string `db:"user_id"`
	Workspace string `db:"workspace"`
}

// guestPagePeopleSQL lists, for the pages bound as {:pages} in the workspace
// bound as {:workspaceId}, everyone the page access rule may let open them,
// and no fewer: the people on its shares and those above it whose sharing
// reaches it (loadPageAccessAncestorIDs), by account or app, verified
// email, group or the whole workspace (pageSharePrincipals); at the page its
// access is decided at — itself, or for a database's page the page holding
// its database (authorizeRowPageRecord) — its private owner, its
// teamspace's people by themselves or a group, or the whole workspace where
// the teamspace opens to it or is everyone's (decideTeamspaceAccess), and
// People's own (authorizeIfPeoplePage); whoever came in through a web link
// of the page or one above it (pageLinkAccessRole); and the workspace's
// owner. The rule itself decides each (searchGuestOrganizationPeople).
func guestPagePeopleSQL(app core.App) string {
	people := `(SELECT [[page_id]] FROM {{leafen_workspace_people_databases}} WHERE [[workspace_id]] = {:workspaceId})`
	peopleSources := `(SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}} WHERE [[workspace_id]] = {:workspaceId})`
	return `
		WITH RECURSIVE [[chain]] ([[anchor]], [[id]], [[parent_id]], [[row]], [[reached]], [[access]]) AS (
			SELECT [[page]].[[id]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[data_source_id]] != '', 1, 1
			FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:pages})) AND [[page]].[[workspace]] = {:workspaceId}
			UNION
			SELECT [[chain]].[[anchor]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[data_source_id]] != '',
				[[chain]].[[reached]] AND NOT EXISTS (
					SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]] = [[chain]].[[id]]),
				[[chain]].[[access]] AND [[chain]].[[row]]
			FROM {{leafen_pages}} [[page]]
			JOIN [[chain]] ON [[page]].[[id]] = [[chain]].[[parent_id]]
			WHERE [[chain]].[[parent_id]] != '' AND [[page]].[[workspace]] = {:workspaceId}
		),
		[[shared]] AS (
			SELECT [[chain]].[[anchor]], [[share]].[[principal_type]], [[share]].[[principal_id]]
			FROM [[chain]] JOIN {{leafen_page_permissions}} [[share]] ON [[share]].[[page_id]] = [[chain]].[[id]]
			WHERE [[chain]].[[reached]] AND [[share]].[[workspace_id]] = {:workspaceId}
		),
		[[decided]] AS (
			SELECT [[chain]].[[anchor]], [[page]].[[id]], [[page]].[[data_source_id]], [[page]].[[teamspace_id]],
				[[page]].[[` + privatePageOwnerField + `]] AS [[private_owner]]
			FROM [[chain]] JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[chain]].[[id]]
			WHERE [[chain]].[[access]]
		),
		[[everyone]] AS (
			SELECT [[shared]].[[anchor]] FROM [[shared]]
			WHERE [[shared]].[[principal_type]] = 'workspace' AND [[shared]].[[principal_id]] = {:workspaceId}
			UNION SELECT [[decided]].[[anchor]] FROM [[decided]]
			JOIN {{leafen_teamspaces}} [[teamspace]] ON [[teamspace]].[[id]] = [[decided]].[[teamspace_id]]
			WHERE [[teamspace]].[[workspace_id]] = {:workspaceId} AND [[teamspace]].[[deleted_at]] = 0 AND [[teamspace]].[[archived_at]] = 0
				AND (([[teamspace]].[[access]] != '` + teamspaceAccessPrivate + `' AND [[teamspace]].[[outside_access]])
					OR [[teamspace]].[[id]] IN (SELECT [[default_id]].[[value]]
						FROM {{leafen_teamspace_settings}} [[settings]], json_each([[settings]].[[default_teamspace_ids]]) [[default_id]]
						WHERE [[settings]].[[workspace_id]] = {:workspaceId}))
			UNION SELECT [[decided]].[[anchor]] FROM [[decided]]
			WHERE [[decided]].[[id]] IN ` + people + ` OR [[decided]].[[data_source_id]] IN ` + peopleSources + `
		)
		SELECT DISTINCT [[anchor]] AS [[page_id]], [[user_id]], '' AS [[workspace]] FROM (
			SELECT [[shared]].[[anchor]], [[shared]].[[principal_id]] AS [[user_id]] FROM [[shared]]
			WHERE [[shared]].[[principal_type]] IN ('user', 'integration')
			UNION SELECT [[shared]].[[anchor]], [[address]].[[user_id]] FROM [[shared]]
			JOIN {{leafen_account_emails}} [[address]] ON [[address]].[[email]] = lower(trim([[shared]].[[principal_id]]))
			WHERE [[shared]].[[principal_type]] = 'email' AND ` + provenAccountEmailSQL("[[address]].") + `
			UNION SELECT [[shared]].[[anchor]], [[group_member]].[[user_id]] FROM [[shared]]
			JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]] = [[shared]].[[principal_id]]
			JOIN {{leafen_workspace_permission_group_members}} [[group_member]] ON [[group_member]].[[group_id]] = [[group]].[[id]]
			WHERE [[shared]].[[principal_type]] = 'group' AND [[group]].[[workspace_id]] = {:workspaceId} AND [[group]].[[deleted_at]] IS NULL
			UNION SELECT [[decided]].[[anchor]], [[decided]].[[private_owner]] FROM [[decided]]
			WHERE [[decided]].[[private_owner]] NOT IN ('', '` + leafenSystemAuthorID + `')
			UNION SELECT [[decided]].[[anchor]], [[member]].[[principal_id]] FROM [[decided]]
			JOIN {{leafen_teamspace_members}} [[member]] ON [[member]].[[teamspace_id]] = [[decided]].[[teamspace_id]]
			WHERE [[member]].[[workspace_id]] = {:workspaceId} AND [[member]].[[deleted_at]] = 0 AND [[member]].[[principal_type]] = 'user'
			UNION SELECT [[decided]].[[anchor]], [[group_member]].[[user_id]] FROM [[decided]]
			JOIN {{leafen_teamspace_members}} [[member]] ON [[member]].[[teamspace_id]] = [[decided]].[[teamspace_id]]
			JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]] = [[member]].[[principal_id]]
			JOIN {{leafen_workspace_permission_group_members}} [[group_member]] ON [[group_member]].[[group_id]] = [[group]].[[id]]
			WHERE [[member]].[[workspace_id]] = {:workspaceId} AND [[member]].[[deleted_at]] = 0
				AND [[member]].[[principal_type]] = 'group' AND [[group]].[[deleted_at]] IS NULL
			UNION SELECT [[decided]].[[anchor]], [[person]].[[user_id]] FROM [[decided]]
			JOIN {{leafen_workspace_people_rows}} [[person]] ON [[person]].[[row_id]] = [[decided]].[[id]]
			WHERE [[person]].[[workspace_id]] = {:workspaceId}
			UNION SELECT [[decided]].[[anchor]], [[app]].[[user_id]] FROM [[decided]]
			JOIN {{leafen_integration_accounts}} [[app]] ON [[app]].[[workspace_id]] = {:workspaceId} AND [[app]].[[removed_at]] = 0
			WHERE [[decided]].[[id]] IN ` + people + ` OR [[decided]].[[data_source_id]] IN ` + peopleSources + `
			UNION SELECT [[chain]].[[anchor]], [[visitor]].[[user_id]] FROM [[chain]]
			JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[workspace_id]] = {:workspaceId} AND [[link]].[[root_page_id]] = [[chain]].[[id]]
			JOIN {{leafen_page_link_visitors}} [[visitor]] ON [[visitor]].[[link_id]] = [[link]].[[id]]
			WHERE [[link]].[[web_enabled]] = 1
			UNION SELECT [[chain]].[[anchor]], ` + workspacePermissionOwnerSQL(app, "[[workspace]]") + ` FROM [[chain]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
			WHERE [[chain]].[[id]] = [[chain]].[[anchor]]
		) WHERE [[user_id]] != ''
		UNION SELECT [[anchor]], '', {:workspaceId} FROM [[everyone]]
	`
}

// guestSharedPages lists the organization's pages out of the trash, with
// every page above them (pageOutOfTrashPathSQL), shared with the guest by
// their account or a verified email, newest share first, by workspace.
func guestSharedPages(app core.App, guestID, organizationID string) ([]string, map[string][]string, error) {
	rows := []struct {
		PageID    string `db:"page_id"`
		Workspace string `db:"workspace"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[permission]].[[page_id]], [[page]].[[workspace]] FROM {{leafen_page_permissions}} [[permission]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[permission]].[[workspace_id]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[permission]].[[page_id]]
		WHERE [[workspace]].[[organization]]={:organizationId} AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
			AND (([[permission]].[[principal_type]]='user' AND [[permission]].[[principal_id]]={:guest})
				OR ([[permission]].[[principal_type]]='email'
					AND lower(trim([[permission]].[[principal_id]])) IN (` + verifiedAccountEmailSQL("guest") + `)))
		GROUP BY [[permission]].[[page_id]]
		ORDER BY MAX([[permission]].[[updated_at]]) DESC, [[permission]].[[page_id]]
	`).Bind(dbx.Params{"organizationId": organizationID, "guest": guestID}).All(&rows); err != nil {
		return nil, nil, err
	}
	order := make([]string, 0, len(rows))
	byWorkspace := map[string][]string{}
	for _, row := range rows {
		order = append(order, row.PageID)
		byWorkspace[row.Workspace] = append(byWorkspace[row.Workspace], row.PageID)
	}
	return order, byWorkspace, nil
}

// guestPageReach is, for each person, the pages shared with a guest that
// may open to them, in the order the guest's pages came.
type guestPageReach map[string][]string

// readGuestPageReach reads who the pages shared with a guest may open to:
// the people their sources name, and the members of each workspace a share
// with it or a teamspace open to it reaches, as such a share reaches them
// (workspaceShareMemberSQL) and its owner.
func readGuestPageReach(app core.App, order []string, byWorkspace map[string][]string) (guestPageReach, error) {
	named := map[string]map[string]bool{}
	name := func(userID, pageID string) {
		if named[userID] == nil {
			named[userID] = map[string]bool{}
		}
		named[userID][pageID] = true
	}
	wide := map[string][]string{}
	for workspaceID, pages := range byWorkspace {
		encoded, err := json.Marshal(pages)
		if err != nil {
			return nil, err
		}
		nominees := []guestPageNominee{}
		if err := app.DB().NewQuery(guestPagePeopleSQL(app)).
			Bind(dbx.Params{"workspaceId": workspaceID, "pages": string(encoded)}).All(&nominees); err != nil {
			return nil, err
		}
		for _, nominee := range nominees {
			if nominee.Workspace != "" {
				wide[nominee.Workspace] = append(wide[nominee.Workspace], nominee.PageID)
			} else {
				name(nominee.UserID, nominee.PageID)
			}
		}
	}
	if len(wide) > 0 {
		workspaces := make([]string, 0, len(wide))
		for workspaceID := range wide {
			workspaces = append(workspaces, workspaceID)
		}
		encoded, err := json.Marshal(workspaces)
		if err != nil {
			return nil, err
		}
		members := []struct {
			UserID    string `db:"user_id"`
			Workspace string `db:"workspace"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[membership]].[[user_id]], [[membership]].[[workspace_id]] AS [[workspace]]
			FROM {{leafen_workspace_memberships}} [[membership]]
			WHERE [[membership]].[[workspace_id]] IN (SELECT [[value]] FROM json_each({:workspaces}))
				AND ` + workspaceShareMemberSQL("[[membership]].[[workspace_id]]", "[[membership]].[[user_id]]") + `
			UNION SELECT ` + workspacePermissionOwnerSQL(app, "[[workspace]]") + `, [[workspace]].[[id]]
			FROM {{leafen_workspaces}} [[workspace]]
			WHERE [[workspace]].[[id]] IN (SELECT [[value]] FROM json_each({:workspaces}))
		`).Bind(dbx.Params{"workspaces": string(encoded)}).All(&members); err != nil {
			return nil, err
		}
		for _, member := range members {
			for _, pageID := range wide[member.Workspace] {
				name(member.UserID, pageID)
			}
		}
	}
	reach := guestPageReach{}
	for userID, pages := range named {
		for _, pageID := range order {
			if pages[pageID] {
				reach[userID] = append(reach[userID], pageID)
			}
		}
	}
	return reach, nil
}

// searchGuestOrganizationPeople finds, for a guest of the organization, the
// people who share access with them: the guest, and whoever may open a page
// shared with the guest. within, when given, keeps to those people.
func searchGuestOrganizationPeople(
	app core.App, guest *core.Record, organizationID, query string, limit int, within []string, peopleOnly bool,
) ([]storedOrganizationPerson, error) {
	order, byWorkspace, err := guestSharedPages(app, guest.Id, organizationID)
	if err != nil {
		return nil, err
	}
	reach, err := readGuestPageReach(app, order, byWorkspace)
	if err != nil {
		return nil, err
	}
	reach[guest.Id] = nil
	kept := map[string]bool{}
	for _, id := range within {
		kept[id] = true
	}
	ids := make([]string, 0, len(reach))
	for userID := range reach {
		if within == nil || kept[userID] {
			ids = append(ids, userID)
		}
	}
	return fillWindows(limit, limit, func(size int, last *storedOrganizationPerson) ([]storedOrganizationPerson, error) {
		return loadOrganizationPeopleWindow(app, organizationID, query, ids, peopleOnly, last, size)
	}, func(candidate *storedOrganizationPerson) (bool, error) {
		if candidate.UserID == guest.Id {
			return true, nil
		}
		for _, pageID := range reach[candidate.UserID] {
			_, err := authorizePage(app, candidate.UserID, pageID, permissionWorkspaceRead)
			if err == nil {
				return true, nil
			}
			if !isPermissionDenied(err) && !errors.Is(err, sql.ErrNoRows) {
				return false, err
			}
		}
		return false, nil
	})
}
