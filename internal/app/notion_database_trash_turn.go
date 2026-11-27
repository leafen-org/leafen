package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's API takes a database's trash change beside the rest of its update
// (seen 2026-11-23, Notion-Version 2026-03-11, synthetic databases under the
// test page, two runs): a title, description, icon, is_inline or parent sent
// with in_trash go on the database as it goes to the trash or comes out of
// it — save a parent beside taking it out, which is left: it comes back
// where it was (seen four times, full-page and inline). Here it comes out
// first and goes in last, as a page does
// (notion_database_row_page_mutation.go); a value of the wrong form is
// refused before either, and a rest refused once it is out puts it back.
// in_trash as the database already stands leaves only the rest. What a
// database in the trash refuses was decided before (serveNotionDatabaseUpdate).

// applyNotionDatabaseUpdate applies a database update that is taken: the
// trash, the rest, or both in their turn.
func applyNotionDatabaseUpdate(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, page pageAuthorization, input notionDatabaseUpdateRequest,
) error {
	rest := input
	rest.InTrash = nil
	trashed := container.Trashed || page.Page.GetBool("in_trash")
	if input.InTrash == nil || *input.InTrash == trashed {
		if rest.empty() {
			return nil
		}
		return updateNotionDatabase(event, ygo, identity, container, page, rest)
	}
	inTrash := *input.InTrash
	if !inTrash {
		rest.Parent = nil
	}
	if rest.empty() {
		return changeNotionDatabaseTrash(event, ygo, identity, container, page, inTrash)
	}
	// Notion's API takes no null icon for a database (seen 2026-10-30), and
	// the trash stays as it was (seen 2026-11-23).
	if string(rest.Icon) == "null" {
		return notionDatabaseRowPageRequestInvalid("%s", notionBodyFault("body.icon", "an object or `undefined`", "null"))
	}
	reload := func() (notionDatabaseContainer, pageAuthorization, error) {
		return loadNotionDatabaseContainer(event.App, identity, container.ID, permissionWorkspaceWrite)
	}
	if inTrash {
		if err := updateNotionDatabase(event, ygo, identity, container, page, rest); err != nil {
			return err
		}
		changed, changedPage, err := reload()
		if err != nil {
			return err
		}
		return changeNotionDatabaseTrash(event, ygo, identity, changed, changedPage, true)
	}
	if err := changeNotionDatabaseTrash(event, ygo, identity, container, page, false); err != nil {
		return err
	}
	restored, restoredPage, err := reload()
	if err == nil {
		err = updateNotionDatabase(event, ygo, identity, restored, restoredPage, rest)
	}
	if err != nil {
		again, againPage, reloadErr := reload()
		if reloadErr == nil {
			reloadErr = changeNotionDatabaseTrash(event, ygo, identity, again, againPage, true)
		}
		if reloadErr != nil {
			return errors.Join(err, reloadErr)
		}
	}
	return err
}
