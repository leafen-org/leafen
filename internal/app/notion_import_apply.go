package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type storedNotionSource struct {
	Sequence       int64  `db:"sequence"`
	SourceID       string `db:"source_id"`
	Kind           string `db:"kind"`
	ParentSourceID string `db:"parent_source_id"`
	ParentKind     string `db:"parent_kind"`
	Position       int    `db:"position"`
	Title          string `db:"title"`
	Text           string `db:"text"`
}

func (r *durableJobRunner) applyDiscoveredNotionChunk(job storedJob, payload notionImportJobPayload) error {
	return r.applyDiscoveredNotionChunkIn(context.Background(), job, payload)
}

// applyDiscoveredNotionChunkIn makes a turn of what the job read; ctx is the
// runner's, which a stopping server ends: the steps that ask Notion within
// it (a migration's comments) stop there (notion_migration_comment_asks.go).
func (r *durableJobRunner) applyDiscoveredNotionChunkIn(ctx context.Context, job storedJob, payload notionImportJobPayload) error {
	if payload.StampingRows {
		// The pages take their Notion history before the rows take theirs.
		if payload.PageStampCursor != notionImportPagesStamped {
			return r.stampNotionImportPagesChunk(job, payload)
		}
		return r.stampNotionImportRowsChunk(job, payload)
	}
	if payload.Restructuring {
		return r.restructureNotionImportChunk(ctx, job, payload)
	}
	if payload.Publishing {
		return r.publishNotionImportChunk(job, payload)
	}
	if payload.Linking {
		// One the graph refuses is left out, and linking goes on next turn.
		err := r.linkNotionImportChunk(job, payload)
		if refused := (&notionImportItemRefusedError{}); errors.As(err, &refused) {
			_, _, err = r.leaveNotionImportItemOut(job, payload, refused)
		}
		return err
	}
	return r.makeNotionImportWindows(job, payload)
}

