package app

// Each big stage of a move is said to the person as it starts, with the
// address where they watch it live (user decision 2026-11-23 "묶어서 올려":
// "주요단계 … 일때는 알려주고 그 단계마다 링크를 줘서 … 실시간으로
// 알 수 있도록" — the scan, the one question, the settings reading, the run's
// reading, making, comments and check, and the end). An answer that starts a
// stage gives that line in Leafen's words, in the person's language, the
// address in the sentence: told only of a field named link, agents sent the
// line without it, and lines an agent composed itself between calls stayed
// in its own thinking (2026-11-23, isolated agent runs); a line given in
// Leafen's words reached the person, as the time left's why did. The one
// question carries its link in the question (notion_migration_confirm.go),
// and the end its report and moved content.

// notionMigrationStageLineWords is what each stage's line says has begun,
// in English and Korean.
var notionMigrationStageLineWords = map[string][2]string{
	notionMigrationStageScanning:  {"Leafen is reading your Notion’s structure now", "리펜이 지금 Notion의 구조를 읽고 있습니다"},
	notionMigrationStageFormats:   {"Leafen is reading how your Notion looks now", "리펜이 지금 Notion의 꾸밈을 읽고 있습니다"},
	notionMigrationStageReading:   {"The move has started: Leafen is reading Notion now", "옮기기를 시작했습니다. 지금 Notion을 읽고 있습니다"},
	notionMigrationStageCreating:  {"Notion is read: Leafen is making it now", "Notion을 다 읽었고, 지금 리펜에 만들고 있습니다"},
	notionMigrationStageComments:  {"The content is in: Leafen is moving the comments now", "내용은 다 들어왔고, 지금 댓글을 옮기고 있습니다"},
	notionMigrationStageVerifying: {"Leafen is checking the move against Notion now", "리펜이 지금 옮긴 것을 Notion과 대조하고 있습니다"},
}

// The stages a run's line is said for besides the run's own: the scan and
// the settings reading.
const (
	notionMigrationStageScanning = "scanning"
	notionMigrationStageFormats  = "formats"
)

// notionMigrationStageLine is a stage's line in the person's language, its
// live view's address left as notionMigrationLinkMark for the answer to name
// (notionMigrationNamesLink); "" for a stage without one.
func notionMigrationStageLine(language, stage string) string {
	return notionMigrationStageLineLeft(language, stage, 0)
}

// notionMigrationStageLineLeft is a running stage's line with its time left
// (minutes; 0 for none): the line an agent sends as the move goes is where
// it is and how long it still takes, so the address goes in that line
// (2026-11-23: agents sent the time left at each look and said each stage's
// start in their own thinking, without the address).
func notionMigrationStageLineLeft(language, stage string, minutes int) string {
	words, ok := notionMigrationStageLineWords[stage]
	if !ok {
		return ""
	}
	left := ""
	if when := notionMigrationLeftWords(language, minutes); when != "" {
		left = " · " + when
	}
	if language == accountLanguageKo {
		return words[1] + left + ". 진행 상황은 리펜 가져오기 창에서 실시간으로 보실 수 있습니다: " + notionMigrationLinkMark
	}
	return words[0] + left + ". You can watch it live in Leafen’s import window: " + notionMigrationLinkMark
}

// notionMigrationStageLineNext is what an answer says is left to do as its
// stage starts: that stage's line to the person, once for the stage, as the
// answer's stage_line gives it (2026-11-23: quoted in next, agents wrote
// their own line without the address, having given it once before).
func notionMigrationStageLineNext(language, stage string) string {
	if notionMigrationStageLine(language, stage) == "" {
		return ""
	}
	return notionMigrationStageLineFacts
}

// notionMigrationStageLineFacts is what an answer with a stage_line says of
// it.
const notionMigrationStageLineFacts = "stage_line is where the move is now, in Leafen's words in the person's language, with the address of their live view in it: " +
	"each line to them on the move carries the address, so they open the view from the line they read, whatever was said before. " +
	"Left to do as each stage starts, once for it: stage_line to the person"

// notionMigrationStageLineIn is the stage_line of an answer about a
// migration: the line of the stage it is in now, as its status reads it
// (notionMigrationStatusStage), its address named, with the time left where
// the stage has one (minutes; 0 for none) — "" before any stage of it starts,
// while its plan waits on the person, and once it is done or stopped. Every
// answer about a migration takes its line here and the settings reading its
// own as it starts it (notion_migration_formats_read.go), so no answer says a
// stage the move is not in (2026-11-24: the sources' answer, before any
// migration was started, already said Leafen was reading the structure).
func notionMigrationStageLineIn(language string, migration notionMigration, job *notionImportJob, link string, minutes int) string {
	return notionMigrationStageLineOf(language, notionMigrationStatusStage(migration, job), link, minutes)
}

// notionMigrationStageLineOf is an answer's stage_line, its address named,
// with the time left where the stage has one (minutes; 0 for none).
func notionMigrationStageLineOf(language, stage, link string, minutes int) string {
	if link == "" {
		return ""
	}
	return notionMigrationNamesLink(notionMigrationStageLineLeft(language, stage, minutes), link)
}
