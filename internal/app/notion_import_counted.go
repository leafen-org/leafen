package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How a Notion import counts what it read, as the person knows their Notion,
// said once for its progress, its report's line of what moved and a run
// again's tally of what it made new, changed and left as it was (2026-11-16:
// a run again's report said 2 pages moved and 3 left as they were — the
// tally counted the page Leafen makes to hold a full-page database, which the
// line of what moved doesn't — and a run's progress counted a database's 2
// templates among its rows, 241 of Notion's 239):
//
//   - a page is one of Notion's, not the page Leafen makes to hold a
//     full-page database;
//   - a database is one of Notion's, its other data sources with it, as the
//     scan counts it;
//   - a row is one of a database's: one in Notion's trash, which comes to
//     Leafen's, and a template are counted apart — a run again's templates
//     with those an earlier run made that Notion still gives and left as they
//     were (notionImportKeptTemplatesSQL), not one Notion no longer gives;
//   - a block is one of a page's, not a page's, database's or block's icon or
//     cover nor a file in a files property, which go with what holds them,
//     nor the line Leafen takes a callout's words out into;
//   - what the body of a template a run again left as it was holds — its
//     blocks, and any page, database or row in it — counts as read and left
//     as it was, as the run that last read it counted it, unless a person
//     deleted the template for good (notionImportKeptTemplateBodySQL);
//   - what the body of a template a run again read again holds counts as a
//     page's body does, made, changed or left as it was by what it held
//     before (notionMigrationTally.countTemplateBodiesAgain);
//   - the pages and rows the report names as made or last changed by a
//     Notion person no member stands for are these pages and rows, those in
//     Notion's trash with them, a kept template's body's too
//     (notionImportAuthoredSQL).
//
// notionImportCountedKindSQL says the same of a staged source in SQL.

const (
	notionImportCountedPage       = "page"
	notionImportCountedDatabase   = "database"
	notionImportCountedRow        = "row"
	notionImportCountedTrashedRow = "trashed_row"
	notionImportCountedTemplate   = "template"
	notionImportCountedBlock      = "block"
)

// notionImportCountedKind is what a staged source counts as, by its kind,
// source and parent kind and text, "" for nothing
// (a property, a view, an issue, the holder of a full-page database, a file
// something holds).
func notionImportCountedKind(kind, sourceID, parentKind, text string) string {
	switch kind {
	case "page":
		if strings.HasPrefix(sourceID, notionDatabaseRootPageSourceID("")) {
			return ""
		}
		return notionImportCountedPage
	case "database":
		if parentKind == notionImportPropertyIssueParentKind {
			return ""
		}
		return notionImportCountedDatabase
	case "row":
		switch {
		case strings.Contains(text, `"`+notionImportRowTemplateKey+`"`):
			return notionImportCountedTemplate
		case strings.Contains(text, `"`+notionImportRowArchivedKey+`"`):
			return notionImportCountedTrashedRow
		}
		return notionImportCountedRow
	case "property", "view", "issue":
		return ""
	}
	if strings.HasSuffix(sourceID, notionImportCalloutFirstLineSuffix) ||
		kind == graphBlockTypeFile && notionImportOwnedFileSource(sourceID) {
		return ""
	}
	return notionImportCountedBlock
}

// notionImportCountedKindSQL is notionImportCountedKind of a row of
// leafen_notion_import_sources, read alone, with notionImportCountedParams
// bound.
var notionImportCountedKindSQL = `CASE
	WHEN [[kind]] = 'page' THEN CASE WHEN substr([[source_id]], 1, length({:countedHolder})) = {:countedHolder} THEN '' ELSE 'page' END
	WHEN [[kind]] = 'database' THEN CASE WHEN [[parent_kind]] = {:countedDataSource} THEN '' ELSE 'database' END
	WHEN [[kind]] = 'row' THEN CASE
		WHEN instr([[text]], {:countedTemplate}) > 0 THEN 'template'
		WHEN instr([[text]], {:countedTrashed}) > 0 THEN 'trashed_row'
		ELSE 'row' END
	WHEN [[kind]] IN ('property', 'view', 'issue') THEN ''
	WHEN substr([[source_id]], -length({:countedCalloutLine})) = {:countedCalloutLine} THEN ''
	WHEN [[kind]] = 'file' AND (instr([[source_id]], {:countedPropertyFile}) > 0 OR ` + notionImportOwnedPresentationSQL() + `) THEN ''
	ELSE 'block' END`

