package app

import (
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	productFileReferenceOwnerSitePreviewImage = "site_preview_image"
	productFileReferenceOwnerSiteFavicon      = "site_favicon"
)

// A Google Analytics 4 measurement ID; it is written into the published
// site's HTML, so nothing else is accepted.
// A measurement ID (G-) or a Google tag ID (GT-), as Notion takes either (seen 2026-11-06).
var googleAnalyticsMeasurementIDPattern = regexp.MustCompile(`^GT?-[A-Z0-9]{4,20}$`)

// Site images are shown to anyone who opens the published site, so only
// raster image types are served.
var publicSiteAssetContentTypes = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {},
	"image/x-icon": {}, "image/vnd.microsoft.icon": {},
}

type storedPublicSitePreview struct {
	PreviewImageFileID string `db:"preview_image_file_id"`
	FaviconFileID      string `db:"favicon_file_id"`
	GoogleAnalyticsID  string `db:"google_analytics_id"`
	AllowIndexing      bool   `db:"allow_indexing"`
	AllowDuplicate     bool   `db:"allow_duplicate"`
	ShowShare          bool   `db:"show_share"`
}

func normalizePublicSitePreviewInput(input *publicMutationInput) {
	input.PreviewImageFileID = strings.TrimSpace(input.PreviewImageFileID)
	input.FaviconFileID = strings.TrimSpace(input.FaviconFileID)
	input.GoogleAnalyticsID = strings.ToUpper(strings.TrimSpace(input.GoogleAnalyticsID))
}

// validPublicSitePreviewInput tells a site's share preview image, favicon
// and Analytics ID each left out or of their form; validatePublicMutationInput
// asks each in turn, to name the one that isn't.
func validPublicSitePreviewInput(input publicMutationInput) bool {
	return validPublicSiteFileID(input.PreviewImageFileID) && validPublicSiteFileID(input.FaviconFileID) &&
		validGoogleAnalyticsID(input.GoogleAnalyticsID)
}

// validPublicSiteFileID tells a site's image left out or named by a file id.
func validPublicSiteFileID(fileID string) bool {
	return fileID == "" || workspaceExportFileRecordIDPattern.MatchString(fileID)
}

// validGoogleAnalyticsID tells a site's Analytics ID left out or of its form.
func validGoogleAnalyticsID(id string) bool {
	return id == "" || googleAnalyticsMeasurementIDPattern.MatchString(id)
}

func loadPublicSitePreview(app core.App, siteID string) (storedPublicSitePreview, error) {
	preview := storedPublicSitePreview{}
	err := app.DB().NewQuery(`
		SELECT [[preview_image_file_id]], [[favicon_file_id]], [[google_analytics_id]], [[allow_indexing]], [[allow_duplicate]],
			[[show_share]]
		FROM {{leafen_public_sites}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": siteID}).One(&preview)
	return preview, err
}

// applyPublicSitePreview saves the share preview image, favicon, Google
// Analytics ID, whether visitors may duplicate its pages ("템플릿으로 복제")
// and whether search engines may index a site being published
// (left as it was when not asked). The images must be the
// workspace's own raster images, and the site keeps them alive through file
// references owned by the site.
func applyPublicSitePreview(app core.App, state storedPublicMutationState, input publicMutationInput) error {
	// Each image the site is given, named by its own field when it can't be
	// the site's.
	requested := map[string]string{}
	fileIDs := []string{}
	for _, image := range []struct{ field, owner, fileID string }{
		{publicMutationFieldPreviewImageFileID, productFileReferenceOwnerSitePreviewImage, input.PreviewImageFileID},
		{publicMutationFieldFaviconFileID, productFileReferenceOwnerSiteFavicon, input.FaviconFileID},
	} {
		if image.fileID == "" {
			continue
		}
		requested[image.owner] = image.fileID
		if !slices.Contains(fileIDs, image.fileID) {
			fileIDs = append(fileIDs, image.fileID)
		}
		files, err := loadGraphFileAuthorityRecords(app, state.WorkspaceID, []string{image.fileID})
		if err != nil {
			return publicMutationFieldRefused(image.field, "%s names no file of the workspace", image.field)
		}
		for _, file := range files {
			if _, allowed := publicSiteAssetContentTypes[strings.ToLower(file.ContentType)]; !allowed {
				return publicMutationFieldRefused(image.field, "%s names a file that isn't a PNG, JPEG, GIF, WebP or ICO image", image.field)
			}
		}
	}
	var indexing, duplicate, share any
	if input.AllowIndexing != nil {
		indexing = *input.AllowIndexing
	}
	if input.AllowDuplicate != nil {
		duplicate = *input.AllowDuplicate
	}
	if input.ShowShare != nil {
		share = *input.ShowShare
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_sites}}
		SET [[preview_image_file_id]] = {:preview}, [[favicon_file_id]] = {:favicon},
			[[google_analytics_id]] = {:analytics}, [[allow_indexing]] = COALESCE({:indexing}, [[allow_indexing]]),
			[[allow_duplicate]] = COALESCE({:duplicate}, [[allow_duplicate]]),
			[[show_share]] = COALESCE({:share}, [[show_share]])
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[root_page_id]] = {:pageId}
	`).Bind(dbx.Params{
		"preview": input.PreviewImageFileID, "favicon": input.FaviconFileID,
		"analytics": input.GoogleAnalyticsID, "indexing": indexing, "duplicate": duplicate, "share": share, "id": state.SiteID,
		"workspaceId": state.WorkspaceID, "pageId": state.PageID,
	}).Execute(); err != nil {
		return err
	}
	prior := []string{}
	if err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId} AND [[owner_id]] = {:siteId}
			AND [[owner_kind]] IN ({:previewOwner}, {:faviconOwner})
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": state.WorkspaceID, "pageId": state.PageID, "siteId": state.SiteID,
		"previewOwner": productFileReferenceOwnerSitePreviewImage,
		"faviconOwner": productFileReferenceOwnerSiteFavicon,
	}).Column(&prior); err != nil {
		return err
	}
	for ownerKind, fileID := range requested {
		if _, err := app.DB().Insert("leafen_file_references", dbx.Params{
			"workspace_id": state.WorkspaceID, "page_id": state.PageID, "owner_kind": ownerKind,
			"owner_id": state.SiteID, "property_id": "", "file_id": fileID,
		}).Execute(); err != nil {
			return err
		}
	}
	return reconcileProductFileReferenceLifecycle(app, state.WorkspaceID, append(prior, fileIDs...))
}
