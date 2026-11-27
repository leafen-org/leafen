package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The report's line on the imported forms that came without questions.
// Notion's API doesn't give a form's questions (its view gives only whether
// it takes responses and from whom, read 2026-11-07): a form the reading of
// the person's Notion didn't read comes without them, taking no answers, and
// opens offering to make them from its properties; reading it brings them
// and who may answer (notion_migration_formats_form.go). A form the reading
// read comes without them only when none of the properties they ask came, or
// when Leafen's form couldn't keep them — the line says which. A form that
// kept some of its questions is listed with what is left
// (notion_migration_formats_report.go).

// notionMigrationReportForms counts them by why.
type notionMigrationReportForms struct {
	// NotRead the reading didn't read; Unbrought it read, none of the
	// properties their questions ask having come; Refused it read, Leafen's
	// form not keeping their questions; Unsaved it read, the run not saving
	// its view (notion_migration_formats_apply.go).
	NotRead, Unbrought, Refused, Unsaved int
}

var notionMigrationReportFormsText = map[string]map[string]string{
	accountLanguageKo: {
		"notRead":   "폼 질문: Notion이 연결에 주지 않아 폼 %d개가 질문 없이, 응답을 받지 않는 채로 들어왔습니다. 에이전트가 로그인된 Notion에서 폼을 읽어 리펜에 넘기면 리펜이 질문과 응답 받기를 맞춥니다. 폼을 열어 속성으로 질문을 만들 수도 있습니다.",
		"unbrought": "폼 질문: 폼 %d개는 Notion에서 질문을 읽었지만 질문이 묻는 속성이 오지 않아(계획에서 보고서로 보냈거나 리펜에 없는 유형) 질문 없이 들어왔습니다. 폼을 열어 질문을 추가할 수 있습니다.",
		"refused":   "폼 질문: 폼 %d개는 Notion에서 질문을 읽었지만 리펜 폼이 담지 못해(폼이 채우지 않는 속성을 묻거나 글이 폼이 담는 것보다 깁니다) 질문 없이, 응답을 받지 않는 채로 들어왔습니다. 폼을 열면 속성으로 질문을 다시 만들 수 있습니다.",
		"unsaved":   "폼 질문: 폼 %d개는 Notion에서 질문을 읽었지만 리펜이 그 보기를 저장하지 못해(그 사이 보기가 바뀌었거나 리펜이 받지 않음) 질문 없이, 응답을 받지 않는 채로 들어왔습니다. 폼을 열면 속성으로 질문을 만들 수 있습니다.",
	},
	accountLanguageEn: {
		"notRead_one":     "Form questions: Notion doesn’t give them to a connection, so 1 form came without questions, taking no answers. When your agent has Leafen read it in your logged-in Notion, Leafen matches its questions and who may answer; or open it to make its questions from its properties.",
		"notRead_other":   "Form questions: Notion doesn’t give them to a connection, so %d forms came without questions, taking no answers. When your agent has Leafen read them in your logged-in Notion, Leafen matches their questions and who may answer; or open a form to make its questions from its properties.",
		"unbrought_one":   "Form questions: 1 form’s questions were read in Notion, but the properties they ask didn’t come (sent to the report by the plan, or of a type Leafen has none for), so it came without questions. Open it to add questions.",
		"unbrought_other": "Form questions: %d forms’ questions were read in Notion, but the properties they ask didn’t come (sent to the report by the plan, or of a type Leafen has none for), so they came without questions. Open a form to add questions.",
		"refused_one":     "Form questions: 1 form’s questions were read in Notion, but Leafen’s form couldn’t keep them (a question asks a property a form doesn’t fill in, or its words are longer than a form keeps), so it came without questions, taking no answers. Open it to make its questions again from its properties.",
		"refused_other":   "Form questions: %d forms’ questions were read in Notion, but Leafen’s form couldn’t keep them (a question asks a property a form doesn’t fill in, or its words are longer than a form keeps), so they came without questions, taking no answers. Open a form to make its questions again from its properties.",
		"unsaved_one":     "Form questions: 1 form’s questions were read in Notion, but Leafen couldn’t save its view (it changed meanwhile, or Leafen refused it), so it came without questions, taking no answers. Open it to make its questions from its properties.",
		"unsaved_other":   "Form questions: %d forms’ questions were read in Notion, but Leafen couldn’t save their views (they changed meanwhile, or Leafen refused them), so they came without questions, taking no answers. Open a form to make its questions from its properties.",
	},
}

