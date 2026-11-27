package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page copied into another workspace — moved there, or a published page a
// visitor copies to their own — brings the workspace emoji its text, its
// icons and its databases' templates use as carried ones
// (custom_emoji_carried.go), as Notion's copy in another workspace keeps
// showing them. The copy names each by a new ID as it meets it, and the
// emoji's picture joins the copy's files; once that is copied the emoji is
// there. A copy within its workspace keeps naming the workspace's own.

// pageDuplicationCustomEmojiMappings maps the workspace emoji of the copy's
// source that ids name to the carried ones the copy brings, mapping those
// it hadn't met. A copy within its workspace, or a job that isn't a page's
// copy or a block copy from another workspace (block_copy_workspaces.go),
// maps none.
func pageDuplicationCustomEmojiMappings(app core.App, jobID string, ids map[string]struct{}) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	job, err := loadPageDuplicationJobByID(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) {
		return blockCopyCustomEmojiMappings(app, jobID, ids)
	}
	if err != nil {
		return nil, err
	}
	if !job.crossWorkspace() {
		return map[string]string{}, nil
	}
	return carriedCustomEmojiMappings(app, job, pageDuplicationCustomEmojisTable, ids,
		func(emojis []storedCustomEmoji, now int64) error {
			pictures := make([]pageDuplicationFileReference, 0, len(emojis))
			for _, emoji := range emojis {
				pictures = append(pictures, pageDuplicationFileReference{
					SourcePageID: job.SourceRootPageID, TargetPageID: job.TargetRootPageID, SourceFileID: emoji.FileID,
				})
			}
			// Their pictures are copied as the copy's other files are: the copy
			// waits for them (advancePageDuplication) before it goes on.
			if err := storePageDuplicationFileMappings(app, job.ID, pictures); err != nil {
				return err
			}
			if _, err := app.DB().NewQuery(`
				UPDATE {{leafen_page_duplication_file_state}} SET [[files_complete]]=0, [[updated_at]]={:now}
				WHERE [[job_id]]={:jobId} AND EXISTS (SELECT 1 FROM {{leafen_page_duplication_files}}
					WHERE [[job_id]]={:jobId} AND [[status]]='pending')
			`).Bind(dbx.Params{"jobId": job.ID, "now": now}).Execute(); err != nil {
				return err
			}
			return settlePageDuplicationCarriedCustomEmojis(app, job)
		})
}

// carriedCustomEmojiMappings maps, for a copy into another workspace, the
// emoji of its source that ids name to the carried ones it brings, as the
// copy's table keeps them; those it meets first are named now, and bring
// takes their pictures along.
func carriedCustomEmojiMappings(
	app core.App, job storedPageDuplicationJob, table string, ids map[string]struct{},
	bring func([]storedCustomEmoji, int64) error,
) (map[string]string, error) {
	mappings := map[string]string{}
	sourceIDs := referenceIDs(ids)
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, err
	}
	stored := []struct {
		SourceID string `db:"source_emoji_id"`
		TargetID string `db:"target_emoji_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_emoji_id]],[[target_emoji_id]] FROM {{` + table + `}}
		WHERE [[job_id]]={:jobId} AND [[source_emoji_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"jobId": job.ID, "ids": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	for _, mapping := range stored {
		mappings[mapping.SourceID] = mapping.TargetID
	}
	missing := []string{}
	for _, id := range sourceIDs {
		if _, mapped := mappings[id]; !mapped {
			missing = append(missing, id)
		}
	}
	emojis, err := loadCarriableCustomEmojis(app, job.sourceWorkspace(), missing)
	if err != nil || len(emojis) == 0 {
		return mappings, err
	}
	now := time.Now().UTC().UnixMilli()
	for _, emoji := range emojis {
		targetID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(
			"leafen-carried-custom-emoji-v1\x00"+job.ID+"\x00"+emoji.ID,
		)).String()
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{` + table + `}}
				([[job_id]],[[source_emoji_id]],[[target_emoji_id]],[[created_at]])
			VALUES ({:jobId},{:sourceId},{:targetId},{:now})
			ON CONFLICT ([[job_id]],[[source_emoji_id]]) DO NOTHING
		`).Bind(dbx.Params{"jobId": job.ID, "sourceId": emoji.ID, "targetId": targetID, "now": now}).Execute(); err != nil {
			return nil, err
		}
		mappings[emoji.ID] = targetID
	}
	return mappings, bring(emojis, now)
}

// settlePageDuplicationCarriedCustomEmojis adds to the copy's workspace each
// carried emoji the copy brings whose picture is copied.
func settlePageDuplicationCarriedCustomEmojis(app core.App, job storedPageDuplicationJob) error {
	return settleCarriedCustomEmojis(app, job, pageDuplicationCustomEmojisTable, pageDuplicationFilesTable)
}