// A run again reads a migration's templates from Notion's list of each
// database's, and passes by one an earlier run made that Notion didn't change
// since (notion_import_templates.go): the ledger's entry for each template
// Notion still gives keeps, as its state, the run that read it last
// (markNotionImportTemplateGiven). One Notion no longer gives — deleted
// there, out of the connection's reach, or of a database the plan left out —
// keeps an earlier run's, and stays in Leafen as a page or row the source no
// longer gives does, named in the report (notion_migration_removed.go)
// (2026-11-17: a run again's report and check said 2 templates came after one
// of the 2 was deleted in Notion).

// markNotionImportTemplateGiven notes that Notion gave the run a template an
// earlier run of its migration made.
func markNotionImportTemplateGiven(app core.App, jobID, migrationID, sourceID string) error {
	if migrationID == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migration_ledger}} SET [[state]]={:jobId}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{"jobId": jobID, "migrationId": migrationID, "sourceId": sourceID, "kind": notionMigrationLedgerTemplate}).Execute()
	return err
}

// notionImportKeptTemplatesWhereSQL picks, in a migration's ledger, the
// templates its earlier runs made that this run's Notion still gave and that
// the run left as they were — Notion didn't change them, so the run didn't
// stage them — which count among the templates the run read, in its
// progress, its report's line and its check against Notion alike
// (2026-11-17: a run again's check said it compared 0 templates while its
// report said 2 came). Bind :migrationId and :jobId with
// notionImportCountedParams.
var notionImportKeptTemplatesWhereSQL = `[[migration_id]]={:migrationId} AND [[kind]]={:countedLedgerTemplate}
	AND [[state]]={:jobId} AND [[source_id]] NOT IN (
		SELECT [[source_id]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId})`

// notionImportKeptTemplatesSQL counts notionImportKeptTemplatesWhereSQL's
// templates.
var notionImportKeptTemplatesSQL = `(SELECT COUNT(*) FROM {{leafen_notion_migration_ledger}}
	WHERE ` + notionImportKeptTemplatesWhereSQL + `)`

// A run again doesn't read a template it leaves as it was, nor its body, so
// the ledger keeps, by each template's source, what its body held as the
// run that read it last counted it (2026-11-17: a run's report said it read
// 3 blocks, a page's and its 2 templates' bodies, and a run again with
// nothing changed said it read 1 and left 1 as it was).
const notionMigrationLedgerTemplateBody = "template_body"

// notionImportKeptTemplateBodiesWhereSQL picks, as [[body]], the ledger's
// records of what the bodies of notionImportKeptTemplatesWhereSQL's
// templates held, of those still in Leafen, in its trash too: one a person
// deleted for good holds nothing (notionMigrationTemplateGone), so nothing
// of its body counts as read or left as it was, though the template still
// counts among those Notion gave, and the check says it is gone
// (2026-11-17: a template deleted for good kept its body's 3 blocks among
// those read and left as they were).
var notionImportKeptTemplateBodiesWhereSQL = `[[body]].[[migration_id]]={:migrationId} AND [[body]].[[kind]]={:countedLedgerTemplateBody}
	AND json_valid([[body]].[[state]]) AND [[body]].[[source_id]] IN (
		SELECT [[source_id]] FROM {{leafen_notion_migration_ledger}} WHERE ` + notionImportKeptTemplatesWhereSQL + `
			AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_database_templates}}))`

// notionImportKeptTemplateBodySQL sums what the bodies of
// notionImportKeptTemplateBodiesWhereSQL held of one count of
// notionMigrationTallyCounts: pages, databases, rows or blocks.
func notionImportKeptTemplateBodySQL(count string) string {
	return `(SELECT COALESCE(SUM(json_extract([[body]].[[state]], '$.` + count + `')), 0)
	FROM {{leafen_notion_migration_ledger}} AS [[body]]
	WHERE ` + notionImportKeptTemplateBodiesWhereSQL + `)`
}

// notionImportKept is what a run again left as it was of the templates an
// earlier run made: how many, and what their bodies held.
type notionImportKept struct {
	Templates int
	Body      notionMigrationTallyCounts
}

