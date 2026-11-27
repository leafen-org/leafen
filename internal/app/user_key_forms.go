package app

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// errUserKeyFormNamesNothing is a form call that names neither the page it
// configures nor the form it reads or answers, so no workspace holds it.
var errUserKeyFormNamesNothing = errors.New("a form call names no page or form")

// formFileTokenHeader carries a form's token with a part of a file sent to
// it, the body being the part (form_file_parts.go).
const formFileTokenHeader = "X-Leafen-Form-Token"

// userKeyFormWorkspaces are the workspaces a form call acts in: the page a
// body names, configuring a form, and the form a token names — in the body
// or a part's header — answering one, sending its files or keeping it as a
// favorite. Each must be one of the key's, as each is what the handler may
// act in; a call naming none, or a form no token finds, is outside them.
func userKeyFormWorkspaces(event *core.RequestEvent, body userKeyBody, authority userKeyAuthority) ([]string, error) {
	pageID, single := body.one("pageId")
	if !single {
		return nil, errUserKeyOutsideWorkspaces
	}
	token, single := body.one("token")
	if !single {
		return nil, errUserKeyOutsideWorkspaces
	}
	workspaces := []string{}
	if pageID != "" {
		var workspaceID string
		err := event.App.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errUserKeyOutsideWorkspaces
		}
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspaceID)
	}
	for _, named := range []string{token, strings.TrimSpace(event.Request.Header.Get(formFileTokenHeader))} {
		if named == "" {
			continue
		}
		link, err := loadStoredFormLinkByToken(event.App, named)
		if errors.Is(err, errFormNotFound) {
			return nil, errUserKeyOutsideWorkspaces
		}
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, link.WorkspaceID)
	}
	if len(workspaces) == 0 {
		return nil, errUserKeyFormNamesNothing
	}
	slices.Sort(workspaces)
	workspaces = slices.Compact(workspaces)
	for _, workspaceID := range workspaces {
		if workspaceID == "" || !slices.Contains(authority.WorkspaceIDs, workspaceID) {
			return nil, errUserKeyOutsideWorkspaces
		}
	}
	return workspaces, nil
}
