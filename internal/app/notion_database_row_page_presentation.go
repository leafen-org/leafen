package app

import (
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// A row page's icon and cover are written on its page record, for the Public
// API and workspace imports alike.

func applyNotionDatabaseRowPagePresentation(
	app core.App,
	rowID, workspaceID, containerPageID, actorID string,
	presentation notionPagePresentationRequest,
) error {
	if presentation.Icon == nil && presentation.IconType == nil && presentation.Cover == nil {
		return nil
	}
	if err := writeNotionDatabaseRowPagePresentation(app, rowID, workspaceID, containerPageID, actorID, presentation); err != nil {
		return err
	}
	return announceDatabaseRowPageChange(app, rowID)
}

func writeNotionDatabaseRowPagePresentation(
	app core.App,
	rowID, workspaceID, containerPageID, actorID string,
	presentation notionPagePresentationRequest,
) error {
	if (presentation.Icon == nil) != (presentation.IconType == nil) {
		return errNotionDatabaseRowPageProjectionInvalid
	}
	if err := validateNotionPagePresentationReferences(
		app, actorID, workspaceID, &presentation,
	); err != nil {
		return err
	}
	// A new icon or cover meets the DLP policy as a page's does (database_row_page_dlp.go).
	if err := enforceDatabaseRowPageLookDLP(app, workspaceID, rowID, presentation.Icon, presentation.Cover); err != nil {
		return err
	}
	if presentation.Cover != nil {
		// A database page's cover is its page record's, as a page's is.
		if utf8.RuneCountInString(*presentation.Cover) > maxPageCoverRunes {
			return &notionDatabaseRowPageRequestError{message: "database row-page cover is too long"}
		}
		found, err := setRowPageRecordCover(app, rowID, workspaceID, actorID, *presentation.Cover, nil)
		if err != nil {
			return err
		}
		if !found {
			return errDatabaseRowPageConflict
		}
		if err := replaceProductFilePageReferences(app, workspaceID, rowID, "", "none", *presentation.Cover); err != nil {
			return err
		}
		if presentation.Icon == nil {
			return touchDatabaseRowPageAuthority(app, rowID, time.Now().UTC().UnixMilli())
		}
	}
	if utf8.RuneCountInString(*presentation.Icon) > maxPageIconRunes ||
		len(*presentation.IconType) > maxPageIconType {
		return &notionDatabaseRowPageRequestError{message: "database row-page icon is too long"}
	}
	switch *presentation.IconType {
	case "none", "emoji", "image", "custom_emoji", nativeIconType:
	default:
		return &notionDatabaseRowPageRequestError{message: "database row-page icon type is invalid"}
	}
	return updateDatabaseRowPagePresentation(
		app, rowID, workspaceID, containerPageID, actorID,
		*presentation.Icon, *presentation.IconType,
	)
}

func updateDatabaseRowPagePresentation(
	app core.App,
	rowID, workspaceID, containerPageID, actorID, icon, iconType string,
) error {
	found, err := setRowPageRecordIcon(app, rowID, workspaceID, containerPageID, actorID, icon, iconType)
	if err != nil {
		return err
	}
	if !found {
		return errDatabaseRowPageConflict
	}
	if err := touchDatabaseRowPageAuthority(app, rowID, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}
	return replaceProductFileDatabaseRowPageIconReference(
		app, workspaceID, containerPageID, rowID, icon, iconType,
	)
}