// notionImportKeptTemplates is notionImportKeptTemplatesSQL and
// notionImportKeptTemplateBodySQL of a run; nothing for an import that isn't
// a migration's.
func notionImportKeptTemplates(app core.App, jobID, migrationID string) (notionImportKept, error) {
	if migrationID == "" {
		return notionImportKept{}, nil
	}
	read := struct {
		Templates int `db:"templates"`
		Pages     int `db:"pages"`
		Databases int `db:"databases"`
		Rows      int `db:"rows"`
		Blocks    int `db:"blocks"`
	}{}
	err := app.DB().NewQuery(`SELECT ` + notionImportKeptTemplatesSQL + ` AS [[templates]],
		` + notionImportKeptTemplateBodySQL("pages") + ` AS [[pages]],
		` + notionImportKeptTemplateBodySQL("databases") + ` AS [[databases]],
		` + notionImportKeptTemplateBodySQL("rows") + ` AS [[rows]],
		` + notionImportKeptTemplateBodySQL("blocks") + ` AS [[blocks]]`).
		Bind(notionImportCountedParams(dbx.Params{"jobId": jobID, "migrationId": migrationID})).One(&read)
	return notionImportKept{Templates: read.Templates, Body: notionMigrationTallyCounts{
		Pages: read.Pages, Databases: read.Databases, Rows: read.Rows, Blocks: read.Blocks,
	}}, err
}

// notionImportTemplateBody is what a template's body held, with the
// database it is a template of (notion_migration_verify_rows.go).
type notionImportTemplateBody struct {
	notionMigrationTallyCounts
	Database string `json:"database"`
	// Who made and who last changed each page and row in it, by their Notion
	// IDs (notionImportAuthoredSQL).
	Authors [][2]string `json:"authors,omitempty"`
	// Each page, database, row and block in it as the run took it, by its
	// source (notionImportTemplateBodySourceDigest).
	Read map[string]string `json:"read,omitempty"`
}

