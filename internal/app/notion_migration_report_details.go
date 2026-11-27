package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The report says what moved and names what didn't, each by the page it was
// on, as the import keeps it: a count alone leaves the person nowhere to
// look. Many of one kind are one line with the ones it is folded under it
// (notion_migration_report_groups.go).

// notionMigrationReportIssues bounds what the page lists; the import keeps all.
const notionMigrationReportIssues = 200

// notionMigrationReportDetails is what the report reads of a run's job.
type notionMigrationReportDetails struct {
	Views       int
	LinkedViews int
	// What the run read, as Notion counts it, and what a run again made new,
	// changed and left as it was (notion_migration_report_counts.go).
	Counts notionMigrationRunCounts
	Tally  *notionMigrationRunTally
	// The imported forms that came without questions, by why
	// (notion_migration_report_forms.go).
	Forms notionMigrationReportForms
	// Each item of what didn't come whole with the first pages it was on, as
	// many as a group's toggle lists, and how many in all
	// (notion_import_issue_places.go).
	Issues []storedNotionImportBlockIssue
	Files  []storedNotionFileOutcome
	// How many files it still couldn't copy, by why: all of them, the list's
	// and the files line's one count (notion_migration_report_files.go).
	FileCounts map[string]int
	// Listed holds the reasons the list by page names, which the report's
	// first lines leave to it.
	Listed map[string]bool
	// What Notion's API doesn't give that the run couldn't match, all of it
	// (notion_migration_formats_left.go): the report says it by kind.
	FormatsLeft []leafenMCPNotionFormatLeft
	// What the run's check against Notion found, in the person's language
	// (notion_migration_verify_step.go).
	Verify         *notionMigrationVerify
	VerifyLanguage string
	// The resolved discussions Notion's API didn't give, as a reading of
	// the person's Notion counted them (notion_migration_comment_discussions.go).
	Resolved notionMigrationResolvedLeft
}

func loadNotionMigrationReportDetails(app core.App, jobID, migrationID string) (notionMigrationReportDetails, error) {
	details, err := notionMigrationReportDetails{}, error(nil)
	if details.FormatsLeft, err = notionMigrationFormatsLeftAll(app, jobID); err != nil {
		return details, err
	}
	if details.Verify, details.VerifyLanguage, err = loadNotionMigrationVerify(app, migrationID); err != nil {
		return details, err
	}
	if details.Counts, err = loadNotionMigrationRunCounts(app, jobID, migrationID); err != nil {
		return details, err
	}
	if details.Tally, err = loadNotionMigrationRunTally(app, migrationID); err != nil {
		return details, err
	}
	if details.Resolved, err = loadNotionMigrationResolvedLeft(app, jobID, migrationID); err != nil {
		return details, err
	}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_sources}} AS [[view]]
		JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]]=[[view]].[[job_id]]
			AND [[mapping]].[[source_id]]=[[view]].[[source_id]] AND [[mapping]].[[kind]]='view'
		WHERE [[view]].[[job_id]]={:jobId} AND [[view]].[[kind]]='view'
	`).Bind(dbx.Params{"jobId": jobID}).Row(&details.Views); err != nil {
		return details, err
	}
	if details.Forms, err = loadNotionMigrationReportForms(app, jobID); err != nil {
		return details, err
	}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='block' AND instr([[text]], '"linkedDatabase":true')>0
			AND [[source_id]] IN (SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
				WHERE [[job_id]]={:jobId} AND [[kind]]='block')
	`).Bind(dbx.Params{"jobId": jobID}).Row(&details.LinkedViews); err != nil {
		return details, err
	}
	// A template an earlier run made and this one didn't read again, as
	// Notion still gives it and left it be, keeps its body's linked databases:
	// the report counts them with the migration's (notion_import_counted.go).
	kept := struct {
		Views  int `db:"views"`
		Linked int `db:"linked"`
	}{}
	if err := app.DB().NewQuery(`
		WITH [[kept]] AS (
			SELECT [[native_id]] FROM {{leafen_notion_migration_ledger}} WHERE ` + notionImportKeptTemplatesWhereSQL + `
		)
		SELECT
			(SELECT COUNT(*) FROM {{leafen_database_views}} WHERE [[page_id]] IN (SELECT [[native_id]] FROM [[kept]])) AS [[views]],
			(SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]] IN (SELECT [[native_id]] FROM [[kept]])
				AND [[type]]={:inline} AND json_extract([[content]], '$.linkedDatabaseSource')=1) AS [[linked]]
	`).Bind(notionImportCountedParams(dbx.Params{
		"jobId": jobID, "migrationId": migrationID, "inline": graphBlockTypeInlineDatabase,
	})).One(&kept); err != nil {
		return details, err
	}
	details.Views += kept.Views
	details.LinkedViews += kept.Linked
	// What the migration still hasn't fully moved, its earlier runs' too
	// (notion_migration_issues.go), by item with the first pages of each; one
	// not settled yet reads its job's. Every item with a page is read, so the
	// list names each why it holds.
	settled, err := notionMigrationIssuesSettled(app, migrationID)
	if err != nil {
		return details, err
	}
	if details.Issues, err = loadNotionImportIssuePlaces(app, jobID, migrationID, notionMigrationReportGroupShown); err != nil {
		return details, err
	}
	details.Listed = map[string]bool{}
	for _, issue := range details.Issues {
		details.Listed[issue.Reason] = true
	}
	// The files the migration still couldn't copy, its earlier runs' too.
	details.Files, details.FileCounts, err = loadNotionMigrationReportFiles(app, jobID, migrationID, settled)
	return details, err
}

