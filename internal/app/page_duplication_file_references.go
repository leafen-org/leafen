package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func remapPageDuplicationPageFiles(
	app core.App,
	jobID string,
	source *core.Record,
) (string, string, error) {
	icon := source.GetString("icon")
	cover := source.GetString("cover")
	iconID, iconManaged := productFilePresentationID(icon)
	iconManaged = iconManaged && source.GetString("icon_type") == "image"
	coverID, coverManaged := productFilePresentationID(cover)
	ids := []string{}
	if iconManaged {
		ids = append(ids, iconID)
	}
	if coverManaged {
		ids = append(ids, coverID)
	}
	mappings, err := pageDuplicationReferenceMappings(app, jobID, "file", ids)
	if err != nil {
		return "", "", err
	}
	// The top of a copy exists before its files are known; it takes them when
	// its turn comes (refreshPageDuplicationCopyTop).
	if iconManaged {
		if mappings[iconID] == "" {
			icon = ""
		} else {
			icon = productFilePresentationWithID(icon, mappings[iconID])
		}
	}
	if coverManaged {
		if mappings[coverID] == "" {
			cover = ""
		} else {
			cover = productFilePresentationWithID(cover, mappings[coverID])
		}
	}
	// A workspace emoji icon names the one a copy into another workspace
	// brings (page_duplication_custom_emoji.go).
	icon, err = pageDuplicationCustomEmojiIcon(app, jobID, icon, source.GetString("icon_type"))
	return icon, cover, err
}

func recordPageDuplicationBlockFileReferences(
	app core.App,
	job storedPageDuplicationJob,
	pageID string,
	blocks []pageDuplicationSourceBlock,
) error {
	items := map[string]struct{}{}
	owners := map[string]string{}
	for _, block := range blocks {
		candidate := &graphBlock{Type: block.Type, Content: []byte(block.Content)}
		if graphBlockCarriesManagedFileReference(candidate) {
			if _, err := parseGraphFileBlockContent([]byte(block.Content)); err != nil {
				return err
			}
		}
		// A file block's file was copied for the copy; a callout's uploaded
		// icon is the same picture, kept by both blocks (block_kept_file.go).
		fileID := graphBlockKeptFileID(candidate)
		if fileID == "" {
			continue
		}
		items[fileID] = struct{}{}
		owners[block.TargetID] = fileID
	}
	fileIDs := make([]string, 0, len(items))
	for fileID := range items {
		fileIDs = append(fileIDs, fileID)
	}
	if len(fileIDs) == 0 {
		return nil
	}
	if _, err := loadGraphFileAuthorityRecords(app, job.WorkspaceID, fileIDs); err != nil {
		return err
	}
	params := dbx.Params{
		"workspaceId": job.WorkspaceID, "pageId": pageID,
		"ownerKind": productFileReferenceOwnerBlock,
	}
	values := make([]string, 0, len(owners))
	index := 0
	for ownerID, fileID := range owners {
		params[fmt.Sprintf("owner%d", index)] = ownerID
		params[fmt.Sprintf("file%d", index)] = fileID
		values = append(values, fmt.Sprintf(
			"({:workspaceId},{:pageId},{:ownerKind},{:owner%d},'',{:file%d})", index, index,
		))
		index++
	}
	if _, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_file_references}}
			([[workspace_id]],[[page_id]],[[owner_kind]],[[owner_id]],[[property_id]],[[file_id]])
		VALUES ` + strings.Join(values, ","),
	).Bind(params).Execute(); err != nil {
		return err
	}
	return reconcileProductFileReferenceLifecycle(app, job.WorkspaceID, fileIDs)
}
