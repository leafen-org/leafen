package organization

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	leafen "leafen.org/leafen/internal/app"
)

func registerEnterpriseLegalHoldHooks(app core.App) {
	app.OnRecordDeleteExecute(leafen.PagesCollection).Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "leafen-legal-hold-page-delete",
		Priority: 100,
		Func: func(event *core.RecordEvent) error {
			if err := assertLegalHoldDeleteTransaction(event.App); err != nil {
				return err
			}
			workspaceID := event.Record.GetString("workspace")
			organizationID, err := leafen.LoadLegalHoldWorkspaceOrganization(event.App, workspaceID)
			if err != nil {
				return err
			}
			if err := leafen.AssertNoActiveLegalHoldForPermanentDelete(
				event.App, organizationID, leafen.LegalHoldDeletionTarget{
					WorkspaceID:      workspaceID,
					PageIDs:          []string{event.Record.Id},
					CustodianUserIDs: []string{event.Record.GetString("owner")},
				},
			); err != nil {
				return err
			}
			return event.Next()
		},
	})
	app.OnRecordDeleteExecute(leafen.WorkspacesCollection).Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "leafen-legal-hold-workspace-delete",
		Priority: 100,
		Func: func(event *core.RecordEvent) error {
			if err := assertLegalHoldDeleteTransaction(event.App); err != nil {
				return err
			}
			organizationID := event.Record.GetString("organization")
			if organizationID == "" {
				return leafen.ErrLegalHoldUnavailable
			}
			if err := leafen.AssertNoActiveLegalHoldForPermanentDelete(
				event.App, organizationID, leafen.LegalHoldDeletionTarget{
					WorkspaceID:     event.Record.Id,
					EntireWorkspace: true,
				},
			); err != nil {
				return err
			}
			return event.Next()
		},
	})
}