// notionMigrationReportPlace names where something was: a mention of the page
// it was on, or its title when no page came of it.
func notionMigrationReportPlace(pageID, title string, fallback string) string {
	if pageID != "" {
		return fmt.Sprintf(`<mention-page url="%s"/>`, pageID)
	}
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	return fallback
}

// notionMigrationReportBlockReasons are the whys the import gives a block,
// whose subject is its type; a property's or view's subject is its name.
var notionMigrationReportBlockReasons = map[string]bool{
	notionImportIssueUnsupportedBlock: true, notionImportIssueInvalidBlock: true, notionImportIssueSettingOmitted: true,
	notionImportIssueTextLinkOmitted: true, notionImportIssueTitleShortened: true, notionImportIssueTextReferenceUnresolved: true,
	notionImportIssueLinkUnresolved: true, notionImportIssueLinkOutside: true, notionImportIssueTextReferenceOutside: true,
	notionImportIssueSyncedUnresolved: true, notionImportIssueNestedPageMoved: true,
}

// notionMigrationReportSubject names what an issue was about as the person
// knows it: a block by its kind's name in the block menu's words — one Notion
// gives as "unsupported", or of a kind the report has no name for, as an
// unknown kind, never its key (2026-11-20: "메모 1 · unsupported (1)") — and a
// property or view by its own name.
func notionMigrationReportSubject(text map[string]string, issue storedNotionImportBlockIssue) string {
	if !notionMigrationReportBlockReasons[issue.Reason] {
		return issue.BlockType
	}
	if name, ok := text["block:"+issue.BlockType]; ok {
		return name
	}
	return text["block:unsupported"]
}

func notionMigrationReasonLabel(text map[string]string, reason string) string {
	if label, ok := text["reason:"+reason]; ok {
		return label
	}
	return reason
}