// settleCarriedCustomEmojis adds to the copy's workspace each carried emoji
// the copy's tables say it brings whose picture is copied: by the name and
// picture its source has, which is how its text shows it.
func settleCarriedCustomEmojis(app core.App, job storedPageDuplicationJob, emojiTable, filesTable string) error {
	rows := []struct {
		TargetID  string `db:"target_id"`
		Name      string `db:"name"`
		FileID    string `db:"target_file_id"`
		CreatedAt int64  `db:"created_at"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[brought]].[[target_emoji_id]] AS [[target_id]],[[source]].[[name]],[[file]].[[target_file_id]],
			[[source]].[[created_at]],[[source]].[[updated_at]]
		FROM {{` + emojiTable + `}} AS [[brought]]
		JOIN {{leafen_custom_emojis}} AS [[source]] ON [[source]].[[id]]=[[brought]].[[source_emoji_id]]
			AND [[source]].[[workspace_id]]={:sourceWorkspaceId}
		JOIN {{` + filesTable + `}} AS [[file]] ON [[file]].[[job_id]]=[[brought]].[[job_id]]
			AND [[file]].[[source_file_id]]=[[source]].[[file_id]] AND [[file]].[[status]]='copied'
		WHERE [[brought]].[[job_id]]={:jobId}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_custom_emojis}} WHERE [[id]]=[[brought]].[[target_emoji_id]])
		ORDER BY [[brought]].[[target_emoji_id]]
	`).Bind(dbx.Params{"jobId": job.ID, "sourceWorkspaceId": job.sourceWorkspace()}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := app.DB().Insert(customEmojisTable, dbx.Params{
			"id": row.TargetID, "workspace_id": job.WorkspaceID, "name": row.Name,
			"name_key": customEmojiCarriedNameKey(row.TargetID), "file_id": row.FileID, "carried": true,
			"created_by": job.ActorID, "revision": 1, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		}).Execute(); err != nil {
			return err
		}
		if err := reconcileProductFileReferenceLifecycle(app, job.WorkspaceID, []string{row.FileID}); err != nil {
			return err
		}
	}
	return nil
}

