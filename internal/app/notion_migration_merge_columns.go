package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The columns a merged database's properties, or a split's rows', stand for
// in their target (notion_migration_merge.go): each property as the plan
// leaves it, and the target's column of its name and type it matches, by the
// one rule the plan's preview judges it by too (notionMergeStandsFor).

// notionMergeColumn is a property as the plan leaves it: its name, its
// type, the type it comes as and, for a relation, the database it relates
// to through the merges; Kept is false for one the plan only reports.
type notionMergeColumn struct {
	Name, Type, Source, Target string
	Kept                       bool
	// The staged property, for a formula's or rollup's definition
	// (notion_migration_merge_computed.go).
	staged storedNotionSource
}

func (run *notionMigrationRunPlan) mergeColumn(databaseID string, source storedNotionSource) (notionMergeColumn, error) {
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return notionMergeColumn{}, err
	}
	_, propertyID, _ := strings.Cut(source.SourceID, ":property:")
	entry := run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)]
	column := notionMergeColumn{
		Name: strings.TrimSpace(entry.Name), Type: strings.TrimSpace(entry.Type), Source: plan.Type,
		Kept: entry.Action == "map" || entry.Action == "preserve", staged: source,
	}
	if column.Name == "" {
		column.Name = strings.TrimSpace(source.Title)
	}
	if entry.Action == "preserve" {
		column.Type = graphValueTypeRichText
	}
	if column.Type == "" {
		column.Type = plan.Type
	}
	if plan.Link != nil && plan.Type == graphValueTypeRelation {
		target := plan.Link.RelationDatabaseID
		if into, merged := run.mergeTarget(target); merged {
			target = into
		}
		column.Target = notionMigrationSourceKey(target)
	}
	// A property turned into objects is a relation to their database
	// (notion_migration_objects.go).
	if entry.Action == "object" {
		target := entry.Database
		if into, merged := run.mergeTarget(target); merged {
			target = into
		}
		column.Type, column.Source, column.Target, column.Kept = graphValueTypeRelation, graphValueTypeRelation, notionMigrationSourceKey(target), true
	}
	return column, nil
}

// notionMergeTargetProperty is the column of the target a merged
// database's property stands for (notionMergeStandsFor) — first among the
// target's own properties, then among the columns other databases merged
// into it, or splitting rows into it, bring as new ones; found gives what a
// match became, by its key in the ledger, "" for one not made yet. "" when
// there is none, and the property becomes a new column, under the name the
// column gives — '<name> (<its database>)' for a formula or rollup coming
// apart.
func notionMergeTargetProperty(
	app core.App, jobID string, run *notionMigrationRunPlan, source storedNotionSource,
	found notionMergeFound,
) (string, notionMergeColumn, error) {
	into, _ := run.mergeTarget(source.ParentSourceID)
	column, err := run.mergeColumn(source.ParentSourceID, source)
	if err != nil || !column.Kept {
		return "", column, err
	}
	nativeID, apart, err := run.targetColumnApart(app, jobID, column, into, source.ParentSourceID, found)
	// A formula or rollup computing otherwise than the target's of its name
	// comes beside it (notion_migration_merge_computed.go).
	if err == nil && nativeID == "" && apart {
		column.Name, err = run.apartName(app, jobID, column.Name, source.ParentSourceID)
	}
	return nativeID, column, err
}

// targetColumn is what the first column a property stands for became in a
// target database (targetColumnApart).
func (run *notionMigrationRunPlan) targetColumn(
	app core.App, jobID string, column notionMergeColumn, into, from string, found notionMergeFound,
) (string, error) {
	nativeID, _, err := run.targetColumnApart(app, jobID, column, into, from, found)
	return nativeID, err
}

// notionMergeFound gives what a candidate a property stands for became in
// the target, by the candidate's key in the ledger — its own source, or for
// a split's column there '<property>:split:<data source>' — "" for one not
// made yet; own says it is one of the target's own properties.
type notionMergeFound func(candidate storedNotionSource, key string, own bool) (string, error)

