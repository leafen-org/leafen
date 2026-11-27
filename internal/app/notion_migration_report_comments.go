package app

import (
	"fmt"
	"strings"
)

// notionMigrationReportCommentsStopped says in the report that the person
// stopped the comments (notion_migration_comments_stop.go).
var notionMigrationReportCommentsStopped = map[string]string{
	accountLanguageKo: "댓글: 옮기는 도중에 멈추셨습니다. 그때까지 옮긴 댓글은 남고, 나머지는 Notion에 있습니다. 다시 실행하면 댓글을 처음부터 다시 묻습니다.",
	accountLanguageEn: "Comments: stopped on the way, as asked. The comments moved until then stay; the rest are in Notion. A run again asks for comments again from the start.",
}

// notionMigrationReportCommentsNone says in the report that a run asked
// for comments and moved none — Notion gave none, or a run before moved them
// (they count once): Notion gives a connection only the discussions not
// resolved, so one resolved stays in Notion (2026-11-16: a
// real run asked 11,300 blocks and moved 0 while its top page had a
// discussion, and its report said nothing of comments' count; a resolved
// discussion on a synthetic page — Notion's own record of it resolved —
// came back empty from Notion's comments API, an open one beside it came).
var notionMigrationReportCommentsNone = map[string]string{
	accountLanguageKo: "댓글: 이번 실행에서 새로 옮긴 댓글은 0개입니다 — Notion이 이 연결에 준 해결되지 않은 댓글 가운데 아직 옮기지 않은 것이 없었습니다.",
	accountLanguageEn: "Comments: no new comments moved this run — of the unresolved comments Notion gave this connection, none were left to move.",
}

// notionMigrationReportResolved says in the report how many resolved
// discussions stayed in Notion — its API gives a connection only those not
// resolved — as a reading of the person's Notion counted them on what the
// run moved: some (perhaps more, where the reading couldn't read every
// page), none where it could read all (then nothing is said), or not counted.
var notionMigrationReportResolved = map[string]map[string]string{
	accountLanguageKo: {
		"some":     "해결된 토론 %d개는 Notion이 이 연결에 주지 않아 옮기지 않았으니 Notion에서 확인하세요.",
		"someMore": "해결된 토론 %d개는 Notion이 이 연결에 주지 않아 옮기지 않았습니다. 리펜이 Notion에서 읽지 못한 페이지에 더 있을 수 있으니 Notion에서 확인하세요.",
		"noneRead": "해결된 토론은 Notion이 이 연결에 주지 않습니다. 리펜이 Notion에서 읽은 페이지에는 없었지만 읽지 못한 페이지에 있을 수 있으니 Notion에서 확인하세요.",
		"unknown":  "해결된 토론은 Notion이 이 연결에 주지 않아 옮기지 않았습니다. 리펜이 Notion에서 읽지 않아 몇 개인지 모르니 Notion에서 확인하세요.",
	},
	accountLanguageEn: {
		"some":     "%d resolved discussions didn’t move, as Notion doesn’t give them to a connection; check them in Notion.",
		"someMore": "%d resolved discussions didn’t move, as Notion doesn’t give them to a connection. There may be more on pages Leafen couldn’t read in Notion; check them in Notion.",
		"noneRead": "Notion doesn’t give resolved discussions to a connection. The pages Leafen read in Notion had none, but pages it couldn’t read may; check them in Notion.",
		"unknown":  "Resolved discussions didn’t move, as Notion doesn’t give them to a connection. Leafen didn’t read your Notion, so it doesn’t know how many; check them in Notion.",
	},
}

// notionMigrationReportCommentLines are the report's lines on comments: those
// that didn't move — none given, those Notion didn't give, a stop — which the
// report says among the first things, and how many moved (none, when the run
// asked and Notion gave none) with what Notion's comments lack and the
// resolved discussions left, said later — only when the run's plan brought
// comments.
func notionMigrationReportCommentLines(
	language string, text map[string]string, report notionMigrationRunReport, tool string, resolved notionMigrationResolvedLeft,
) (missed, said []string) {
	if report.CommentsUnavailable {
		return []string{text["comments"]}, nil
	}
	if report.CommentsImported > 0 {
		said = append(said, fmt.Sprintf(text["commentsDone"], report.CommentsImported))
	} else if tool == "" && report.CommentsUnreadable == 0 && !report.CommentsStopped && notionMigrationCommentsAsked(report) {
		said = append(said, cmpOr(notionMigrationReportCommentsNone[language], notionMigrationReportCommentsNone[accountLanguageEn]))
	}
	if report.CommentsUnreadable > 0 {
		missed = append(missed, fmt.Sprintf(text["commentsUnreadable"], report.CommentsUnreadable))
	}
	if report.CommentsStopped {
		missed = append(missed, cmpOr(notionMigrationReportCommentsStopped[language], notionMigrationReportCommentsStopped[accountLanguageEn]))
	}
	// Notion gives a block's comments without the text they point at, and
	// only those not resolved.
	asked := notionMigrationCommentsAsked(report) || report.CommentsImported > 0 || report.CommentsUnreadable > 0 || report.CommentsStopped
	if tool == "" && asked {
		said = append(said, strings.TrimSpace(text["commentsPageOnly"]+" "+notionMigrationReportResolvedSentence(language, resolved)))
	}
	return missed, said
}

func notionMigrationReportResolvedSentence(language string, resolved notionMigrationResolvedLeft) string {
	words := notionMigrationReportResolved[language]
	if words == nil {
		words = notionMigrationReportResolved[accountLanguageEn]
	}
	switch {
	case !resolved.Counted:
		return words["unknown"]
	case resolved.Count > 0 && resolved.Partial:
		return fmt.Sprintf(words["someMore"], resolved.Count)
	case resolved.Count > 0:
		return fmt.Sprintf(words["some"], resolved.Count)
	case resolved.Partial:
		return words["noneRead"]
	}
	return ""
}

// notionMigrationCommentsAsked is whether a run's plan brought comments: its
// estimate, kept as the run started, gives their minutes only then.
func notionMigrationCommentsAsked(report notionMigrationRunReport) bool {
	return report.Estimate != nil && report.Estimate.CommentMinutes > 0
}