// makeNotionImportWindow makes up to items of what the job read, in one
// transaction (notion_import_turn_hold.go).
func (r *durableJobRunner) makeNotionImportWindow(job storedJob, payload notionImportJobPayload, items int) (notionImportWindowMade, error) {
	cursor, err := parseNotionApplyCursor(job.Cursor)
	if err != nil {
		return notionImportWindowMade{}, err
	}
	made := notionImportWindowMade{}
	// The source being made, which a failure names (notion_import_left_out.go).
	var making *storedNotionSource
	err = r.app.RunInTransaction(func(txApp core.App) error {
		start := time.Now()
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		authorization, err := authorizeNotionImportTurn(txApp, current, payload)
		if err != nil {
			return err
		}
		// A resumed run first makes what it left out behind where it stopped
		// (notion_import_late.go).
		window, late, err := loadNotionImportWindow(txApp, current.ID, payload, cursor, items)
		if err != nil {
			return err
		}
		// What the run left out is left out of the window before anything of
		// it is read (notion_import_left_out.go).
		all, leftOut := window.Sources, newNotionImportLeftOut(payload)
		sources, err := leftOut.window(txApp, current.ID, all)
		if err != nil {
			return err
		}
		hasMore := window.CandidateCount > len(window.Sources)
		runPlan, err := loadNotionMigrationRunPlan(txApp, current.Owner, payload)
		if err != nil {
			return err
		}
		// Formulas and rollups the plan preserves come as their shown text.
		sources = runPlan.keepShownAsText(sources)
		merged, err := notionMergeMembers(txApp, current.ID, runPlan, sources)
		if err != nil {
			return err
		}
		applyMappings, err := loadNotionApplyMappingsSkipping(txApp, current.ID, sources, leftOut.or(runPlan.linkedLater),
			func(source storedNotionSource) bool {
				membership, member := merged[source.Sequence]
				return payload.ApplyingMerges != member || notionMergedDatabaseItself(source, membership, member)
			})
		if err != nil {
			return err
		}
		dataSourcePages, err := loadNotionApplyDataSourcePages(txApp, sources, applyMappings)
		if err != nil {
			return err
		}
		propertyReferences, err := loadNotionApplyPropertyReferences(txApp, sources, applyMappings)
		if err != nil {
			return err
		}
		rowIdentities, err := loadNotionApplyRowIdentities(txApp, sources, applyMappings)
		if err != nil {
			return err
		}
		pageCollection, err := loadNotionApplyPageCollection(txApp, sources, applyMappings)
		if err != nil {
			return err
		}
		pageCursors, err := reserveNotionApplyPageCursors(txApp, payload.AuthorityOwnerID, sources, applyMappings)
		if err != nil {
			return err
		}
		topParentID, err := runPlan.destination(txApp, current)
		if err != nil {
			return err
		}
		documents := map[string]*notionYgoDocument{}
		structuralDocuments := map[string]*structuralMutationBatch{}
		createdInteractions := []pageInteraction{}
		createdAt := time.Now().UTC().UnixMilli()
		pages, blocks, databases, rows := 0, 0, 0, 0
		filesCopied, filesOmitted := 0, 0
		// A migration's run counts what it made, changed and left as it was
		// (notion_migration_run_again.go); the import's report what it made.
		tally := notionMigrationTally{}
		for _, source := range sources {
			making = &source
			source.Title = runPlan.title(source)
			if kind := notionMigrationLiveKind(source.Kind); kind != "" {
				noteNotionMigrationLive(payload.MigrationID, notionMigrationStageCreating, kind, source.Title)
			}
			// A run again counts only what it makes.
			made := true
			kind := notionSourceMappingKind(source.Kind)
			if kind != "" {
				_, existed := applyMappings.nativeID(source.SourceID, kind)
				made = !existed
			}
			// What a merged database holds is made in its target, in a second
			// pass once the target and all else are made; the second pass
			// makes only that.
			membership, member := merged[source.Sequence]
			switch {
			case member && notionMergeWrapper(source):
				continue
			case payload.ApplyingMerges != member:
				continue
			case member && membership.Split:
				if source.Kind == "row" {
					handled, err := applyNotionSplitRow(
						txApp, current, payload, runPlan, source, structuralDocuments, applyMappings,
						dataSourcePages, propertyReferences, rowIdentities,
					)
					if err != nil {
						return err
					}
					if handled {
						if made {
							rows++
						}
						tally.count(source, made, false)
						continue
					}
				}
			case member:
				if err := aliasNotionMergedDatabase(
					txApp, current, payload, runPlan, membership.Database, applyMappings, dataSourcePages,
				); err != nil {
					return err
				}
				switch {
				case source.Kind == "database" && source.ParentKind != notionImportPropertyIssueParentKind:
					// Counted as moved into its target, as the report counts it.
					tally.count(source, made, false)
					continue
				case source.Kind == "property" && runPlan.linkedLater(source.SourceID):
					// Its objects' relation is made in the link phase.
					continue
				case source.Kind == "property":
					if source, err = runPlan.planned(txApp, current.ID, source); err != nil {
						return err
					}
					if err := applyNotionMergedProperty(
						txApp, current, payload, runPlan, source, structuralDocuments, applyMappings, dataSourcePages, propertyReferences,
					); err != nil {
						return err
					}
					continue
				}
			}
			switch source.Kind {
			case "page":
				changed, err := applyNotionPage(
					txApp, current, payload, source, pageCollection, applyMappings,
					pageCursors, authorization.Organization.OrganizationID,
					createdAt, &createdInteractions, structuralDocuments, topParentID,
				)
				if err != nil {
					return err
				}
				if made {
					pages++
				}
				tally.count(source, made, changed)
			case "paragraph", "template", "block":
				changed, err := applyNotionStagedTextBlock(
					txApp, current, payload, source, documents, structuralDocuments, applyMappings,
				)
				if err != nil {
					return err
				}
				if made {
					blocks++
				}
				tally.count(source, made, changed)
			case "issue":
				// Reported by the import; nothing to apply.
			case "view":
				// Made once every property exists, in the link phase.
			case graphBlockTypeTable, graphBlockTypeTableRow, graphBlockTypeBookmark, graphBlockTypeLinkPreview:
				changed, err := applyNotionContentBlock(
					txApp, current, payload, source, structuralDocuments, applyMappings,
				)
				if err != nil {
					return err
				}
				if made {
					blocks++
				}
				tally.count(source, made, changed)
			case "database":
				if source.ParentKind == notionImportPropertyIssueParentKind {
					// Another data source of a database of several.
					if err := applyNotionDataSource(
						txApp, current, payload, source, structuralDocuments, applyMappings, dataSourcePages,
					); err != nil {
						return err
					}
					continue
				}
				renamed, err := notionMigrationDatabaseRenamed(txApp, source, applyMappings, func() error {
					return applyNotionDatabase(
						txApp, current, payload, source, structuralDocuments, applyMappings, dataSourcePages,
					)
				})
				if err != nil {
					return err
				}
				if made {
					blocks++
					databases++
				}
				tally.count(source, made, renamed)
			case "property":
				// One the plan turns into objects is a relation the link phase makes.
				if runPlan.linkedLater(source.SourceID) {
					continue
				}
				// Options the plan gives one name are one, and a formula reads the run's columns (notion_migration_formula_reads.go).
				if source, err = runPlan.planned(txApp, current.ID, source); err != nil {
					return err
				}
				_, err := applyNotionDatabasePropertyMapping(
					txApp, current, payload, source, structuralDocuments, applyMappings,
					dataSourcePages, propertyReferences,
				)
				if err != nil {
					return err
				}
				// Rows made before it take their cells of it (notion_import_late.go).
				if made {
					if err := notionImportLateProperty(txApp, &payload, late, source); err != nil {
						return err
					}
				}
			case "row":
				changed, err := applyNotionDatabaseRow(
					txApp, current, payload, runPlan, source, structuralDocuments, applyMappings,
					dataSourcePages, propertyReferences, rowIdentities, runPlan.rowPlace(txApp, current.ID),
				)
				if err != nil {
					return err
				}
				if made {
					rows++
				}
				tally.count(source, made, changed)
			case graphBlockTypeFile, graphBlockTypePDF, graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio:
				// A page, database or row took its icon, cover or file already.
				if notionImportOwnedFileSource(source.SourceID) {
					task, err := loadNotionFileTask(txApp, current.ID, source.SourceID)
					// One an earlier run brought has no task in this one.
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					if err != nil {
						return err
					}
					if task.Status == "copied" {
						filesCopied++
					} else if task.Status == "omitted" {
						filesOmitted++
					}
					continue
				}
				created, omitted, changed, err := applyNotionFile(txApp, current, payload, source, structuralDocuments, applyMappings)
				if err != nil {
					return err
				}
				if created || !made {
					tally.count(source, made, changed)
				}
				if created {
					blocks++
					metadata, decodeErr := decodeNotionFileSourceMetadata(source.Text)
					if decodeErr != nil {
						return decodeErr
					}
					if metadata.ExternalURL == "" {
						filesCopied++
					}
				}
				if omitted {
					filesOmitted++
				}
			default:
				return errors.New(errorCodeNotionImportCreateFailed)
			}
		}
		making = nil
		payload.LeftOut = leftOut.list()
		// The blocks go into a page's history before the text written in them.
		if err := saveStructuralMutationBatches(txApp, current.Owner, structuralDocuments); err != nil {
			return err
		}
		if err := saveNotionYgoDocuments(txApp, current.Owner, documents); err != nil {
			return err
		}
		if err := recordNotionAdminLegalHoldPageInteraction(
			txApp, createdInteractions...,
		); err != nil {
			return err
		}
		if err := addNotionMigrationRunTally(txApp, current.ID, payload, tally); err != nil {
			return err
		}
		if err := applyMappings.flush(txApp, current.ID); err != nil {
			return err
		}
		late = notionImportLateWindowDone(&payload, late, all, hasMore)
		if len(all) != 0 && !late {
			current.Cursor = strconv.FormatInt(all[len(all)-1].Sequence, 10)
			if !payload.ApplyingMerges {
				current.Processed += len(all)
			}
		}
		switch {
		case late || len(all) != 0 && hasMore:
			made.more = true
		case runPlan.hasSecondPass() && !payload.ApplyingMerges:
			// Again from the start, for what waited for its merge target.
			payload.ApplyingMerges, current.Cursor = true, ""
		default:
			// Columns made for a property elsewhere follow Notion's names (notion_migration_rerun_column_names.go).
			if err := followNotionMigrationColumnNames(txApp, current, payload, runPlan); err != nil {
				return err
			}
			// Relations and rollups are linked before the pages are
			// published, when the import brought any.
			linking, err := notionImportHasLinks(txApp, current.ID, payload.MigrationID, runPlan)
			if err != nil {
				return err
			}
			payload.ApplyingMerges, payload.Linking, payload.Publishing = false, linking, !linking
		}
		made.held = time.Since(start)
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		chunkIndex := current.ChunkCount + 1
		if err := recordNotionImportChunk(txApp, current.ID, chunkIndex, len(all), current.Cursor, notionApplyDigest(all), now); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_reports}}
			SET [[pages]] = [[pages]] + {:pages}, [[blocks]] = [[blocks]] + {:blocks},
				[[databases]] = [[databases]] + {:databases}, [[rows]] = [[rows]] + {:rows},
				[[files_copied]] = [[files_copied]] + {:filesCopied},
				[[files_omitted]] = [[files_omitted]] + {:filesOmitted}, [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{
			"jobId": current.ID, "pages": pages, "blocks": blocks,
			"databases": databases, "rows": rows, "filesCopied": filesCopied,
			"filesOmitted": filesOmitted, "updated": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[cursor]] = {:cursor}, [[processed_items]] = {:processed},
				[[chunk_count]] = {:chunkCount}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(encoded), "cursor": current.Cursor,
			"processed": current.Processed, "chunkCount": chunkIndex,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		made.count, made.cursor, made.payload = len(all), current.Cursor, payload
		return err
	})
	if err != nil {
		return notionImportWindowMade{}, notionImportItemAt(making, err)
	}
	return made, nil
}