// notionMigrationReportReasons are the report's words for why something didn't
// come whole; they join its text in the person's language.
var notionMigrationReportReasons = map[string]map[string]string{
	accountLanguageKo: {
		"block:paragraph":                                   "텍스트",
		"block:heading_1":                                   "제목1",
		"block:heading_2":                                   "제목2",
		"block:heading_3":                                   "제목3",
		"block:heading_4":                                   "제목4",
		"block:bulleted_list_item":                          "글머리 기호 목록",
		"block:numbered_list_item":                          "번호 매기기 목록",
		"block:to_do":                                       "할 일 목록",
		"block:toggle":                                      "토글 목록",
		"block:quote":                                       "인용",
		"block:callout":                                     "콜아웃",
		"block:code":                                        "코드",
		"block:equation":                                    "블록 수학 공식",
		"block:divider":                                     "구분선",
		"block:breadcrumb":                                  "이동 경로",
		"block:table_of_contents":                           "목차",
		"block:column_list":                                 "열",
		"block:column":                                      "열",
		"block:embed":                                       "임베드",
		"block:tab":                                         "탭",
		"block:table":                                       "표",
		"block:table_row":                                   "표 행",
		"block:bookmark":                                    "북마크",
		"block:link_preview":                                "링크 미리보기",
		"block:file":                                        "파일",
		"block:pdf":                                         "PDF",
		"block:image":                                       "이미지",
		"block:video":                                       "동영상",
		"block:audio":                                       "오디오",
		"block:child_page":                                  "페이지",
		"block:child_database":                              "데이터베이스",
		"block:synced_block":                                "동기화 블록",
		"block:link_to_page":                                "페이지 링크",
		"block:template":                                    "템플릿 버튼",
		"block:page":                                        "페이지",
		"block:toggle_heading_1":                            "토글 제목1",
		"block:toggle_heading_2":                            "토글 제목2",
		"block:toggle_heading_3":                            "토글 제목3",
		"block:toggle_heading_4":                            "토글 제목4",
		"block:button":                                      "버튼",
		"block:inline_database":                             "데이터베이스 - 인라인",
		"block:database":                                    "데이터베이스",
		"block:row":                                         "데이터베이스 행",
		"block:property":                                    "데이터베이스 속성",
		"block:unsupported":                                 "알 수 없는 종류",
		"fileNotCopiedGroup":                                "복사하지 못한 파일 %d개",
		"fileUnsafeGroup":                                   "리펜이 보안상 받지 않는 형식이라 옮기지 않은 파일 %d개",
		"fileNotCopied":                                     "복사하지 못했습니다",
		"fileUnsafe":                                        "리펜이 보안상 받지 않는 파일 형식입니다",
		"fileSkippedGroup":                                  "계획대로 Notion에 둔 파일 %d개",
		"fileSkipped":                                       "계획대로 Notion에 두었습니다",
		"reason:unsupported_block_type":                     "리펜에 없는 블록이라 가져오지 않았습니다",
		"reason:block_content_invalid":                      "읽거나 담을 수 없어 가져오지 않았습니다",
		"reason:block_setting_omitted":                      "아이콘·색 같은 설정 일부 없이 가져왔습니다",
		"reason:title_shortened":                            "제목이 너무 길어 뒤를 잘랐습니다",
		"reason:text_link_omitted":                          "글은 두고, 리펜이 열 수 없는 링크만 뺐습니다",
		"reason:text_reference_unresolved":                  "글은 두고, 이번에 오지 않은 페이지로 가는 링크·멘션만 뺐습니다",
		"reason:link_target_unresolved":                     "가리키는 페이지가 이번에 오지 않아 페이지 링크를 뺐습니다",
		"reason:link_target_outside":                        "가리키는 페이지가 이번에 오지 않아 Notion의 그 페이지로 가는 링크로 남겼습니다",
		"reason:text_reference_outside":                     "글은 두고, 이번에 오지 않은 페이지로 가는 멘션·링크를 Notion의 그 페이지로 가는 링크로 남겼습니다",
		"reason:synced_block_source_unresolved":             "원본이 이번에 오지 않아 동기화 사본을 뺐습니다",
		"reason:nested_page_moved":                          "블록 안에 있던 하위 페이지를 페이지 끝으로 옮겼습니다",
		"reason:property_type_unsupported":                  "리펜에 없는 형식이라 속성을 옮기지 않았습니다",
		"reason:property_setting_omitted":                   "설정 일부 없이 속성을 옮겼습니다",
		"reason:property_value_formatting_omitted":          "일부 값은 서식이나 멘션 없이 글만 옮겼습니다",
		"reason:property_target_unresolved":                 "가리키는 데이터베이스나 속성이 이번에 오지 않았습니다",
		"reason:relation_value_unresolved":                  "이번에 오지 않은 페이지와의 관계를 뺐습니다",
		"reason:property_value_truncated":                   "Notion이 앞 25개만 주어 나머지 관계는 옮기지 못했습니다",
		"reason:formula_reference_unresolved":               "수식이 옮겨지지 않은 속성을 가리켜 오류가 날 수 있습니다",
		"reason:formula_function_unsupported":               "수식이 리펜에 없는 함수를 써 오류가 날 수 있습니다",
		"reason:property_value_recomputed":                  "멤버와 이어지지 않은 Notion 사람은 가져온 사람으로 보입니다",
		"reason:property_value_omitted":                     "일부 값을 읽거나 복사하지 못했습니다",
		"reason:property_title_shortened":                   "제목이 너무 긴 행은 뒤를 잘랐습니다",
		"reason:database_views_unavailable":                 "Notion이 보기를 주지 않았습니다",
		"reason:database_source_unavailable":                "Notion이 데이터 소스를 주지 않았습니다",
		"reason:database_unavailable":                       "Notion이 주지 않았거나 원본이 이번에 오지 않은 데이터베이스입니다",
		"reason:linked_source_outside":                      "이번에 가져오지 않은 데이터베이스를 보여 줍니다 — 그 데이터베이스도 들어가게 가져오면 함께 옵니다",
		"reason:linked_source_outside_named":                "이번에 가져오지 않은 “%s” 데이터베이스를 보여 줍니다 — 그 데이터베이스도 들어가게 가져오면 함께 옵니다",
		"reason:linked_source_unavailable":                  "보여 주는 원본 데이터베이스를 Notion이 이 연결에 주지 않았습니다",
		"reason:view_type_unsupported":                      "리펜에 없는 보기 형식입니다",
		"reason:object_name_ambiguous":                      "같은 이름의 행이 여럿이라 관계로 잇지 못했습니다",
		"reason:view_setting_omitted":                       "보기 설정 일부를 옮기지 못했습니다",
		"reason:view_property_unresolved":                   "보기가 가리키는 속성이 옮겨지지 않았습니다",
		"reason:stale_hidden_view_property_setting_ignored": "이제 없는 속성의 숨김 설정을 뺐습니다",
		"reason:view_row_order_unavailable":                 "Notion이 직접 정한 행 순서를 주지 않습니다",
	},
	accountLanguageEn: {
		"block:paragraph":                                   "Text",
		"block:heading_1":                                   "Heading 1",
		"block:heading_2":                                   "Heading 2",
		"block:heading_3":                                   "Heading 3",
		"block:heading_4":                                   "Heading 4",
		"block:bulleted_list_item":                          "Bulleted list",
		"block:numbered_list_item":                          "Numbered list",
		"block:to_do":                                       "To-do list",
		"block:toggle":                                      "Toggle list",
		"block:quote":                                       "Quote",
		"block:callout":                                     "Callout",
		"block:code":                                        "Code",
		"block:equation":                                    "Block equation",
		"block:divider":                                     "Divider",
		"block:breadcrumb":                                  "Breadcrumb",
		"block:table_of_contents":                           "Table of contents",
		"block:column_list":                                 "Columns",
		"block:column":                                      "Column",
		"block:embed":                                       "Embed",
		"block:tab":                                         "Tab",
		"block:table":                                       "Table",
		"block:table_row":                                   "Table row",
		"block:bookmark":                                    "Bookmark",
		"block:link_preview":                                "Link preview",
		"block:file":                                        "File",
		"block:pdf":                                         "PDF",
		"block:image":                                       "Image",
		"block:video":                                       "Video",
		"block:audio":                                       "Audio",
		"block:child_page":                                  "Page",
		"block:child_database":                              "Database",
		"block:synced_block":                                "Synced block",
		"block:link_to_page":                                "Page link",
		"block:template":                                    "Template button",
		"block:page":                                        "Page",
		"block:toggle_heading_1":                            "Toggle heading 1",
		"block:toggle_heading_2":                            "Toggle heading 2",
		"block:toggle_heading_3":                            "Toggle heading 3",
		"block:toggle_heading_4":                            "Toggle heading 4",
		"block:button":                                      "Button",
		"block:inline_database":                             "Database - Inline",
		"block:database":                                    "Database",
		"block:row":                                         "Database row",
		"block:property":                                    "Database property",
		"block:unsupported":                                 "Unknown kind",
		"fileNotCopiedGroup":                                "%d files that couldn’t be copied",
		"fileUnsafeGroup":                                   "%d files of a type Leafen doesn’t take, for security",
		"fileNotCopied":                                     "couldn’t be copied",
		"fileUnsafe":                                        "a file type Leafen doesn’t take, for security",
		"fileSkippedGroup":                                  "%d files left in Notion, as the plan chose",
		"fileSkipped":                                       "left in Notion, as the plan chose",
		"reason:unsupported_block_type":                     "a block type Leafen doesn’t have, left out",
		"reason:block_content_invalid":                      "couldn’t be read or kept, left out",
		"reason:block_setting_omitted":                      "came without a setting such as its icon or color",
		"reason:title_shortened":                            "its title was too long, so the end was cut",
		"reason:text_link_omitted":                          "kept its words, lost a link Leafen can’t open",
		"reason:text_reference_unresolved":                  "kept its words, lost a link or mention to a page that didn’t come",
		"reason:link_target_unresolved":                     "a page link whose page didn’t come, left out",
		"reason:link_target_outside":                        "a page link whose page didn’t come, kept as a link to it in Notion",
		"reason:text_reference_outside":                     "kept its words, a mention or link to a page that didn’t come kept as a link to it in Notion",
		"reason:synced_block_source_unresolved":             "a synced copy whose original didn’t come, left out",
		"reason:nested_page_moved":                          "a sub-page inside a block, moved to the end of the page",
		"reason:property_type_unsupported":                  "a property type Leafen doesn’t have, left out",
		"reason:property_setting_omitted":                   "came without some of its settings",
		"reason:property_value_formatting_omitted":          "some values came as text alone, without formatting or mentions",
		"reason:property_target_unresolved":                 "what it points to didn’t come",
		"reason:relation_value_unresolved":                  "lost relations to pages that didn’t come",
		"reason:property_value_truncated":                   "Notion gave only the first 25 relations",
		"reason:formula_reference_unresolved":               "a formula naming a property that didn’t come; it may show an error",
		"reason:formula_function_unsupported":               "a formula with a function Leafen doesn’t have; it may show an error",
		"reason:property_value_recomputed":                  "Notion people matched to no member show as the person importing",
		"reason:property_value_omitted":                     "some values couldn’t be read or copied",
		"reason:property_title_shortened":                   "rows with too long a title had the end cut",
		"reason:database_views_unavailable":                 "Notion didn’t give its views",
		"reason:database_source_unavailable":                "Notion didn’t give the data source",
		"reason:database_unavailable":                       "a database Notion didn’t give, or whose source didn’t come",
		"reason:linked_source_outside":                      "shows a database this import didn’t take — import with that database included and it comes along",
		"reason:linked_source_outside_named":                "shows “%s”, a database this import didn’t take — import with that database included and it comes along",
		"reason:linked_source_unavailable":                  "Notion didn’t give this connection the database it shows",
		"reason:view_type_unsupported":                      "a view layout Leafen doesn’t have",
		"reason:object_name_ambiguous":                      "several rows share the name, so it wasn’t linked",
		"reason:view_setting_omitted":                       "some view settings didn’t come",
		"reason:view_property_unresolved":                   "a view naming a property that didn’t come",
		"reason:stale_hidden_view_property_setting_ignored": "left out a hidden setting of a property that’s gone",
		"reason:view_row_order_unavailable":                 "Notion doesn’t give an order set by hand",
	},
}

