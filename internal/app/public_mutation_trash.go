package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// refusePublicMutationInTrash keeps the sharing of a page in the trash, or
// under a page in it, as it is until it is restored: Notion's share window
// reads it there, its field, roles and general access greyed and its "게시"
// tab saying a page in the trash can't be published (seen 2026-11-13). What
// the page made public may still be turned off (authorizePublicMutationFor).
func refusePublicMutationInTrash(app core.App, pageID string) error {
	trashed, err := pageInTrashPath(app, pageID)
	if err != nil {
		return err
	}
	if trashed {
		return errPublicMutationInTrash
	}
	return nil
}

// refusePublicMutationInTrashFor is refusePublicMutationInTrash for a change
// from the share window, but a page whose own site is published keeps its
// site's settings in reach: Notion's "게시" tab for a published page in the
// trash is its usual published tab, every row there, with no restore in it
// (seen 2026-11-24) — so the site's settings are changed there as ever, and
// show once the page is restored (its site isn't read while it is in the
// trash, authorizePublicContentPage). A site not published yet still waits
// for the page to be restored.
func refusePublicMutationInTrashFor(app core.App, pageID string, input publicMutationInput) error {
	if input.Action == publicMutationActionPublishSite {
		published := false
		if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_public_sites}}
			WHERE [[root_page_id]]={:page} AND [[published]]=1)`).Bind(dbx.Params{"page": pageID}).Row(&published); err != nil {
			return err
		}
		if published {
			return nil
		}
	}
	return refusePublicMutationInTrash(app, pageID)
}
