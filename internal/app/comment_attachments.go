package app

import "github.com/pocketbase/pocketbase/core"

// attachCommentAttachments sets each comment record's attachments: the
// workspace files its body names, in the body's order, with their names,
// types and sizes as the files hold them now. A file that is gone, being
// deleted or still uploading is left out, as is a body that names none.
func attachCommentAttachments(app core.App, records []commentRecord) error {
	named := make([][]string, len(records))
	byWorkspace := map[string][]string{}
	for index := range records {
		records[index].Attachments = nil
		ids, err := commentAttachmentIDs(records[index].Body)
		if err != nil || len(ids) == 0 {
			continue
		}
		named[index] = ids
		byWorkspace[records[index].WorkspaceID] = append(byWorkspace[records[index].WorkspaceID], ids...)
	}
	files := map[string]graphFileBlockContent{}
	for workspaceID, ids := range byWorkspace {
		found, err := availableGraphFileRecords(app, workspaceID, uniqueStrings(ids))
		if err != nil {
			return err
		}
		for id, file := range found {
			files[workspaceID+"/"+id] = file
		}
	}
	for index, ids := range named {
		for _, id := range ids {
			if file, ok := files[records[index].WorkspaceID+"/"+id]; ok {
				records[index].Attachments = append(records[index].Attachments, commentAttachment{
					ID: id, Name: file.Name, ContentType: file.ContentType, Size: file.Size,
				})
			}
		}
	}
	return nil
}

// authorizeCommentAttachmentFiles refuses a comment that names a file its
// author can't read, but for one it already named: everyone who reads the
// comment may read its files (authorizeReferencedProductFileRead).
func authorizeCommentAttachmentFiles(app core.App, actorID string, fileIDs, kept []string) error {
	already := make(map[string]bool, len(kept))
	for _, id := range kept {
		already[id] = true
	}
	for _, id := range uniqueStrings(fileIDs) {
		if already[id] {
			continue
		}
		if _, _, err := authorizeWorkspaceFile(app, actorID, id, permissionWorkspaceRead); isPermissionDenied(err) {
			return errCommentInvalid
		} else if err != nil {
			return err
		}
	}
	return nil
}
