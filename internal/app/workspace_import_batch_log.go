package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A batch of several files or a ZIP leaves a page saying what became of each
// file, among the person's private pages wherever the files went, as Notion's
// "Import Log" and "Zip Import" do (seen 2026-11-04). A batch of several files
// opens on it. A file brought in alone needs none. The page reads in the
// language the person imported in, where Notion's is English on any screen.

var workspaceImportBatchLogText = map[string]map[string]string{
	accountLanguageKo: {
		"title": "가져오기 기록 - %s", "zipTitle": "Zip 가져오기 - %s - %s", "summary": "파일 %d개 중 %d개를 가져왔습니다.", "failedSummary": " %d개는 실패했습니다.",
		"file": "파일 이름", "status": "상태", "page": "가져온 페이지", "message": "메시지",
		"imported": "성공", "failed": "실패", "skipped": "건너뜀", "cancelled": "취소됨", "importedMessage": "성공적으로 가져왔습니다",
		"zipPages": "페이지 %d개를 가져왔습니다.", "zipPagesAttachments": "페이지 %d개와 첨부 %d개를 가져왔습니다.",
		"zipDebug": "디버깅 정보 (리펜 확인용)", "zipDebugWorkspace": "워크스페이스 ID:", "zipDebugImport": "가져오기 ID:",
		"zipMappings": "페이지 매핑 (처음 20개만)", "zipNumber": "번호", "zipSource": "원본 파일 경로", "zipPage": "리펜 페이지",
		"zipUnreferenced": "쓰이지 않은 첨부", "zipFilePath": "파일 경로", "zipAttachmentLink": "첨부 링크",
		"zipOutput": "출력 파일", "zipMappingFile": "LeafenPageMapping.json",
	},
	accountLanguageEn: {
		"title": "Import Log - %s", "zipTitle": "Zip Import - %s - %s", "summary": "%[2]d of %[1]d files imported successfully.", "failedSummary": " %d file(s) failed.",
		"file": "File name", "status": "Status", "page": "Imported page", "message": "Message",
		"imported": "Success", "failed": "Failure", "skipped": "Skipped", "cancelled": "Cancelled", "importedMessage": "Imported successfully",
		"zipPages": "%d pages were imported successfully.", "zipPagesAttachments": "%d pages and %d attachments were imported successfully.",
		"zipDebug": "Debugging Information (For Leafen usage)", "zipDebugWorkspace": "Workspace ID:", "zipDebugImport": "Import ID:",
		"zipMappings": "Page Mappings (First 20 only)", "zipNumber": "Number", "zipSource": "Source File Path", "zipPage": "Leafen Page",
		"zipUnreferenced": "Unreferenced Attachments", "zipFilePath": "File Path", "zipAttachmentLink": "Attachment Link",
		"zipOutput": "Output Files", "zipMappingFile": "LeafenPageMapping.json",
	},
}

var workspaceImportBatchLogCell = regexp.MustCompile(`[\r\n]+`)

var workspaceImportBatchLogMarks = regexp.MustCompile("[\\\\`*_\\[\\]<>~$]")

// workspaceImportBatchLogEscape keeps a name, path or message as it is
// written in its table cell, on one line and as words: an address it seems
// to hold stays unlinked ("팀 자료/소개.md" is no link to "소개.md"), as
// Notion's log writes its paths and names (seen 2026-11-13).
func workspaceImportBatchLogEscape(value string) string {
	return notionMarkdownUnlinkedText(workspaceImportBatchLogCell.ReplaceAllString(strings.TrimSpace(value), " "), func(words string) string {
		return workspaceImportBatchLogMarks.ReplaceAllString(words, `\$0`)
	})
}