func applyNotionSourceText(
	app core.App,
	pageID string,
	blockID string,
	encoded string,
	documents map[string]*notionYgoDocument,
) error {
	document, err := loadNotionYgoDocument(app, pageID, documents)
	if err != nil {
		return err
	}
	plan, err := decodeNotionParagraphSourceText(encoded)
	if err != nil {
		return err
	}
	// One delta writes the spans in turn, each with only its own formatting;
	// inserting them one at a time put each inside the formatting of the
	// span before it.
	delta := make([]crdt.Delta, 0, len(plan.Spans))
	for _, span := range plan.Spans {
		var insert any = span.Text
		attributes := crdt.Attributes(nil)
		if len(span.Attributes) > 0 {
			attributes = crdt.Attributes(span.Attributes)
		}
		// An empty piece is its embed (graph_text_empty_pieces.go).
		if kind, _ := attributes["emptyPiece"].(string); kind != "" && span.Text == "" {
			attributes = maps.Clone(attributes)
			delete(attributes, "emptyPiece")
			insert = graphTextEmptyPieceEmbed(kind)
		} else if span.Text == "" {
			continue
		}
		delta = append(delta, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: insert, Attributes: attributes})
	}
	document.doc.Transact(func(txn *crdt.Transaction) {
		txn.GetText(ygoBlockTextRootPrefix+blockID).ApplyDelta(txn, graphTextDeltaPieced(delta))
	})
	return nil
}

func parseNotionApplyCursor(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 0 {
		return 0, errors.New(errorCodeNotionImportCreateFailed)
	}
	return cursor, nil
}

func notionApplyDigest(sources []storedNotionSource) string {
	hasher := sha256.New()
	for _, source := range sources {
		_, _ = fmt.Fprintf(hasher, "%d\x00%s\x00%s\n", source.Sequence, source.SourceID, source.Kind)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
