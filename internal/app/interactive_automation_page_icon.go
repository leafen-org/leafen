package app

import (
	"errors"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A run's "페이지 아이콘 편집…", as Notion's (seen 2026-11-21 in a database's
// automation and a button property; a page's button and a schedule offer
// none): the page that set it off, or the row whose button was pressed,
// takes the icon the step names — an emoji, a built-in icon or the
// workspace's own emoji — in place of the one it had, as a page's icon is
// set (database_row_page_presentation.go).

// interactiveAutomationRowPageWrites are what a run writes on its rows' own
// pages after its rows' values: the rows it archives and the icons it sets.
type interactiveAutomationRowPageWrites struct {
	// Archives are the rows it archives (interactive_automation_archive.go).
	Archives []plannedInteractiveAutomationArchive
	// PageIcons are the rows whose page icon it sets, the last step's for each.
	PageIcons []plannedInteractiveAutomationPageIcon
}

// plannedInteractiveAutomationPageIcon is a row whose page takes an icon,
// with the page holding its database.
type plannedInteractiveAutomationPageIcon struct {
	RowID      string
	DatabaseID string
	PageID     string
	Icon       string
	IconType   string
}

// interactiveAutomationUndoIcon is a row page's icon before a button's run
// set it, and the one the run set, for its notice's "실행 취소".
type interactiveAutomationUndoIcon struct {
	PageID      string `json:"pageId"`
	Icon        string `json:"icon"`
	IconType    string `json:"iconType"`
	SetIcon     string `json:"setIcon"`
	SetIconType string `json:"setIconType"`
}

// normalizeInteractiveAutomationPageIcon checks a set_page_icon step: it
// acts on the triggering page and names an icon a page takes, its picker
// offering no picture (Notion's "아이콘 · 이모지").
func normalizeInteractiveAutomationPageIcon(action *interactiveAutomationAction) error {
	if !automationTargetIsTrigger(action.Target) {
		return errAutomationTargetNotTrigger
	}
	action.Icon, action.IconType = strings.TrimSpace(action.Icon), strings.TrimSpace(action.IconType)
	switch action.IconType {
	case "emoji", nativeIconType, "custom_emoji":
	default:
		return automationRefused("iconType is emoji, icon or custom_emoji")
	}
	if !validPagePresentationInput(pageMetadataInput{Icon: &action.Icon, IconType: &action.IconType}) {
		return automationRefused("icon is the emoji, the built-in icon or the workspace emoji's id the page takes")
	}
	return nil
}

// refuseInteractiveAutomationStrayIcon refuses an icon on a step other than set_page_icon.
func refuseInteractiveAutomationStrayIcon(action interactiveAutomationAction) error {
	if action.Type != interactiveAutomationActionSetPageIcon && (action.Icon != "" || action.IconType != "") {
		return automationRefused("icon and iconType go only with type set_page_icon")
	}
	return nil
}

// interactiveAutomationWritesTriggerPage reports whether a step writes the
// page that set it off: its property, its icon, or its edit or archive
// aimed at this page — none of which a schedule or People's rows have.
func interactiveAutomationWritesTriggerPage(action interactiveAutomationAction) bool {
	return action.Type == interactiveAutomationActionEditProperty || action.Type == interactiveAutomationActionSetPageIcon ||
		interactiveAutomationTargetsThisPage(action)
}

// setPageIcon plans the trigger row's page icon; a later step's icon for the
// same row takes the place of an earlier one's.
func (plan *interactiveAutomationPlan) setPageIcon(
	app core.App, authorization pageAuthorization, source interactiveAutomationSource, action interactiveAutomationAction,
) error {
	if source.TriggerRowID == "" {
		return errAutomationNoTriggerRow
	}
	// The page that set off a run may have gone to the trash since: then
	// there is nothing to set, as with a page it would edit or archive.
	if err := plan.rowsExist(app, authorization, source.TriggerDatabaseID, []string{source.TriggerRowID}); err != nil {
		return err
	}
	planned := plannedInteractiveAutomationPageIcon{
		RowID: source.TriggerRowID, DatabaseID: source.TriggerDatabaseID, PageID: plan.pageOf(source.TriggerDatabaseID),
		Icon: action.Icon, IconType: action.IconType,
	}
	for index := range plan.PageIcons {
		if plan.PageIcons[index].RowID == planned.RowID {
			plan.PageIcons[index] = planned
			return nil
		}
	}
	plan.PageIcons = append(plan.PageIcons, planned)
	return nil
}

// appliedInteractiveAutomationPageIcon is a row page whose icon a run set,
// with the icon it had before.
type appliedInteractiveAutomationPageIcon struct {
	plannedInteractiveAutomationPageIcon
	Before interactiveAutomationUndoIcon
}

// applyInteractiveAutomationPageIcons sets the run's row page icons, a page
// already showing the icon staying as it was, and gives back those it set; a
// database's automation sets them as Leafen's, as it writes. Each row's
// database reads it again where it is open.
func applyInteractiveAutomationPageIcons(
	app core.App, authorization pageAuthorization, plan interactiveAutomationPlan,
) ([]appliedInteractiveAutomationPageIcon, error) {
	if len(plan.PageIcons) == 0 {
		return nil, nil
	}
	actor := authorization.Workspace.PrincipalID
	if plan.SystemAuthored {
		actor = leafenSystemAuthorID
	}
	applied := make([]appliedInteractiveAutomationPageIcon, 0, len(plan.PageIcons))
	for _, icon := range plan.PageIcons {
		authority, err := readDatabaseRowPageAuthority(app, icon.RowID)
		if err != nil {
			return nil, err
		}
		if authority.Icon == icon.Icon && authority.IconType == icon.IconType {
			continue
		}
		if err := updateDatabaseRowPagePresentation(
			app, icon.RowID, authorization.Workspace.WorkspaceID, icon.PageID, actor, icon.Icon, icon.IconType,
		); err != nil {
			return nil, err
		}
		if err := announceDatabaseRowPageChange(app, icon.RowID); err != nil {
			return nil, err
		}
		applied = append(applied, appliedInteractiveAutomationPageIcon{plannedInteractiveAutomationPageIcon: icon,
			Before: interactiveAutomationUndoIcon{PageID: icon.PageID, Icon: authority.Icon, IconType: authority.IconType,
				SetIcon: icon.Icon, SetIconType: icon.IconType}})
	}
	return applied, nil
}

// applyInteractiveAutomationRowPageWrites sets a database automation's row
// page icons, then archives its rows: what it writes on its rows' own pages.
func applyInteractiveAutomationRowPageWrites(app core.App, authorization pageAuthorization, plan interactiveAutomationPlan) error {
	if _, err := applyInteractiveAutomationPageIcons(app, authorization, plan); err != nil {
		return err
	}
	_, err := applyInteractiveAutomationArchives(app, authorization, plan)
	return err
}

// noteInteractiveAutomationUndoIcons keeps the icons a button's run replaced,
// for its notice's "실행 취소".
func noteInteractiveAutomationUndoIcons(progress *interactiveAutomationProgress, applied []appliedInteractiveAutomationPageIcon) {
	for _, icon := range applied {
		if progress.UndoIcons == nil {
			progress.UndoIcons = map[string]interactiveAutomationUndoIcon{}
		}
		progress.UndoIcons[icon.RowID] = icon.Before
	}
}

// restoreInteractiveAutomationPageIcons takes back the icons of a button's
// run, as Notion's "실행 취소" (seen 2026-11-21): each row page still showing
// the icon the run set takes its icon from before; one changed since stays.
func restoreInteractiveAutomationPageIcons(app core.App, authorization pageAuthorization, progress *interactiveAutomationProgress) error {
	if progress == nil || len(progress.UndoIcons) == 0 {
		return nil
	}
	ids := make([]string, 0, len(progress.UndoIcons))
	for id := range progress.UndoIcons {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		before := progress.UndoIcons[id]
		authority, err := readDatabaseRowPageAuthority(app, id)
		if err != nil {
			if errors.Is(err, errDatabaseRowPageNotFound) {
				continue
			}
			return err
		}
		if authority.Icon != before.SetIcon || authority.IconType != before.SetIconType {
			continue
		}
		if err := updateDatabaseRowPagePresentation(
			app, id, authorization.Workspace.WorkspaceID, before.PageID, authorization.Workspace.PrincipalID, before.Icon, before.IconType,
		); err != nil {
			return err
		}
		if err := announceDatabaseRowPageChange(app, id); err != nil {
			return err
		}
	}
	return nil
}
