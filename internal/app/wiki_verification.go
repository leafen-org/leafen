package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func cancelWikiVerificationGeneration(app core.App, page *core.Record) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_verification_queue}}
		SET [[status]]='cancelled',[[lease_token]]='',[[lease_until]]=0,
			[[completed_at]]={:updatedAt},[[updated_at]]={:updatedAt}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]=''
			AND [[status]] NOT IN ('completed','cancelled','failed')
	`).Bind(dbx.Params{"pageId": page.Id, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_verification_email_deliveries}}
		SET [[status]]='cancelled',[[lease_token]]='',[[lease_until]]=0,[[updated_at]]={:updatedAt}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]='' AND [[status]] NOT IN ('sent','skipped','failed','cancelled')
	`).Bind(dbx.Params{"pageId": page.Id, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	page.Set("wiki_verification_generation", page.GetInt("wiki_verification_generation")+1)
	page.Set("verified_at", "")
	page.Set("verified_by", "")
	page.Set("verification_expires_at", "")
	return app.Save(page)
}