// writeWorkspaceImportBatchLog makes the page and, for several files, opens
// the batch on it. What stops it leaves the batch as it ended, without one.
func (r *durableJobRunner) writeWorkspaceImportBatchLog(job storedJob, payload workspaceImportJobPayload) error {
	items := []workspaceImportBatchLogItem{}
	if err := r.app.DB().NewQuery(`
		SELECT [[position]],[[parent_position]],[[kind]],[[name]],[[title]],[[page_id]],[[state]],[[message]],[[file_id]]
		FROM {{leafen_workspace_import_batch_items}}
		WHERE [[job_id]]={:jobId} ORDER BY [[position]]
	`).Bind(dbx.Params{"jobId": job.ID}).All(&items); err != nil {
		return err
	}
	zipName, files, imported, failed := "", 0, 0, 0
	for _, item := range items {
		if item.Kind == "folder" {
			if zipName == "" {
				zipName = item.Name
			}
			continue
		}
		files++
		switch item.State {
		case "imported":
			imported++
		case "failed":
			failed++
		}
	}
	if files < 2 && zipName == "" {
		return nil
	}
	language := payload.Locale
	text := workspaceImportBatchLogText[language]
	if text == nil {
		text, language = workspaceImportBatchLogText[accountLanguageEn], accountLanguageEn
	}
	now := r.clock.now().In(personLocation(r.app, job.Owner))
	date := now.Format("January 2, 2006")
	if language == accountLanguageKo {
		date = fmt.Sprintf("%d년 %d월 %d일", now.Year(), int(now.Month()), now.Day())
	}
	title := fmt.Sprintf(text["title"], date)
	if zipName != "" {
		// Notion's ZIP log shortens the month its several files' log spells
		// out ("Zip Import - 〈zip〉 - Nov 12, 2026", seen 2026-11-12).
		if language != accountLanguageKo {
			date = now.Format("Jan 2, 2006")
		}
		title = fmt.Sprintf(text["zipTitle"], zipName, date)
	}
	var zipLog workspaceImportZipLog
	var body strings.Builder
	if zipName != "" {
		var err error
		if zipLog, err = readWorkspaceImportZipLog(r.app, job.ID, zipName, items); err != nil {
			return err
		}
		body.WriteString(workspaceImportZipLogBody(text, zipLog, job.WorkspaceID, job.ID))
	} else {
		r.writeWorkspaceImportBatchFileTable(&body, text, items, files, imported, failed)
	}
	pageID := ""
	if err := r.app.RunInTransaction(func(tx core.App) error {
		response, status, err := admitPageCreationTransactionWithLock(tx, job.Owner, normalizePageCreationAdmissionInput(pageCreationAdmissionInput{
			RequestID: "workspace-import-log:" + job.ID, WorkspaceID: job.WorkspaceID, Title: title,
		}), pageLockIgnored)
		if err == nil && status == pageCreationApplied {
			pageID = response.Page.ID
		}
		return err
	}); err != nil || pageID == "" {
		return err
	}
	markdown := body.String()
	digest := sha256.Sum256([]byte("workspace-import-log\x00" + job.ID + "\x00" + markdown))
	if _, err := r.ygo.replacePageMarkdown(context.Background(), r.app, pageMarkdownReplacement{
		PrincipalID: job.Owner, PublicPageID: pageID, MutationID: "workspace-import-log:" + job.ID,
		Reader: principalMarkdownReader(job.Owner), PrepareInput: principalMarkdownMentionInput(job.Owner),
		RequestHash: hex.EncodeToString(digest[:]),
		Authorize: func(tx core.App) (pageAuthorization, error) {
			return authorizePageBody(tx, job.Owner, pageID, permissionWorkspaceWrite)
		},
		Next: func(string) (string, bool, error) { return markdown, true, nil },
	}); err != nil {
		return err
	}
	if zipName != "" {
		return r.addWorkspaceImportZipLogParts(job, payload.PageIDs, pageID, zipName, text, zipLog)
	}
	return r.scheduleWorkspaceImportBatch(job, 0, func(_ core.App, current *workspaceImportJobPayload) error {
		current.RootPageID, current.RootPageIDs = pageID, []string{pageID}
		return nil
	})
}

// writeWorkspaceImportBatchFileTable writes, as Notion's Import Log, the count
// in a box, then a table without a header row, each file numbered (seen
// 2026-11-05).
func (r *durableJobRunner) writeWorkspaceImportBatchFileTable(
	body *strings.Builder, text map[string]string, items []workspaceImportBatchLogItem, files, imported, failed int,
) {
	summary := fmt.Sprintf(text["summary"], files, imported)
	if failed > 0 {
		summary += fmt.Sprintf(text["failedSummary"], failed)
	}
	body.WriteString("<callout>\n\t" + summary + "\n</callout>\n")
	row := func(cells ...string) {
		body.WriteString("<tr>\n")
		for _, cell := range cells {
			body.WriteString("<td>" + cell + "</td>\n")
		}
		body.WriteString("</tr>\n")
	}
	body.WriteString("<table>\n")
	row(text["file"], text["status"], text["page"], text["message"])
	number := 0
	for _, item := range items {
		if item.Kind == "folder" {
			continue
		}
		state := text["failed"]
		switch item.State {
		case "imported":
			state = text["imported"]
		case "skipped":
			state = text["skipped"]
		case "cancelled":
			state = text["cancelled"]
		}
		page, message := "", item.Message
		if item.State == "imported" && item.PageID != "" {
			page = fmt.Sprintf(`<mention-page url="%s"/>`, item.PageID)
		}
		if item.State == "imported" && strings.TrimSpace(message) == "" {
			message = text["importedMessage"]
		}
		number++
		row(fmt.Sprintf("%d. %s", number, workspaceImportBatchLogEscape(item.Name)), state, page, workspaceImportBatchLogEscape(message))
	}
	body.WriteString("</table>\n")
}
