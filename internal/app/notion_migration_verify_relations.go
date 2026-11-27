package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
)

// The check's two-way relations: in Notion a two-way relation names its rows
// from both sides, so each row one side names names it back. The check lists
// each link of the databases the run made that only one side holds — a row
// naming another that doesn't name it back — with the relation's names and
// how an agent puts it back as Notion has it (2026-11-24: a run again left a
// row Notion had trashed naming its customer one way, and the check said
// nothing differed). A row in either trash still counts, as a relation keeps
// one (database_row_trash_relations.go); a row deleted for good doesn't.
//
// Only rows are compared: a row that stands for a template the run makes
// (notionMigrationVerifyTemplateRows) is left out on either side. Notion
// doesn't name a template back from the rows its relation names — seen
// 2026-11-24 on the gate's synthetic source, by the API (상대 하나's
// "Related to 장부 표" lists 장부 하나 and 장부 넷, not the template 새 항목
// naming it) and on its page in Notion alike — and such a row is taken away
// only once the databases of its body are copied into the template, after
// the check: the check had listed 새 항목 → 상대 하나 as one way only.

// A two-way relation's link only one side holds has the setting
// notionMigrationVerifyOneWay (contracts/notion-import-wire.json).

type notionMigrationVerifyRelation struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	DataSourceID string `db:"data_source_id"`
	Page         string `db:"page_id"`
	Name         string `db:"name"`
	Config       string `db:"config"`
}

func (v *notionMigrationVerifier) relations() error {
	made := map[string]string{}
	for key, id := range v.sources {
		made[id] = key
	}
	if len(made) == 0 {
		return nil
	}
	encoded, err := json.Marshal(workspaceExportSortedKeys(made))
	if err != nil {
		return err
	}
	templates, err := v.templateRows()
	if err != nil {
		return err
	}
	relations := []notionMigrationVerifyRelation{}
	if err := v.app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[owner]], [[property]].[[data_source_id]], [[source]].[[page_id]],
			[[property]].[[name]], [[property]].[[config]]
		FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]]=[[property]].[[data_source_id]]
		WHERE [[property]].[[type]]='relation' AND [[property]].[[data_source_id]] IN (SELECT [[value]] FROM json_each({:sources}))
		ORDER BY [[property]].[[data_source_id]], [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{"sources": string(encoded)}).All(&relations); err != nil {
		return err
	}
	for _, relation := range relations {
		config := map[string]any{}
		if json.Unmarshal([]byte(relation.Config), &config) != nil {
			continue
		}
		pair := databaseRelationPairOf(relation.ID, relation.DataSourceID, graphValueTypeRelation, config)
		if pair == nil || pair.selfPaired(relation.ID) {
			continue
		}
		other, _, exists, err := loadDatabaseRelationPairTargetProperty(v.app, relation.Owner, pair.PairPropertyID)
		if err != nil {
			return err
		}
		named, err := databaseRelationPairNamesBack(v.app, relation.Owner, databaseRelationPairPayload{
			SourceDataSourceID: relation.DataSourceID, PropertyID: relation.ID,
			TargetDataSourceID: pair.TargetDataSourceID, PairPropertyID: pair.PairPropertyID,
		})
		if err != nil {
			return err
		}
		if !exists || !named {
			continue
		}
		if err := v.oneWay(relation, pair, other, templates); err != nil {
			return err
		}
	}
	return nil
}

