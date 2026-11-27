package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A migration's report page (decision "설계 우선 Notion 이사" 2절 5단계,
// 4절): the run's last step writes, under the import's top page, what didn't
// move and why first, with the check against Notion, then what moved against
// what the scan counted, the structure the plan changed, the values preserved
// or kept only in the report, and the views to rebuild — in the person's
// language, many of one kind said once (notion_migration_report_groups.go). A run again rewrites the
// same page, keeping what the person changed there
// (notion_migration_report_merge.go). Rebuilt views say so in their own
// description.

// notionMigrationReportValues bounds the reported values the page lists for
// a property; the run's report keeps them all.
const notionMigrationReportValues = 100

var notionMigrationReportText = map[string]map[string]string{
	accountLanguageKo: {
		"title":                              "가져오기 보고서",
		"lead":                               "Notion에서 옮긴 결과입니다 (%s, 계획 버전 %d).",
		"leadTool":                           "%s에서 옮긴 결과입니다 (%s, 계획 버전 %d).",
		"toolLoss":                           "%s %d개: %s",
		"moved":                              "옮긴 것",
		"scanLine":                           "스캔에서 센 Notion 원본: 페이지 %d · 데이터베이스 %d · 행 %d",
		"filesLine":                          "파일: 복사 %d · Notion에 둠 %d",
		"filesLineTool":                      "파일: 복사 %d",
		"removedLine":                        "원본에서 삭제한 블록: 삭제 %d · 남김 %d (리펜에서 편집했거나 안에 리펜에서 추가한 것이 있어서). 삭제한 블록은 그 페이지의 버전 기록에서 복원할 수 있습니다.",
		"placedLine":                         "원본에서 위치를 옮긴 블록: 옮김 %d · 그대로 %d (리펜에서 옮겼거나 편집할 수 없어서).",
		"notGiven":                           "원본이 이번에 주지 않은 페이지·행·데이터베이스 템플릿",
		"notGivenLead":                       "원본에서 지웠거나 다른 곳으로 옮겼거나, 연결이 볼 수 없게 됐거나, 계획에서 제외한 것입니다. 리펜에는 그대로 두었으니 필요 없으면 휴지통으로 옮기세요.",
		"structure":                          "바꾼 구조",
		"skippedPages":                       "건너뛴 페이지 %d개 (그 아래 포함)",
		"skippedDbs":                         "건너뛴 데이터베이스 %d개",
		"renamed":                            "%s → %s",
		"merged":                             "합침: %s → %s",
		"movedItems":                         "다른 위치로 옮긴 페이지·데이터베이스 %d개",
		"property":                           "%s · %s → %s",
		"preserved":                          "보존한 값",
		"preservedRow":                       "%s · %s → 텍스트 속성 %s",
		"reported":                           "옮길 곳이 없어 따로 둔 속성",
		"more":                               "외 %d개",
		"keptDeleted":                        "값 %d개는 모두 데이터베이스의 \"삭제된 속성\"에 남아 있어, 복원하면 다시 보입니다.",
		"keptProperty":                       "보기가 이 속성으로 묶거나 날짜를 정해 그대로 두었습니다(값 %d개).",
		"keptFormula":                        "계산식은 데이터베이스의 \"삭제된 속성\"에 남아 있어, 복원하면 다시 계산됩니다.",
		"computed":                           "계산식: %s",
		"computedEmpty":                      "계산식이 비어 있었습니다.",
		"notMoved":                           "옮기지 못한 것",
		"importIssues":                       "온전히 가져오지 못한 것: 블록 %d · 속성 %d · 보기 %d (어느 페이지인지는 까닭마다 접어 두었습니다)",
		"importerAuthored":                   "페이지·행 %d개는 만든 사람·마지막으로 편집한 사람이 멤버와 이어지지 않은 Notion 사람(통합 포함)입니다. 계정으로는 이 마이그레이션을 실행한 사람이 남고, 그 자리에는 Notion 이름(통합은 연결로)이 보입니다. 편집한 시각은 Notion 그대로입니다. 사람 속성과 멘션의 Notion 사람은 " + notionMigrationPeoplePlaceLink(accountLanguageKo) + "에서 멤버와 이을 수 있습니다.",
		"importerAuthoredTool":               "페이지·행 %d개는 만든 사람·마지막으로 편집한 사람이 이 마이그레이션을 실행한 사람으로 보입니다.",
		"comments":                           "댓글: Notion이 이 연결에 댓글을 주지 않아 옮기지 못했습니다.",
		"commentsDone":                       "댓글 %d개를 옮겼습니다 (작성자·시각은 각 댓글 첫 줄에).",
		"commentsUnreadable":                 "댓글: Notion이 페이지·블록 %d개의 댓글을 주지 않아 그 댓글은 옮기지 못했습니다. Notion에서 확인하세요.",
		"changedMeanwhile":                   "실행하는 동안 휴지통으로 옮기거나 지운 것 %d개(페이지·행·데이터베이스와 그 댓글·템플릿)는 옮겨 둔 그대로 두고 건너뛰었습니다. 복원했다면 다시 실행하면 이어서 옮깁니다.",
		"commentsPageOnly":                   "댓글: 블록에 단 댓글은 그 블록에 붙지만, Notion이 댓글이 가리키던 글 부분을 주지 않아 블록 전체에 붙습니다.",
		"viewsStay":                          "보기: 다시 가져올 때는 처음 옮긴 보기를 그대로 둡니다. Notion에서 바꾼 보기는 따라오지 않으니 필요하면 리펜에서 편집하세요.",
		"people":                             "사람 매핑을 끝내지 못했습니다(%s). " + notionMigrationPeoplePlaceLink(accountLanguageKo) + "에서 다시 이으세요.",
		"peopleDone":                         "Notion 사람 %d명을 워크스페이스 멤버로 매핑했습니다.",
		"peopleSkipped":                      "볼 수 없는 페이지 %d개에 있는 Notion 사람은 그대로 두었습니다.",
		"views":                              "다시 만들 보기",
		"viewUnknown":                        "%s: Notion이 보기를 주지 않아 표 보기 하나로 들어왔습니다",
		"viewUnknownGroup":                   "Notion이 보기를 주지 않아 표 보기 하나로 들어온 데이터베이스 %d개",
		"viewNote":                           "다시 만든 보기의 설명에 “Notion 가져오기 때 다시 만든 보기”라고 적어 둡니다.",
		"again":                              "같은 계획을 다시 실행하면 이 보고서를 업데이트합니다.",
		"againNotion":                        "Notion을 계속 쓴다면 같은 가져오기를 다시 실행하세요(완료됨의 ‘바뀐 것 다시 가져오기’). 새로 생기거나 바뀐 것만 가져오고 이 보고서를 업데이트합니다. 다 옮겼다면 Notion을 읽기 전용으로 두거나 그만 쓰세요. 리펜에서 쓴 것은 Notion으로 돌아가지 않습니다.",
		"untitled":                           "새 데이터베이스",
		"untitledPage":                       "새 페이지",
		"issue:property_type_unsupported":    "리펜에 없는 형식의 속성 %d개 (값은 이 보고서에)",
		"issue:formula_function_unsupported": "리펜이 계산하지 못하는 수식 %d개",
		"issue:formula_reference_unresolved": "없는 속성을 가리키는 수식 %d개",
		"issue:database_views_unavailable":   "Notion이 보기를 주지 않는 데이터베이스 %d개",
		"issue:database_unavailable":         "연결이 읽지 못한 데이터베이스 %d개",
		"issue:page_unavailable":             "연결이 읽지 못한 페이지 %d개",
		"issue:blocks_unavailable":           "Notion이 안쪽 내용을 주지 않은 페이지·블록 %d개",
		"issue:unsupported_block_type":       "가져오지 않는 블록이 있는 페이지 %d개",
	},
	accountLanguageEn: {
		"title":                              "Import report",
		"lead":                               "What moved from Notion (%s, plan revision %d).",
		"leadTool":                           "What moved from %s (%s, plan revision %d).",
		"toolLoss":                           "%s: %d — %s",
		"moved":                              "What moved",
		"scanLine":                           "In Notion, as the scan counted: pages %d · databases %d · rows %d",
		"filesLine":                          "Files: copied %d · left in Notion %d",
		"filesLineTool":                      "Files: copied %d",
		"removedLine":                        "Blocks deleted at the source: removed %d · kept %d (changed in Leafen, or holding something added in Leafen). A removed block comes back from its page’s version history.",
		"placedLine":                         "Blocks moved at the source: moved %d · left in place %d (moved in Leafen, or on a page you can’t change).",
		"notGiven":                           "Pages, rows and database templates the source didn’t give this run",
		"notGivenLead":                       "Deleted or moved at the source, out of the connection’s reach, or left out by the plan. They stay in Leafen; move any you don’t need to the trash.",
		"structure":                          "Structure changes",
		"skippedPages":                       "%d pages skipped (with what’s under them)",
		"skippedDbs":                         "%d databases skipped",
		"renamed":                            "%s → %s",
		"merged":                             "Merged: %s → %s",
		"movedItems":                         "%d pages and databases moved to another place",
		"property":                           "%s · %s → %s",
		"preserved":                          "Preserved values",
		"preservedRow":                       "%s · %s → text property %s",
		"reported":                           "Properties moved aside for having no place",
		"more":                               "and %d more",
		"keptDeleted":                        "All %d values stay in the database’s Deleted properties and come back on restore.",
		"keptProperty":                       "A view groups or dates by it, so it stays where it was (%d values).",
		"keptFormula":                        "The formula stays in the database’s Deleted properties and computes again on restore.",
		"computed":                           "Formula: %s",
		"computedEmpty":                      "The formula was empty.",
		"notMoved":                           "What didn’t move",
		"importIssues":                       "Not fully imported: blocks %d · properties %d · views %d (which pages, folded under each why)",
		"importerAuthored":                   "%d pages and rows were made or last changed by a Notion person matched to no member (an integration among them). The person who ran this migration stays as their account, and their Notion name (an integration’s as a connection) shows in its place. When they were last changed stays as in Notion. Notion people in person properties and mentions can be matched to members in " + notionMigrationPeoplePlaceLink(accountLanguageEn) + ".",
		"importerAuthoredTool":               "%d pages and rows show the person who ran this migration as who made and last changed them.",
		"comments":                           "Comments: Notion didn’t give this connection the comments, so they weren’t moved.",
		"commentsDone":                       "%d comments moved (each says who wrote it and when on its first line).",
		"commentsUnreadable":                 "Comments: Notion didn’t give the comments of %d pages or blocks, so those weren’t moved. Check them in Notion.",
		"changedMeanwhile":                   "%d things moved to the trash or deleted while the run went on (pages, rows, databases and their comments or templates) were left as they were and passed by. If you restored them, run it again to move them.",
		"commentsPageOnly":                   "Comments: a comment on a block comes on that block as a whole, as Notion doesn’t give the text it pointed at.",
		"viewsStay":                          "Views: a run again leaves the views it moved as they are. Views changed in Notion don’t follow, so change them in Leafen if you need to.",
		"people":                             "Mapping people didn’t finish (%s). Match them again in " + notionMigrationPeoplePlaceLink(accountLanguageEn) + ".",
		"peopleDone":                         "%d Notion people mapped to workspace members.",
		"peopleSkipped":                      "Notion people on pages you can’t open (%d) were left as they were.",
		"views":                              "Views to rebuild",
		"viewUnknown":                        "%s: Notion doesn’t give its views, so it came with one table view",
		"viewUnknownGroup":                   "%d databases whose views Notion doesn’t give: each came with one table view",
		"viewNote":                           "A rebuilt view says “Rebuilt in the Notion import” in its description.",
		"again":                              "Running the same plan again rewrites this report.",
		"againNotion":                        "If you keep writing in Notion, run the same import again (Completed › Import changes again): it brings only what’s new or changed and updates this report. Once everything has moved, make Notion read-only or stop using it. What you write in Leafen doesn’t go back to Notion.",
		"untitled":                           "New database",
		"untitledPage":                       "New page",
		"issue:property_type_unsupported":    "%d properties of a type Leafen doesn’t have (values in this report)",
		"issue:formula_function_unsupported": "%d formulas Leafen can’t calculate",
		"issue:formula_reference_unresolved": "%d formulas naming a property that isn’t there",
		"issue:database_views_unavailable":   "%d databases whose views Notion doesn’t give",
		"issue:database_unavailable":         "%d databases the connection can’t read",
		"issue:page_unavailable":             "%d pages the connection can’t read",
		"issue:blocks_unavailable":           "%d pages or blocks whose contents Notion didn’t give",
		"issue:unsupported_block_type":       "%d pages with blocks the import leaves out",
	},
}