// renderNotionMigrationReportForms writes the line for each why there is.
func renderNotionMigrationReportForms(line func(string, ...any), language string, forms notionMigrationReportForms) {
	text := notionMigrationReportFormsText[language]
	if text == nil {
		text = notionMigrationReportFormsText[accountLanguageEn]
	}
	for _, why := range []struct {
		key   string
		count int
	}{{"notRead", forms.NotRead}, {"unbrought", forms.Unbrought}, {"refused", forms.Refused}, {"unsaved", forms.Unsaved}} {
		if why.count == 0 {
			continue
		}
		words, counted := text[why.key]
		if !counted {
			words = text[why.key+map[bool]string{true: "_one", false: "_other"}[why.count == 1]]
		}
		line("- %s", strings.Replace(words, "%d", fmt.Sprint(why.count), 1))
	}
}

// loadNotionMigrationReportForms counts a run's imported forms without
// questions, against what the migration's reading read of them.
func loadNotionMigrationReportForms(app core.App, jobID string) (notionMigrationReportForms, error) {
	forms := notionMigrationReportForms{}
	views := []struct {
		SourceID string `db:"source_id"`
		Config   string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[mapping]].[[source_id]], [[view]].[[config]] FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]]=[[mapping]].[[native_id]]
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='view' AND [[view]].[[type]]={:form}
	`).Bind(dbx.Params{"jobId": jobID, "form": graphViewTypeForm}).All(&views); err != nil || len(views) == 0 {
		return forms, err
	}
	readings := []struct {
		NotionID string `db:"notion_id"`
		Value    string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[format]].[[notion_id]], [[format]].[[value]] FROM {{leafen_notion_migration_formats}} AS [[format]]
		JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration]].[[id]]=[[format]].[[migration_id]]
		WHERE [[migration]].[[import_job_id]]={:jobId} AND [[format]].[[kind]]='view'
	`).Bind(dbx.Params{"jobId": jobID}).All(&readings); err != nil {
		return forms, err
	}
	read, unsaved := map[string]*notionMigrationFormFormat{}, map[string]bool{}
	for _, reading := range readings {
		format := notionMigrationViewFormat{}
		if json.Unmarshal([]byte(reading.Value), &format) == nil && format.Form != nil {
			read[canonicalNotionID(reading.NotionID)] = format.Form
			unsaved[canonicalNotionID(reading.NotionID)] = format.Unsaved
		}
	}
	// The properties that came, by their database and Notion key.
	properties := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}} WHERE [[job_id]]={:jobId} AND [[kind]]='property'
	`).Bind(dbx.Params{"jobId": jobID}).Column(&properties); err != nil {
		return forms, err
	}
	came := map[string]bool{}
	for _, source := range properties {
		if database, property, found := strings.Cut(source, ":property:"); found && !strings.Contains(property, ":") {
			came[database+"\x00"+notionImportPropertyKey(property)] = true
		}
	}
	for _, view := range views {
		database, viewID, found := strings.Cut(view.SourceID, ":view:")
		if !found {
			continue
		}
		config := struct {
			Form *struct {
				Questions []json.RawMessage `json:"questions"`
			} `json:"leafenForm"`
		}{}
		_ = json.Unmarshal([]byte(view.Config), &config)
		form := read[canonicalNotionID(viewID)]
		switch {
		case form == nil && config.Form == nil:
			forms.NotRead++
		case form == nil || len(form.Questions) == 0 || config.Form != nil && len(config.Form.Questions) > 0:
			// Not read but set up since, as Notion had it, or with questions.
		case unsaved[canonicalNotionID(viewID)]:
			forms.Unsaved++
		case !notionMigrationFormAskedCame(form, database, came):
			forms.Unbrought++
		case config.Form == nil:
			forms.Refused++
		}
	}
	return forms, nil
}

// notionMigrationFormAskedCame reports whether any property a read form's
// questions ask came.
func notionMigrationFormAskedCame(form *notionMigrationFormFormat, database string, came map[string]bool) bool {
	for _, question := range form.Questions {
		if came[database+"\x00"+notionImportPropertyKey(question.Property)] {
			return true
		}
	}
	return false
}