// templateRows are the rows that stand for a template the run makes, as a
// JSON list: one whose staged row is a template's, before the run makes the
// template, and one the run made a template of, which goes once the
// databases of its body are copied into it (deferRowPageRemovalToDatabaseCopies).
func (v *notionMigrationVerifier) templateRows() (string, error) {
	rows := []string{}
	if err := v.app.DB().NewQuery(`
		SELECT [[mapping]].[[native_id]] FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_notion_import_sources}} AS [[source]] ON [[source]].[[job_id]]=[[mapping]].[[job_id]]
			AND [[source]].[[source_id]]=[[mapping]].[[source_id]] AND [[source]].[[kind]]='row'
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='row' AND instr([[source]].[[text]], {:countedTemplate})>0
		UNION
		SELECT json_extract([[payload_json]], '$.sourcePageId') FROM {{leafen_jobs}}
		WHERE [[kind]]={:copyKind} AND [[status]]={:queued} AND json_extract([[payload_json]], '$.importTemplateRow')=1
	`).Bind(notionImportCountedParams(dbx.Params{
		"jobId": v.jobID, "copyKind": rowPageDatabaseCopyJobKind, "queued": durableJobStatusQueued,
	})).Column(&rows); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(rows)
	return string(encoded), err
}

// oneWay lists the links of a relation between rows whose other side
// doesn't name back.
func (v *notionMigrationVerifier) oneWay(
	relation notionMigrationVerifyRelation, pair *databaseRelationPair, other databaseRelationPairTargetProperty, templates string,
) error {
	links := []struct {
		RowID  string `db:"row_id"`
		Row    string `db:"row_title"`
		Target string `db:"target_title"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[item]].[[row_id]], COALESCE([[row]].[[title]], '') AS [[row_title]], COALESCE([[target]].[[title]], '') AS [[target_title]]
		FROM {{leafen_database_relation_items}} AS [[item]]
		INDEXED BY [[idx_leafen_database_relation_items_property]]
		JOIN {{leafen_database_rows}} AS [[held]] ON [[held]].[[id]]=[[item]].[[target_row_id]] AND [[held]].[[data_source_id]]={:target}
		LEFT JOIN {{leafen_pages}} AS [[row]] ON [[row]].[[id]]=[[item]].[[row_id]]
		LEFT JOIN {{leafen_pages}} AS [[target]] ON [[target]].[[id]]=[[item]].[[target_row_id]]
		WHERE [[item]].[[owner]]={:owner} AND [[item]].[[page_id]]={:page} AND [[item]].[[property_id]]={:property}
			AND [[item]].[[row_id]] NOT IN (SELECT [[value]] FROM json_each({:templates}))
			AND [[item]].[[target_row_id]] NOT IN (SELECT [[value]] FROM json_each({:templates}))
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_relation_items}} AS [[back]]
				WHERE [[back]].[[owner]]={:owner} AND [[back]].[[property_id]]={:pair}
					AND [[back]].[[row_id]]=[[item]].[[target_row_id]] AND [[back]].[[target_row_id]]=[[item]].[[row_id]]
			)
		ORDER BY [[item]].[[row_id]], [[item]].[[ordinal]]
	`).Bind(dbx.Params{
		"owner": relation.Owner, "page": relation.Page, "property": relation.ID,
		"target": pair.TargetDataSourceID, "pair": pair.PairPropertyID, "templates": templates,
	}).All(&links); err != nil {
		return err
	}
	database := notionMigrationVerifyDatabase(v.sourceKey(relation.DataSourceID))
	untitled := notionMigrationVerifyText[accountLanguageEn]["untitled"]
	if text := notionMigrationVerifyText[v.language]; text != nil {
		untitled = text["untitled"]
	}
	title := func(value string) string {
		if value = strings.TrimSpace(value); value == "" {
			return untitled
		}
		return value
	}
	for _, link := range links {
		v.differ(notionMigrationVerifyDifference{
			Kind: notionMigrationVerifyKindRelation, Setting: notionMigrationVerifyOneWay, NotionID: database, ID: link.RowID,
			Title:  title(link.Row) + " → " + title(link.Target),
			Notion: relation.Name + " ↔ " + other.Name, Leafen: relation.Name + " → " + other.Name,
			Reason: notionMigrationVerifyUnexplained,
			Fix:    "notion-update-page: set the relation on either row as Notion has it; the other side follows",
		})
	}
	return nil
}

// sourceKey is the staged key of a data source the run made.
func (v *notionMigrationVerifier) sourceKey(dataSourceID string) string {
	for key, id := range v.sources {
		if id == dataSourceID {
			return key
		}
	}
	return ""
}