// notionMigrationOrigin is the tool a migration's bundle came from and
// what the tool said it couldn't carry; none for Notion.
type notionMigrationOrigin struct {
	Tool   string
	Losses []workspaceBundleLoss
	// RunAgain is a run after the first, which brings only what changed.
	RunAgain bool
}

// notionMigrationReportForTool is the report's words for a move from another
// tool: they name that tool, and it has no people to match to members.
func notionMigrationReportForTool(text map[string]string, tool string) map[string]string {
	named := make(map[string]string, len(text))
	for key, value := range text {
		named[key] = strings.ReplaceAll(strings.ReplaceAll(value, "Notion이 ", tool+" 내보내기가 "), "Notion", tool)
	}
	named["importerAuthored"] = text["importerAuthoredTool"]
	return named
}

// renderNotionMigrationReport writes the report's Markdown.
func renderNotionMigrationReport(
	language string, revision int, plan notionMigrationPlan, summary notionMigrationSummary,
	report notionMigrationRunReport, job notionImportJob, origin notionMigrationOrigin, now time.Time,
	notGiven []notionMigrationNotGiven, importerAuthored int, details notionMigrationReportDetails,
) string {
	text := notionMigrationReportText[language]
	if text == nil {
		text = notionMigrationReportText[accountLanguageEn]
	}
	if origin.Tool != "" {
		text = notionMigrationReportForTool(text, origin.Tool)
	}
	// A database with no title is called what Notion calls one, and so is a
	// row (untitledPage).
	name := func(title string) string {
		if title = strings.TrimSpace(title); title == "" {
			return text["untitled"]
		}
		return title
	}
	var out strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }
	// A run that stopped says so first (notion_migration_failure.go).
	if stop := report.Failure; stop != nil && job.Status == durableJobStatusFailed {
		line(`<callout icon="⚠️" color="red_bg">%s</callout>`, notionMigrationFailureCallout(language, *stop))
		line("")
	}
	if origin.Tool != "" {
		line(text["leadTool"], origin.Tool, now.Format("2006-01-02"), revision)
	} else {
		line(text["lead"], now.Format("2006-01-02"), revision)
	}
	// What didn't move comes first, then the check against Notion
	// (notion_migration_report_not_moved.go).
	line("")
	renderNotionMigrationReportNotMoved(line, language, text, plan, summary, report, job, origin, importerAuthored, details)
	renderNotionMigrationVerify(details.Verify, details.VerifyLanguage, text, line)
	line("")
	line("## %s", text["moved"])
	for _, counted := range notionMigrationReportCountLines(language, text, summary, job, details, origin) {
		line("- %s", counted)
	}
	if report.BlocksMoved > 0 || report.BlocksMoveKept > 0 {
		line("- "+text["placedLine"], report.BlocksMoved, report.BlocksMoveKept)
	}
	if report.BlocksRemoved > 0 || report.BlocksKept > 0 {
		line("- "+text["removedLine"], report.BlocksRemoved, report.BlocksKept)
	}
	if len(notGiven) > 0 {
		line("")
		line("## %s", text["notGiven"])
		line("%s", text["notGivenLead"])
		writeNotionMigrationReportGroups(line, language, 0, notionMigrationNotGivenGroups(language, notGiven))
	}

	databases, properties, plannedDatabases := notionMigrationReportSources(summary, plan)
	structure, preserved := []string{}, []string{}
	skippedPages, skippedDatabases, moved := 0, 0, 0
	for _, page := range plan.Pages {
		if page.Action == "skip" {
			skippedPages++
		}
		if strings.TrimSpace(page.ParentSourceID) != "" {
			moved++
		}
	}
	for _, planned := range plannedDatabases {
		key := notionMigrationSourceKey(planned.SourceID)
		source := databases[key]
		switch planned.Action {
		case "skip":
			skippedDatabases++
			continue
		case "merge":
			structure = append(structure, fmt.Sprintf(text["merged"], name(source.Title), name(databases[notionMigrationSourceKey(planned.Into)].Title)))
		}
		if title := strings.TrimSpace(planned.Title); title != "" && title != source.Title {
			structure = append(structure, fmt.Sprintf(text["renamed"], name(source.Title), title))
		}
		if strings.TrimSpace(planned.ParentSourceID) != "" {
			moved++
		}
		for _, entry := range planned.Properties {
			property := properties[key+"/"+notionMigrationPropertyKey(entry.SourceID)]
			switch entry.Action {
			case "preserve":
				preserved = append(preserved, fmt.Sprintf(text["preservedRow"], name(source.Title), property.Name, cmpOr(entry.Name, property.Name)))
			case "map":
				target := cmpOr(strings.TrimSpace(entry.Name), property.Name)
				kind := cmpOr(strings.TrimSpace(entry.Type), property.LeafenType)
				if target != property.Name || kind != property.LeafenType {
					structure = append(structure, fmt.Sprintf(text["property"], name(source.Title),
						property.Name+" ("+notionMigrationTypeName(language, property.LeafenType)+")", target+" ("+notionMigrationTypeName(language, kind)+")"))
				}
			}
		}
	}
	structure = append(structure, notionMigrationOptionMergeLines(language, plan, summary, name)...)
	structure = append(structure, notionMigrationComputedBesideLines(language, summary, name)...)
	if skippedPages > 0 {
		structure = append([]string{fmt.Sprintf(text["skippedPages"], skippedPages)}, structure...)
	}
	if skippedDatabases > 0 {
		structure = append([]string{fmt.Sprintf(text["skippedDbs"], skippedDatabases)}, structure...)
	}
	if moved > 0 {
		structure = append(structure, fmt.Sprintf(text["movedItems"], moved))
	}
	if len(structure) > 0 {
		line("")
		line("## %s", text["structure"])
		for _, item := range structure {
			line("- %s", item)
		}
	}
	if len(preserved) > 0 {
		line("")
		line("## %s", text["preserved"])
		for _, item := range preserved {
			line("- %s", item)
		}
	}
	renderNotionMigrationReportReported(line, language, text, report, databases, name)
	viewsUnavailable := map[string]bool{}
	for _, issue := range summary.Issues {
		if issue.Code == notionImportIssueViewsUnavailable {
			viewsUnavailable[notionMigrationSourceKey(issue.SourceID)] = true
		}
	}
	views := newNotionMigrationReportGrouping()
	for _, planned := range plan.Databases {
		if key := notionMigrationSourceKey(planned.SourceID); planned.Action == "import" && viewsUnavailable[key] {
			views.add("unknown", name(databases[key].Title), 1)
		}
	}
	if len(views.order) > 0 {
		line("")
		line("## %s", text["views"])
		writeNotionMigrationReportGroups(line, language, 0, views.list(nil, func(group *notionMigrationReportGroup) {
			group.Say = fmt.Sprintf(text["viewUnknownGroup"], group.Count)
			if group.Total == 1 {
				group.One = fmt.Sprintf(text["viewUnknown"], group.Items[0])
			}
		}))
		line("")
		line("%s", text["viewNote"])
	}
	line("")
	// A move from Notion ends saying what comes after it: Notion written on
	// runs again, or left read-only (notionMigrationAfterNext).
	if origin.Tool == "" {
		line("%s", text["againNotion"])
	} else {
		line("%s", text["again"])
	}
	return out.String()
}

func cmpOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// writeNotionMigrationReport writes, or rewrites, the run's report page
// under the import's top page, made through app: the runner's or a request's.
func (r *durableJobRunner) writeNotionMigrationReport(ctx context.Context, app core.App, job storedJob, payload notionImportJobPayload) error {
	if r.ygo == nil {
		return errNotionMigrationRestructure
	}
	stored, err := loadNotionMigration(r.app, job.Owner, job.WorkspaceID, payload.MigrationID)
	if err != nil {
		return err
	}
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &roots); err != nil {
		return err
	}
	top, found := "", false
	for _, root := range roots {
		sourceID := root.ID
		if root.Kind == "database" {
			sourceID = notionDatabaseRootPageSourceID(root.ID)
		}
		mapped, err := notionImportMappedID(r.app, job.ID, sourceID, "page")
		if err != nil {
			return err
		}
		if mapped == "" {
			continue
		}
		top, found = mapped, true
		// A database's own page holds the database alone: the report goes
		// beside it (2026-11-06: every migration of a database root failed
		// at its end, its report refused under that page).
		if root.Kind == "database" {
			holder, err := r.app.FindRecordById(pagesCollection, mapped)
			if err != nil {
				return err
			}
			top = holder.GetString("parent_id")
		}
		break
	}
	if !found {
		return nil
	}
	// A report whose place a person moved to the trash meanwhile goes to the
	// top of their private pages (notion_import_changed_meanwhile.go).
	if top != "" {
		if away, err := notionImportPageTakenAway(r.app, top); err != nil {
			return err
		} else if away {
			top = ""
		}
	}
	plan, report := notionMigrationPlan{}, emptyNotionMigrationRunReport()
	if err := json.Unmarshal([]byte(stored.PlanJSON), &plan); err != nil {
		return err
	}
	summary, err := loadNotionMigrationSummary(r.app, stored)
	if err != nil {
		return err
	}
	// What Notion can't give is reported where the plan brings it, as the
	// plan's agenda lists it.
	check, err := checkNotionMigrationPlan(r.app, stored, summary, plan)
	if err != nil {
		return err
	}
	summary.Issues = check.Agenda
	if err := json.Unmarshal([]byte(stored.RunReport), &report); err != nil {
		return err
	}
	importJob, err := notionImportResponse(r.app, job.ID, job.Owner, job.WorkspaceID)
	if err != nil {
		return err
	}
	language, err := serverTextLanguage(r.app, job.Owner, nil)
	if err != nil {
		return err
	}
	origin := notionMigrationOrigin{}
	if source, err := loadBundleSource(r.app, job.Owner, job.WorkspaceID, stored.ConnectionID); err == nil {
		origin.Tool = source.Label
		_ = json.Unmarshal([]byte(source.Losses), &origin.Losses)
	} else if !errors.Is(err, errBundleSourceNotFound) {
		return err
	}
	origin.RunAgain = payload.MigrationRerunSince > 0
	notGiven := []notionMigrationNotGiven{}
	if payload.MigrationRerunSince > 0 {
		if notGiven, err = notionMigrationPagesNotGiven(r.app, job.ID, stored.ID); err != nil {
			return err
		}
	}
	importerAuthored, err := notionImportRowsAuthoredByImporter(r.app, job.ID, stored.ID, plan.People)
	if err != nil {
		return err
	}
	details, err := loadNotionMigrationReportDetails(r.app, job.ID, stored.ID)
	if err != nil {
		return err
	}
	cleanup, err := notionMigrationCleanupLines(r.app, job.ID, language, plan, summary, time.Now().In(personLocation(r.app, job.Owner)))
	if err != nil {
		return err
	}
	markdown := renderNotionMigrationReport(language, stored.PlanRevision, plan, summary, report, importJob, origin,
		time.Now().In(personLocation(r.app, job.Owner)), notGiven, importerAuthored, details) +
		renderNotionMigrationCleanup(language, cleanup)
	if markdown, err = notionMigrationReportTemplateMentions(r.app, language, markdown); err != nil {
		return err
	}
	pageID := report.ReportPageID
	if away, err := notionImportPageTakenAway(r.app, pageID); pageID != "" && (err != nil || away) {
		pageID = ""
	}
	made := pageID == ""
	if made {
		text := notionMigrationReportText[language]
		if text == nil {
			text = notionMigrationReportText[accountLanguageEn]
		}
		err := app.RunInTransaction(func(tx core.App) error {
			response, status, err := admitPageCreationTransactionWithLock(tx, job.Owner, normalizePageCreationAdmissionInput(pageCreationAdmissionInput{
				RequestID:   "notion-migration-report:" + stored.ID + ":" + job.ID,
				WorkspaceID: job.WorkspaceID, ParentID: top, Title: text["title"], Content: markdown, ContentInBody: true,
			}), pageLockIgnored)
			if err != nil {
				return err
			}
			if status != pageCreationApplied {
				return fmt.Errorf("%w: report page %v", errNotionMigrationRestructure, status)
			}
			pageID = response.Page.ID
			return updateNotionMigrationRunReport(tx, stored.ID, func(report *notionMigrationRunReport) {
				report.ReportPageID = pageID
			})
		})
		if err != nil {
			return err
		}
	}
	return r.writeNotionMigrationReportPage(ctx, job, stored.ID, pageID, made, markdown, report)
}