// recordNotionImportTemplateBodies keeps in a migration's ledger what the
// body of each template the run read held, as notionImportCountedKind counts
// it, from the template down a level of its body at a time.
func recordNotionImportTemplateBodies(app core.App, migrationID, jobID string) error {
	if migrationID == "" {
		return nil
	}
	params := notionImportCountedParams(dbx.Params{"jobId": jobID, "migrationId": migrationID})
	templates := []struct {
		SourceID string `db:"source_id"`
		Database string `db:"parent_source_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]], [[parent_source_id]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='row' AND instr([[text]], {:countedTemplate}) > 0
	`).Bind(params).All(&templates); err != nil || len(templates) == 0 {
		return err
	}
	of, bodies, level := map[string]string{}, map[string]*notionImportTemplateBody{}, []string{}
	for _, template := range templates {
		of[template.SourceID], level = template.SourceID, append(level, template.SourceID)
		bodies[template.SourceID] = &notionImportTemplateBody{Database: template.Database, Read: map[string]string{}}
	}
	for len(level) > 0 {
		encoded, err := json.Marshal(level)
		if err != nil {
			return err
		}
		held := []struct {
			SourceID     string `db:"source_id"`
			Parent       string `db:"parent_source_id"`
			Kind         string `db:"kind"`
			Title        string `db:"title"`
			Text         string `db:"text"`
			Counted      string `db:"counted"`
			CreatedBy    string `db:"created_by"`
			LastEditedBy string `db:"last_edited_by"`
		}{}
		params["level"] = string(encoded)
		if err := app.DB().NewQuery(`
			SELECT [[source_id]], [[parent_source_id]], [[kind]], [[title]], [[text]], ` + notionImportCountedKindSQL + ` AS [[counted]],
				` + notionImportAuthorSQL("createdBy") + ` AS [[created_by]], ` + notionImportAuthorSQL("lastEditedBy") + ` AS [[last_edited_by]]
			FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[parent_source_id]] IN (SELECT [[value]] FROM json_each({:level}))
		`).Bind(params).All(&held); err != nil {
			return err
		}
		level = nil
		for _, item := range held {
			if _, seen := of[item.SourceID]; seen {
				continue
			}
			of[item.SourceID] = of[item.Parent]
			level = append(level, item.SourceID)
			body := bodies[of[item.Parent]]
			if notionImportAuthoredKinds[item.Counted] {
				body.Authors = append(body.Authors, [2]string{item.CreatedBy, item.LastEditedBy})
			}
			if body.countKind(item.Counted, 1) {
				body.Read[item.SourceID] = notionImportTemplateBodySourceDigest(item.Kind, item.Counted, item.Title, item.Text)
			}
		}
	}
	for template, body := range bodies {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_notion_migration_ledger}}
				([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
			SELECT [[id]], [[workspace_id]], {:sourceId}, {:kind}, '', '', {:body}
			FROM {{leafen_notion_migrations}} WHERE [[id]]={:migrationId}
			ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO UPDATE SET [[state]]=excluded.[[state]]
		`).Bind(dbx.Params{
			"migrationId": migrationID, "sourceId": template, "kind": notionMigrationLedgerTemplateBody, "body": string(encoded),
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// A run again that reads again a template Notion changed makes its body anew
// (updateNotionImportTemplate) — the row standing for it forgot its body
// (clearNotionImportTemplateRowBody) — and counts that body as it counts a
// page's or row's: what is new in it as made, what Notion changed in it as
// changed, the rest as left as it was (2026-11-17: a template Notion added a
// line to counted its whole body as made, 6 blocks). The ledger's record of
// the body keeps each thing in it as the run that read it last took it.

// notionImportTemplateBodySourceDigest is what a run again compares of a
// page, database, row or block in a template's body, as it compares one
// anywhere else: a page's or database's name, a row's cells, a block's
// source without its time (notion_migration_source_change.go); "" for a
// block it takes no change of (a table, a bookmark, a file).
func notionImportTemplateBodySourceDigest(kind, counted, title, text string) string {
	compared := text
	switch {
	case counted == notionImportCountedPage || counted == notionImportCountedDatabase:
		compared = title
	case counted == notionImportCountedRow:
		cells := map[string]json.RawMessage{}
		if json.Unmarshal([]byte(text), &cells) == nil {
			maps.DeleteFunc(cells, func(key string, _ json.RawMessage) bool { return notionImportRowSpecialKey(key) })
			if encoded, err := json.Marshal(cells); err == nil {
				compared = string(encoded)
			}
		}
	case kind == "paragraph" || kind == "template" || kind == "block":
		if plan, err := decodeNotionParagraphSourceText(text); err == nil {
			if digest, err := notionMigrationBlockSourceDigest(plan); err == nil {
				return digest
			}
		}
	default:
		return ""
	}
	digest := sha256.Sum256([]byte(compared))
	return hex.EncodeToString(digest[:])
}

// countTemplateBodiesAgain counts, in a run again, what a turn made of the
// bodies the ledger keeps of templates read before as changed or left as it
// was, by notionImportTemplateBodySourceDigest; what else it made stays made.
func (tally *notionMigrationTally) countTemplateBodiesAgain(app core.App, jobID string, payload notionImportJobPayload) error {
	if payload.MigrationRerunSince <= 0 || payload.MigrationID == "" || len(tally.made) == 0 {
		return nil
	}
	encoded, err := json.Marshal(tally.made)
	if err != nil {
		return err
	}
	read := []struct {
		Sequence int64  `db:"sequence"`
		Kind     string `db:"kind"`
		Title    string `db:"title"`
		Text     string `db:"text"`
		Counted  string `db:"counted"`
		Before   string `db:"before"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source]].[[sequence]], [[source]].[[kind]], [[source]].[[title]], [[source]].[[text]], [[source]].[[counted]],
			[[read]].[[value]] AS [[before]]
		FROM (SELECT [[sequence]], [[source_id]], [[kind]], [[title]], [[text]], ` + notionImportCountedKindSQL + ` AS [[counted]]
			FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[sequence]] IN (SELECT [[value]] FROM json_each({:made}))) AS [[source]]
		JOIN {{leafen_notion_migration_ledger}} AS [[body]] ON [[body]].[[migration_id]]={:migrationId}
			AND [[body]].[[kind]]={:countedLedgerTemplateBody} AND json_valid([[body]].[[state]])
		JOIN json_each([[body]].[[state]], '$.read') AS [[read]] ON [[read]].[[key]]=[[source]].[[source_id]]
	`).Bind(notionImportCountedParams(dbx.Params{
		"jobId": jobID, "migrationId": payload.MigrationID, "made": string(encoded),
	})).All(&read); err != nil {
		return err
	}
	moved := map[int64]bool{}
	for _, item := range read {
		if moved[item.Sequence] || !tally.Made.countKind(item.Counted, -1) {
			continue
		}
		moved[item.Sequence] = true
		counts := &tally.Kept
		if notionImportTemplateBodySourceDigest(item.Kind, item.Counted, item.Title, item.Text) != item.Before {
			counts = &tally.Changed
		}
		counts.countKind(item.Counted, 1)
	}
	return nil
}

// notionImportAuthoredKinds are what notionImportCountedKind calls the
// pages and rows the report names by who made and last changed them.
var notionImportAuthoredKinds = map[string]bool{
	notionImportCountedPage: true, notionImportCountedRow: true, notionImportCountedTrashedRow: true,
}

// notionImportAuthorSQL reads, of a staged page or row of
// leafen_notion_import_sources, the Notion person who made it ("createdBy")
// or last changed it ("lastEditedBy"), "" for anything else; with
// notionImportCountedParams bound.
func notionImportAuthorSQL(who string) string {
	return `CASE
		WHEN [[kind]] = 'row' AND json_valid([[text]])
			THEN COALESCE(json_extract([[text]], '$."` + notionImportRowAuthorsKey + `".value.` + who + `'), '')
		WHEN [[kind]] = 'page' AND substr([[text]], 1, {:countedPresentationSkip} - 1) = {:countedPresentationPrefix}
			AND json_valid(substr([[text]], {:countedPresentationSkip}))
			THEN COALESCE(json_extract(substr([[text]], {:countedPresentationSkip}), '$.history.` + who + `'), '')
		ELSE '' END`
}

// notionImportAuthoredSQL lists who made and who last changed each page and
// row a run read (notionImportAuthoredKinds), and each in the bodies of the
// templates it left as they were (notionImportKeptTemplateBodiesWhereSQL),
// as the run that last read them kept it
// (2026-11-17: a first run named 8, the page holding a full-page database
// and 2 templates among them, and a run again with nothing changed 6). Bind
// :jobId and :migrationId with notionImportCountedParams.
var notionImportAuthoredSQL = `
	SELECT [[created_by]], [[last_edited_by]] FROM (
		SELECT ` + notionImportCountedKindSQL + ` AS [[counted]],
			` + notionImportAuthorSQL("createdBy") + ` AS [[created_by]], ` + notionImportAuthorSQL("lastEditedBy") + ` AS [[last_edited_by]]
		FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[kind]] IN ('page', 'row'))
	WHERE [[counted]] IN ({:authoredPage}, {:authoredRow}, {:authoredTrashedRow})
	UNION ALL
	SELECT COALESCE(json_extract([[author]].[[value]], '$[0]'), ''), COALESCE(json_extract([[author]].[[value]], '$[1]'), '')
	FROM {{leafen_notion_migration_ledger}} AS [[body]], json_each([[body]].[[state]], '$.authors') AS [[author]]
	WHERE ` + notionImportKeptTemplateBodiesWhereSQL

// notionImportOwnedPresentationSQL matches a presentation file's source:
// what notionImportPresentationFileTarget finds.
func notionImportOwnedPresentationSQL() string {
	matches := []string{}
	for _, suffix := range []string{
		notionPresentationPageIcon, notionPresentationPageCover,
		notionPresentationDatabaseIcon, notionPresentationDatabaseCover, notionPresentationBlockIcon,
	} {
		matches = append(matches, "(length([[source_id]]) > "+strconv.Itoa(len(suffix))+
			" AND substr([[source_id]], -"+strconv.Itoa(len(suffix))+") = '"+suffix+"')")
	}
	return strings.Join(matches, " OR ")
}

// notionImportCountedParams binds notionImportCountedKindSQL's values into
// params.
func notionImportCountedParams(params dbx.Params) dbx.Params {
	params["countedHolder"] = notionDatabaseRootPageSourceID("")
	params["countedDataSource"] = notionImportPropertyIssueParentKind
	params["countedTemplate"] = `"` + notionImportRowTemplateKey + `"`
	params["countedTrashed"] = `"` + notionImportRowArchivedKey + `"`
	params["countedCalloutLine"] = notionImportCalloutFirstLineSuffix
	params["countedPropertyFile"] = notionImportPropertyFileMarker
	params["countedLedgerTemplate"] = notionMigrationLedgerTemplate
	params["countedLedgerTemplateBody"] = notionMigrationLedgerTemplateBody
	params["countedPresentationPrefix"] = notionPresentationSourcePrefix
	params["countedPresentationSkip"] = len(notionPresentationSourcePrefix) + 1
	params["authoredPage"] = notionImportCountedPage
	params["authoredRow"] = notionImportCountedRow
	params["authoredTrashedRow"] = notionImportCountedTrashedRow
	return params
}