// targetColumnApart is what the first column a property stands for became in
// a target database — among its own properties, the columns the recommended
// structure's database has, then those of the other databases merged into
// it or splitting rows into it (notionMergeAlongside) — by the rule the plan
// judges it by too (notionMergeStandsFor); found gives what a match became,
// "" for one not made yet, and "" for none. Apart says a formula or rollup
// comes beside the candidates of its name computing otherwise, as a column
// of its own.
func (run *notionMigrationRunPlan) targetColumnApart(
	app core.App, jobID string, column notionMergeColumn, into, from string, found notionMergeFound,
) (string, bool, error) {
	target, err := run.stagedTarget(app, jobID, into)
	if err != nil || target == "" {
		return "", false, err
	}
	parents, splitting := []string{target}, map[string]bool{}
	for _, beside := range notionMergeAlongside(run.merges, run.splits, into, from) {
		if staged, err := run.stagedTarget(app, jobID, beside.Key); err != nil {
			return "", false, err
		} else if staged != "" {
			parents = append(parents, staged)
			splitting[staged] = beside.Split
		}
	}
	encoded, err := json.Marshal(parents)
	if err != nil {
		return "", false, err
	}
	staged := []storedNotionSource{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND [[parent_source_id]] IN (SELECT [[value]] FROM json_each({:parents}))
		ORDER BY [[parent_source_id]]={:target} DESC, [[sequence]]
	`).Bind(dbx.Params{"jobId": jobID, "parents": string(encoded), "target": target}).All(&staged); err != nil {
		return "", false, err
	}
	candidates := make([]notionMergeCandidate, 0, len(staged))
	for _, candidate := range staged {
		other, err := run.mergeColumn(candidate.ParentSourceID, candidate)
		if err != nil {
			return "", false, err
		}
		candidates = append(candidates, notionMergeCandidate{column: other, own: candidate.ParentSourceID == target})
	}
	standsFor, apart, err := notionMergeStandsFor(column, candidates, func(index int) (bool, error) {
		if column.staged.SourceID == "" {
			return true, nil
		}
		return run.sameComputed(app, jobID, column.staged, staged[index], target)
	})
	if err != nil || apart {
		return "", apart, err
	}
	made := ""
	for _, index := range standsFor {
		key := staged[index].SourceID
		// A split's column is in the ledger by the data source the target
		// became (notion_migration_split.go); none before the target is made.
		if splitting[staged[index].ParentSourceID] {
			if made == "" {
				if made, err = notionImportMappedID(app, jobID, target, "data_source"); err != nil {
					return "", false, err
				}
			}
			if made == "" {
				continue
			}
			key = notionSplitColumnSource(key, made)
		}
		if nativeID, err := found(staged[index], key, candidates[index].own); err != nil || nativeID != "" {
			return nativeID, false, err
		}
	}
	return "", false, nil
}

// notionMergeCandidate is a column a merged database's property, or a
// split's rows', may stand for in its target: one of the target's own, or a
// property of another database merged into it or splitting rows into it.
type notionMergeCandidate struct {
	column notionMergeColumn
	own    bool
}

// notionMergeBeside is a database bringing columns into a target beside its
// own, by key: one merged into it, or one a split sends rows to it from.
type notionMergeBeside struct {
	Key   string
	Split bool
}

// notionMergeAlongside are the databases but from that bring columns into
// the target into — merged into it, or splitting rows into it: their
// properties are candidates after the target's own, as the run makes them
// there and the plan says so (2026-11-24: the plan's agenda judged by the
// target's own alone, and said nothing of the columns two merged databases
// computing otherwise kept beside each other; then a merged database's
// property saw none of the columns a split made in its target, nor did one
// split's see another's, so a split's rows made before the merge, or two
// splits into one target, gave the target two columns of each name).
func notionMergeAlongside(merges map[string]string, splits map[string][]notionMigrationPlanSplit, into, from string) []notionMergeBeside {
	into, from = notionMigrationSourceKey(into), notionMigrationSourceKey(from)
	beside := []notionMergeBeside{}
	for key, other := range merges {
		if notionMigrationSourceKey(other) == into && key != from {
			beside = append(beside, notionMergeBeside{Key: key})
		}
	}
	for key, rules := range splits {
		if key == from {
			continue
		}
		for _, rule := range rules {
			if notionMigrationSourceKey(rule.Into) == into {
				beside = append(beside, notionMergeBeside{Key: key, Split: true})
				break
			}
		}
	}
	slices.SortFunc(beside, func(a, b notionMergeBeside) int { return strings.Compare(a.Key, b.Key) })
	return beside
}

// notionMergeStandsFor is the one rule the run (targetColumnApart) and the
// plan (notion_migration_merge_computed_plan.go) judge a property by against
// its candidates in the target, the target's own first: it stands for each of
// the name (any case) and type the plan leaves both with, coming as the same
// type and relating to the same database — any title for a title — a
// formula or rollup only for one computing as it does (same); the run joins
// the first made. A formula or rollup none of the target's own stands for,
// with a candidate of its name and type computing otherwise, comes apart: a
// column of its own, '<name> (<its database>)', standing for no other — not
// even another merged database's computing as it does, so each column kept
// beside holds its own database's rows and the plan can name it before the
// run (2026-11-24: the first made took the others in, and a formula of
// theirs read it by the plain name no column had).
func notionMergeStandsFor(column notionMergeColumn, candidates []notionMergeCandidate, same func(int) (bool, error)) ([]int, bool, error) {
	if !column.Kept {
		return nil, false, nil
	}
	standsFor, apart, own := []int{}, false, false
	for index, candidate := range candidates {
		other := candidate.column
		if !other.Kept || other.Type != column.Type || other.Source != column.Source || other.Target != column.Target ||
			column.Type != graphValueTypeTitle && !strings.EqualFold(other.Name, column.Name) {
			continue
		}
		if workspaceExportComputedType(column.Type) {
			matches, err := same(index)
			if err != nil {
				return nil, false, err
			}
			if !matches {
				apart = true
				continue
			}
		}
		standsFor, own = append(standsFor, index), own || candidate.own
	}
	if apart && !own {
		return nil, true, nil
	}
	return standsFor, false, nil
}