func init() {
	for language, words := range notionMigrationReportReasons {
		for key, value := range words {
			notionMigrationReportText[language][key] = value
		}
	}
}

// notionMigrationTypeNames names a property type in the report's language,
// as the property menu does, not by its key.
var notionMigrationTypeNames = map[string]map[string]string{
	accountLanguageKo: {
		"title": "제목", "rich_text": "텍스트", "number": "숫자", "select": "선택", "multi_select": "다중 선택", "status": "상태",
		"date": "날짜", "person": "사람", "files": "파일과 미디어", "checkbox": "체크박스", "url": "URL", "email": "이메일",
		"phone_number": "전화번호", "formula": "수식", "relation": "관계형", "rollup": "롤업", "created_time": "생성 일시",
		"created_by": "생성자", "last_edited_time": "최종 편집 일시", "last_edited_by": "최종 편집자", "unique_id": "ID",
		"button": "버튼", "place": "장소", "verification": "인증",
	},
	accountLanguageEn: {
		"title": "Title", "rich_text": "Text", "number": "Number", "select": "Select", "multi_select": "Multi-select",
		"status": "Status", "date": "Date", "person": "Person", "files": "Files & media", "checkbox": "Checkbox", "url": "URL",
		"email": "Email", "phone_number": "Phone", "formula": "Formula", "relation": "Relation", "rollup": "Rollup",
		"created_time": "Created time", "created_by": "Created by", "last_edited_time": "Last edited time",
		"last_edited_by": "Last edited by", "unique_id": "ID", "button": "Button", "place": "Place", "verification": "Verification",
	},
}

func notionMigrationTypeName(language, key string) string {
	names := notionMigrationTypeNames[language]
	if names == nil {
		names = notionMigrationTypeNames[accountLanguageEn]
	}
	return cmpOr(names[key], key)
}
