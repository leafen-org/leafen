package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// The names the server gives what it makes for someone follow that person's
// language, as Notion names what it makes in the language of the person who
// makes it: a wiki's database, a database's sub-item and dependency
// properties, a sprint database and its board, a new status's options and
// the checkbox a to-do moved into a database keeps its check in. The server
// writes these names in the languages it writes its text in, by the one rule
// its texts follow (server_text_language.go): the chosen language, or for
// one following the system or not chosen the language their own request
// names first, else their last screen's, else the server's, else English —
// on an agent's path (a key's call, leafen_call's, an MCP tool's) and in work
// done later on its own too (2026-11-27: an agent's status read "Not
// started" while its questions to the same person were Korean), Korean where
// that language is Korean and English otherwise. The web sends a new wiki's
// names in its own language.

type generatedNames struct {
	Wiki                 wikiDatabaseNames
	ParentItem, SubItems string
	// A task database's sub-item pair, as Notion's names it (seen 2026-11-02).
	ParentTask, SubTasks string
	// A wiki's, as Notion's wiki names it (seen 2026-11-11).
	ParentPage, SubPages string
	BlockedBy, Blocking  string
	// The date a database's dependencies shift, made with their pair where
	// it has none, as Notion's (seen 2026-11-18).
	Date string
	// The checkbox a to-do moved into a database keeps its check in.
	Checked string
	Sprints generatedSprintNames
	// A new status's options and groups (newStatusWords).
	Status generatedStatusNames
}

type generatedStatusNames struct {
	NotStarted, InProgress, Done, ToDo, Complete string
}

type generatedSprintNames struct {
	Page, Board, Name, Status, Dates, Tasks, Sprint               string
	SprintID, TotalTasks, CompletedTasks                          string
	AllSprints, Timeline, CurrentSprint, Planning, Backlog        string
	Current, Next, Future, Last, Past, ToDo, InProgress, Complete string
	// Title names a sprint by its number.
	Title string
	// A sprint page's board and notes, and the section of its tasks.
	ByStatus, ByAssignee, PlanningNotes, Relations string
}

var generatedNamesByLanguage = map[string]generatedNames{
	accountLanguageEn: {
		Wiki: wikiDatabaseNames{
			Page: "Page", Owner: "Owner", Tags: "Tags", Verification: "Verification",
			LastEditedTime: "Last edited time", AllPages: "All pages", PagesIOwn: "Pages I own",
			ParentItem: "Parent item", SubItems: "Sub-items",
		},
		ParentItem: "Parent item", SubItems: "Sub-items", ParentTask: "Parent task", SubTasks: "Sub-tasks",
		ParentPage: "Parent page", SubPages: "Sub-pages",
		BlockedBy: "Blocked by", Blocking: "Blocking", Date: "Date",
		Checked: "Checked",
		Sprints: generatedSprintNames{
			Page: "Sprints", Board: "Sprint board", Name: "Sprint name", Status: "Sprint status",
			Dates: "Dates", Tasks: "Tasks", Sprint: "Sprint", AllSprints: "All sprints", Timeline: "Timeline",
			SprintID: "Sprint ID", TotalTasks: "Total tasks", CompletedTasks: "Completed tasks",
			CurrentSprint: "Current sprint", Planning: "Sprint planning", Backlog: "Backlog",
			Current: "Current", Next: "Next", Future: "Future", Last: "Last", Past: "Past",
			ToDo: "To-do", InProgress: "In progress", Complete: "Complete", Title: "Sprint %d",
			ByStatus: "By status", ByAssignee: "By assignee", PlanningNotes: "Planning notes", Relations: "Relations",
		},
		Status: generatedStatusNames{
			NotStarted: "Not started", InProgress: "In progress", Done: "Done", ToDo: "To-do", Complete: "Complete",
		},
	},
	accountLanguageKo: {
		Wiki: wikiDatabaseNames{
			Page: "페이지", Owner: "소유자", Tags: "태그", Verification: "인증",
			LastEditedTime: "최종 편집 일시", AllPages: "모든 페이지", PagesIOwn: "내가 소유한 페이지",
			ParentItem: "상위 항목", SubItems: "하위 항목",
		},
		ParentItem: "상위 항목", SubItems: "하위 항목", ParentTask: "상위 작업", SubTasks: "하위 작업",
		ParentPage: "상위 페이지", SubPages: "하위 페이지",
		BlockedBy: "선행 작업", Blocking: "후속 작업", Date: "날짜",
		Checked: "체크 표시됨",
		Sprints: generatedSprintNames{
			Page: "스프린트", Board: "스프린트 보드", Name: "스프린트 이름", Status: "스프린트 상태",
			Dates: "날짜", Tasks: "작업", Sprint: "스프린트", AllSprints: "모든 스프린트", Timeline: "타임라인",
			SprintID: "스프린트 ID", TotalTasks: "총 작업 수", CompletedTasks: "완료된 작업",
			CurrentSprint: "현재 스프린트", Planning: "스프린트 플래닝", Backlog: "백로그",
			Current: "현재", Next: "다음", Future: "이후", Last: "마지막", Past: "이전",
			ToDo: "할 일", InProgress: "진행 중", Complete: "완료", Title: "스프린트 %d",
			ByStatus: "상태별", ByAssignee: "담당자별", PlanningNotes: "플래닝 노트", Relations: "관계형",
		},
		Status: generatedStatusNames{
			NotStarted: "시작 전", InProgress: "진행 중", Done: "완료", ToDo: "할 일", Complete: "완료",
		},
	},
}

// generatedNamesIn are the names in a language the server writes text in.
func generatedNamesIn(language string) generatedNames {
	if names, ok := generatedNamesByLanguage[language]; ok {
		return names
	}
	return generatedNamesByLanguage[accountLanguageEn]
}

// generatedNamesFor are the names the server gives what it makes for the
// actor, in generatedNameLanguage.
func generatedNamesFor(ctx context.Context, app core.App, actorID string) generatedNames {
	return generatedNamesIn(generatedNameLanguage(ctx, app, actorID))
}

// generatedNameLanguage is the language the server names what it makes for
// the actor in, as serverTextLanguageIn says: the actor's account language,
// or, for an account that follows the system or hasn't chosen, the language
// the person's own request carried in ctx prefers, then the rule's later
// steps (server_text_language.go). A name made after a person — their
// organization and pair bot — follows it too.
func generatedNameLanguage(ctx context.Context, app core.App, actorID string) string {
	language, err := serverTextLanguageIn(ctx, app, actorID)
	if err != nil {
		return accountLanguageEn
	}
	return language
}