// dropPageDuplicationCarriedCustomEmojis takes back, from a copy that
// failed, the carried emoji it brought with the picture fileID, before the
// picture goes.
func dropPageDuplicationCarriedCustomEmojis(app core.App, job storedPageDuplicationJob, fileID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_custom_emojis}}
		WHERE [[workspace_id]]={:workspaceId} AND [[carried]]=1 AND [[file_id]]={:fileId}
			AND [[id]] IN (SELECT [[target_emoji_id]] FROM {{leafen_page_duplication_custom_emojis}}
				WHERE [[job_id]]={:jobId})
	`).Bind(dbx.Params{"workspaceId": job.WorkspaceID, "fileId": fileID, "jobId": job.ID}).Execute()
	return err
}

// rewritePageDuplicationCustomEmoji names, in copied content, the carried
// emoji the copy brings for the ones it named.
func rewritePageDuplicationCustomEmoji(value any, mappings map[string]string) {
	if len(mappings) == 0 {
		return
	}
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			rewritePageDuplicationCustomEmoji(child, mappings)
		}
	case map[string]any:
		pageDuplicationReplaceReference(typed, "customEmojiId", mappings)
		if typed["iconType"] == "custom_emoji" {
			pageDuplicationReplaceReference(typed, "icon", mappings)
		}
		for _, child := range typed {
			rewritePageDuplicationCustomEmoji(child, mappings)
		}
	}
}

// remapPageDuplicationDeltaCustomEmoji names the brought emoji in copied
// block text.
func remapPageDuplicationDeltaCustomEmoji(app core.App, jobID string, deltas [][]crdt.Delta) error {
	ids := map[string]struct{}{}
	for _, delta := range deltas {
		for _, operation := range delta {
			if id, ok := pageDuplicationString(operation.Attributes["customEmojiId"]); ok {
				ids[id] = struct{}{}
			}
		}
	}
	mappings, err := pageDuplicationCustomEmojiMappings(app, jobID, ids)
	if err != nil || len(mappings) == 0 {
		return err
	}
	for deltaIndex, delta := range deltas {
		copied := false
		for index := range delta {
			id, ok := pageDuplicationString(delta[index].Attributes["customEmojiId"])
			target, found := mappings[id]
			if !ok || !found {
				continue
			}
			if !copied {
				delta, copied = append([]crdt.Delta(nil), delta...), true
				deltas[deltaIndex] = delta
			}
			attributes := make(crdt.Attributes, len(delta[index].Attributes))
			for key, value := range delta[index].Attributes {
				attributes[key] = value
			}
			attributes["customEmojiId"] = target
			delta[index].Attributes = attributes
		}
	}
	return nil
}

// remapPageDuplicationSpanCustomEmoji names the brought emoji in copied
// spans: a title's, a database value's, a description's.
func remapPageDuplicationSpanCustomEmoji(app core.App, jobID string, texts [][]graphTextSpan) error {
	ids := map[string]struct{}{}
	for _, spans := range texts {
		for _, span := range spans {
			if span.CustomEmojiID != "" {
				ids[span.CustomEmojiID] = struct{}{}
			}
		}
	}
	mappings, err := pageDuplicationCustomEmojiMappings(app, jobID, ids)
	if err != nil || len(mappings) == 0 {
		return err
	}
	for _, spans := range texts {
		for index := range spans {
			if target, found := mappings[spans[index].CustomEmojiID]; found {
				spans[index].CustomEmojiID = target
			}
		}
	}
	return nil
}

// pageDuplicationCustomEmojiIcon is a copied icon naming a workspace emoji
// (iconType custom_emoji) as the copy names it.
func pageDuplicationCustomEmojiIcon(app core.App, jobID, icon, iconType string) (string, error) {
	if iconType != "custom_emoji" || icon == "" {
		return icon, nil
	}
	mappings, err := pageDuplicationCustomEmojiMappings(app, jobID, map[string]struct{}{icon: {}})
	if err != nil {
		return "", err
	}
	if target, found := mappings[icon]; found {
		return target, nil
	}
	return icon, nil
}

// pageDuplicationTemplateCustomEmojiIcon is a copied template's icon, a
// workspace emoji's named as the copy names it.
func pageDuplicationTemplateCustomEmojiIcon(app core.App, jobID, icon string) (string, error) {
	id, custom := databaseTemplateCustomEmojiID(icon)
	if !custom {
		return icon, nil
	}
	mappings, err := pageDuplicationCustomEmojiMappings(app, jobID, map[string]struct{}{id: {}})
	if err != nil {
		return "", err
	}
	if target, found := mappings[id]; found {
		return databaseTemplateCustomEmojiPrefix + target, nil
	}
	return icon, nil
}

// pageDuplicationTitleSpans is a copied title's stored spans, the brought
// emoji named as the copy names them.
func pageDuplicationTitleSpans(app core.App, jobID, encoded string) (string, error) {
	spans := decodeStoredTextSpans(encoded)
	named := false
	for _, span := range spans {
		named = named || span.CustomEmojiID != ""
	}
	if !named {
		return encoded, nil
	}
	if err := remapPageDuplicationSpanCustomEmoji(app, jobID, [][]graphTextSpan{spans}); err != nil {
		return "", err
	}
	remapped, err := json.Marshal(spans)
	return string(remapped), err
}

// remapPageDuplicationRowCustomEmojiIcon names, on a database page copied
// into another workspace, the brought emoji its icon is.
func remapPageDuplicationRowCustomEmojiIcon(app core.App, jobID, rowID string) error {
	row := struct {
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[icon]],[[icon_type]] FROM {{leafen_pages}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": rowID}).One(&row); err != nil {
		return err
	}
	icon, err := pageDuplicationCustomEmojiIcon(app, jobID, row.Icon, row.IconType)
	if err != nil || icon == row.Icon {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[icon]]={:icon} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"icon": icon, "id": rowID}).Execute()
	return err
}

// pageDuplicationBodyCustomEmoji names the brought emoji in a copied
// database page's or template's body: its blocks' text and content.
func pageDuplicationBodyCustomEmoji(app core.App, jobID string) func([]newRowPageBlock) error {
	return func(body []newRowPageBlock) error {
		ids := map[string]struct{}{}
		for _, block := range body {
			usedCustomEmojiIDs(block.Content, ids)
			for _, span := range block.Spans {
				if span.CustomEmojiID != "" {
					ids[span.CustomEmojiID] = struct{}{}
				}
			}
		}
		mappings, err := pageDuplicationCustomEmojiMappings(app, jobID, ids)
		if err != nil || len(mappings) == 0 {
			return err
		}
		for index := range body {
			rewritePageDuplicationCustomEmoji(body[index].Content, mappings)
			for spanIndex, span := range body[index].Spans {
				if target, found := mappings[span.CustomEmojiID]; found {
					body[index].Spans[spanIndex].CustomEmojiID = target
				}
			}
		}
		return nil
	}
}
