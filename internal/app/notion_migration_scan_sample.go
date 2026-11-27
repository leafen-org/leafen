package app

import (
	"context"
)

// The scan reads no row's body (decision "설계 우선 Notion 이사" 3절 원칙 5:
// bodies are counted, not read out), yet a run reads every row's body block
// by block, and in a real move that was most of the time (2026-11-16: the
// scan took about 200 requests and the run read Notion for 35 minutes). So
// for each database's sample rows the scan lists the top of the body once:
// how many blocks it starts with and how many of them hold more, each a
// request of its own for the run. The run's time is reckoned from that
// sample (notion_migration_estimate.go). The top alone says nothing of what
// its blocks that hold more hold, which a run makes and asks for comments
// too (2026-11-20: a move reckoned 402 blocks to ask and asked 761, and its
// time left went from 2 minutes to 16 as the blocks began): so under each
// database's first sample row the scan lists what the first few of those
// hold, once (notionMigrationSampleInnerLists). Which blocks hold more is
// the run's own rule (notionImportReadsChildren); a row whose top has a plain
// heading Notion calls childless costs the run a read of the row's Markdown,
// and its plain headings a read each when that Markdown shows a heading
// holding blocks (notion_import_heading_children.go): the sample reads that
// Markdown too and counts both.

// notionMigrationRowSample is what a sampled row's body showed: the row, the
// blocks at its top (a page Notion gives at a time) and the requests beyond
// the first that reading all of it takes at least; and of its blocks that
// hold more, those listed, the blocks they hold and how many of those hold
// more again.
type notionMigrationRowSample struct {
	rows, blocks, nested, markdown  int
	inner, innerBlocks, innerNested int
}

// notionMigrationSampleInnerLists is how many blocks holding more the scan
// lists under a database's first sample row, each a request.
const notionMigrationSampleInnerLists = 2

// count adds a step's sample to the scan's counts.
func (sample notionMigrationRowSample) count(counts *notionMigrationCounts) {
	counts.SampledRows += sample.rows
	counts.SampledRowBlocks += sample.blocks
	counts.SampledRowNested += sample.nested
	counts.SampledRowMarkdown += sample.markdown
	counts.SampledInnerLists += sample.inner
	counts.SampledInnerBlocks += sample.innerBlocks
	counts.SampledInnerNested += sample.innerNested
}

// scanNotionMigrationItem reads one queued item of the scan.
func scanNotionMigrationItem(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	switch item.Kind {
	case "comments":
		return scanNotionMigrationComments(ctx, client, change, item)
	case "page":
		return scanNotionMigrationPage(ctx, client, store, change, item)
	case "blocks":
		return scanNotionMigrationBlocks(ctx, client, store.migrationID, change, item)
	case "database":
		return scanNotionMigrationDatabase(ctx, client, store, change, item)
	case "rows":
		return scanNotionMigrationRows(ctx, client, store, change, item)
	case "rowbody":
		return scanNotionMigrationRowBody(ctx, client, store.migrationID, change, item)
	case "rowinner":
		return scanNotionMigrationRowInner(ctx, client, store.migrationID, change, item)
	case "views":
		return scanNotionMigrationViews(ctx, client, store, change, item)
	case "source":
		return scanNotionMigrationSource(ctx, client, store, change, item)
	case "search":
		return scanNotionMigrationSearch(ctx, client, store, change, item)
	case "person":
		return scanNotionMigrationPerson(ctx, client, change, item)
	}
	return nil
}

// scanNotionMigrationRowBody lists the top of a sample row's body. A body
// Notion won't give is passed by: the row itself was read.
func scanNotionMigrationRowBody(
	ctx context.Context, client notionProviderClient, migrationID string, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	listed, err := client.children(ctx, item.ID, "")
	if notionProviderRefused(err) {
		return nil
	}
	if err != nil {
		return err
	}
	blocks, err := notionProviderPageResults(listed)
	if err != nil {
		return err
	}
	change.sample.rows++
	held, err := notionMigrationSampleHeadings(ctx, client, migrationID, change, item.ID, blocks, true)
	if err != nil {
		return err
	}
	for _, block := range blocks {
		change.sample.blocks++
		if notionImportReadsChildren(block) || held && notionProviderPlainHeadingNamedEmpty(block) {
			change.sample.nested++
			// What the first few hold, under a database's first sample row.
			if queued := len(change.queue); item.Depth == 1 && queued < notionMigrationSampleInnerLists {
				change.queue = append(change.queue, notionMigrationScanItem{Kind: "rowinner", ID: block.ID, PageID: item.ID})
			}
		}
	}
	// More than a page of blocks: at least a page more of them.
	if listed.HasMore {
		change.sample.blocks += notionImportProviderPageItems
		change.sample.nested++
	}
	return nil
}

// scanNotionMigrationRowInner lists what a sample row's block that holds
// more holds: its first page of blocks, and how many of those hold more.
func scanNotionMigrationRowInner(
	ctx context.Context, client notionProviderClient, migrationID string, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	listed, err := client.children(ctx, item.ID, "")
	if notionProviderRefused(err) {
		return nil
	}
	if err != nil {
		return err
	}
	blocks, err := notionProviderPageResults(listed)
	if err != nil {
		return err
	}
	change.sample.inner++
	held, err := notionMigrationSampleHeadings(ctx, client, migrationID, change, item.PageID, blocks, false)
	if err != nil {
		return err
	}
	for _, block := range blocks {
		change.sample.innerBlocks++
		if notionImportReadsChildren(block) || held && notionProviderPlainHeadingNamedEmpty(block) {
			change.sample.innerNested++
		}
	}
	if listed.HasMore {
		change.sample.innerBlocks += notionImportProviderPageItems
	}
	return nil
}

// notionMigrationSampleOf is a row's values as short text, for the agent and
// the person to see what each property holds.
func notionMigrationSampleOf(row notionProviderObject) notionMigrationSampleRow {
	sample := notionMigrationSampleRow{ID: row.ID, Values: map[string]string{}}
	for key, property := range row.Properties {
		sample.Values[key] = notionMigrationValueText(property)
	}
	return sample
}
