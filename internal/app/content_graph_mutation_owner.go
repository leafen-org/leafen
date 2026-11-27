package app

import (
	"context"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func (p *pocketBaseYgoPersistence) applyContentGraphMutation(
	principalID string,
	pageID string,
	input graphMutationInput,
	hooks contentGraphMutationHooks,
) (graphMutationReceipt, error) {
	return p.applyContentGraphMutationContext(
		context.Background(), principalID, pageID, input, hooks,
	)
}

func (p *pocketBaseYgoPersistence) applyContentGraphMutationContext(
	ctx context.Context,
	principalID string,
	pageID string,
	input graphMutationInput,
	hooks contentGraphMutationHooks,
) (graphMutationReceipt, error) {
	// A change of hundreds of blocks — an agent's, an import's turn — lets
	// people's edits go first (write_lane.go).
	if ctx != nil && len(input.Operations)+len(input.TextUpdates) >= writeLaneBulkOperations {
		defer writeInBulk(ctx)()
	}
	// A public connection's write records what it made and edited.
	hooks = withConnectionGraphAuthorship(ctx, pageID, input, hooks)
	requestHash, contentOwnerID := "", ""
	receipt := graphMutationReceipt{}
	interactionOrganizationID, interactionWorkspaceID, interactionActorID := "", "", ""
	createdAt := int64(0)
	runtimeWritablePropertyIDs := map[string]struct{}(nil)
	automationFacts := databaseAutomationMutationFacts{}
	rowNotifications := databaseRowNotificationPlan{}
	automationAuthorization := pageAuthorization{}
	automationEventsAdmitted := false
	childPageJobs := childPageBlockJobs{}
	addedRows := []string{}
	historyGraphDeltas := []pageHistoryGraphDelta{}
	var historyDatabases *pageHistoryDatabaseCapture
	var historyRows *pageHistoryRowCapture
	textUpdate, wordsStored, typingStored := "", ygoTextStored{}, ygoTextStored{}
	deletedTextBlockIDs := []string{}
	var pageReceipt *graphPageMutationReceipt
	mergedBlockContent := false
	var dateCommand *databaseDateCommand
	dependencyShifts, dependencyShiftsDenied, relatedChanges := 0, 0, 0
	dependencyShiftTasks, restoringDates := false, false
	relationPairWork, hidingWork := []databaseRelationPairWork(nil), []databaseRelationPairWork(nil)
	ownRows := map[string]struct{}(nil)
	sprintRowsMade, meetingRowsMade := []string(nil), []string(nil)
	// lockOnly is a write that only locked or unlocked inline databases.
	lockOnly := false

	// Whom it tells of itself as: Leafen for its own upkeep, a form's
	// respondent (content_graph_mutation_leafen_own.go).
	teller := contentGraphMutationTeller(principalID, hooks)

	err := p.mutateRoomDocumentContext(ctx, pageID, func(_ context.Context, txApp core.App, doc *crdt.Doc) (bool, error) {
		beginLeafenOwnChange(txApp, hooks)
		rememberWriteLookups(txApp)
		authorization := pageAuthorization{}
		var err error
		if hooks.Authorize != nil {
			authorization, err = hooks.Authorize(txApp)
		} else {
			authorization, err = authorizePageBody(txApp, principalID, pageID, permissionWorkspaceWrite)
			if isPermissionDenied(err) {
				// Held by an operation under way, not out of reach: it says so.
				if busy := pageBusyForMember(txApp, principalID, pageID); busy != "" {
					return false, pageBusyError(busy)
				}
				// A page they read is there: what stops them is that they only view it.
				if _, readErr := authorizePageBody(txApp, principalID, pageID, permissionWorkspaceRead); readErr == nil {
					return false, &graphRequestError{status: http.StatusForbidden, code: errorCodePermissionDenied, message: "You can only view this page."}
				}
				return false, graphNotFoundError()
			}
		}
		if err != nil {
			return false, err
		}
		if authorization.Page == nil || authorization.Page.Id != pageID {
			return false, graphNotFoundError()
		}
		if isDatabaseRowPageRecord(authorization.Page) &&
			(input.PageTitle != nil || !databaseRowPageBodyOperationsAllowed(input.Operations)) {
			return false, graphInputError(errorCodeGraphInvalid, "A database page body holds blocks only.")
		}
		if !hooks.AllowTrashedPage {
			if trashed, err := pageInTrashPath(txApp, pageID); err != nil {
				return false, err
			} else if trashed {
				return false, pageInTrashError()
			}
		}
		if hooks.PrepareInput != nil {
			input, err = hooks.PrepareInput(txApp, authorization, doc, input)
			if err != nil {
				return false, err
			}
		}
		// View updates merge with the stored view; the request is known by
		// what was sent, and a conflict is reported only when it is not a
		// replay of a change already made.
		requestedOperations := input.Operations
		resolvedOperations, viewConflict := resolveGraphViewUpdates(txApp, pageID, input.Operations)
		if viewConflict != nil && !isGraphViewConflict(viewConflict) {
			return false, viewConflict
		}
		if viewConflict == nil {
			input.Operations = resolvedOperations
		}
		if hooks.Prepare == nil {
			if err := validateContentGraphCompoundInput(&input); err != nil {
				return false, err
			}
		}
		if err := enforceDatabaseMutationDeletionBoundary(
			txApp, authorization.Workspace.OwnerID, pageID, input.Operations,
		); err != nil {
			return false, err
		}
		if !hooks.SkipRelationPairSync {
			if !hooks.KeepTrashedRowsInStep {
				if err := enforceDatabaseRowTrashBoundary(txApp, input.Operations); err != nil {
					return false, err
				}
			}
			if err := enforceDatabasePropertyTrashBoundary(txApp, input.Operations); err != nil {
				return false, err
			}
		}
		automationAuthorization = authorization
		leased, leaseErr := workspaceMarkdownReplacePageLeased(
			txApp, pageID, hooks.WorkspaceImportReplaceJobID,
		)
		if leaseErr != nil {
			return false, leaseErr
		}
		if leased {
			return false, graphConflictError(
				errorCodeWorkspaceImportTargetNotFresh,
				"Other changes can't be applied while the page content is being replaced.",
			)
		}
		contentOwnerID = authorization.Workspace.OwnerID
		interactionOrganizationID = authorization.Workspace.Organization.OrganizationID
		interactionWorkspaceID = authorization.Workspace.WorkspaceID
		if !hooks.WorkspaceImportReplaceRollback {
			interactionActorID = principalID
			if interactionActorID == "" && hooks.FormResponse {
				interactionActorID = hooks.FormRespondent
			}
		}
		if hooks.BeforeApply != nil {
			if err := hooks.BeforeApply(txApp, authorization, doc); err != nil {
				return false, err
			}
		}
		if hooks.ResolveRequestHash != nil {
			requestHash, err = hooks.ResolveRequestHash(txApp, authorization, doc)
			if err != nil {
				return false, err
			}
			if !sha256Pattern.MatchString(requestHash) {
				return false, graphInputError(errorCodeGraphInvalid, "Check the content change details.")
			}
		} else {
			if hooks.Prepare != nil {
				prepared, prepareErr := hooks.Prepare(txApp, authorization, doc)
				if prepareErr != nil {
					return false, prepareErr
				}
				if prepared.Deferred {
					return false, graphInputError(errorCodeGraphInvalid, "Check the content change details.")
				}
				input.Operations = prepared.Operations
				runtimeWritablePropertyIDs = prepared.RuntimeWritablePropertyIDs
			}
			requested := input
			if hooks.Prepare == nil {
				requested.Operations = requestedOperations
			}
			requestHash, err = contentGraphCompoundRequestHash(requested)
			if err != nil {
				return false, err
			}
		}

		replayed, found, err := storedGraphMutationReplay(txApp, contentOwnerID, pageID, input.MutationID, requestHash)
		if err != nil || found {
			receipt = replayed
			return false, err
		}
		if viewConflict != nil {
			return false, viewConflict
		}
		// The typing from before the change is stored before it (content_graph_mutation_words.go).
		if typingStored, err = storeGraphMutationTyping(ctx, txApp, principalID, authorization, hooks.Typing); err != nil {
			return false, err
		}
		if hooks.ResolveRequestHash != nil && hooks.Prepare != nil {
			prepared, prepareErr := hooks.Prepare(txApp, authorization, doc)
			if prepareErr != nil {
				return false, prepareErr
			}
			if prepared.Deferred {
				if len(prepared.Operations) != 0 || len(prepared.RuntimeWritablePropertyIDs) != 0 {
					return false, graphInputError(errorCodeGraphInvalid, "Check the content change details.")
				}
				return false, nil
			}
			input.Operations = prepared.Operations
			runtimeWritablePropertyIDs = prepared.RuntimeWritablePropertyIDs
		}
		input.Operations, restoringDates, err = restoreDatabaseDateCommand(
			txApp, contentOwnerID, pageID, principalID, hooks.DateCommandRowID, input.Operations,
		)
		if err != nil {
			return false, err
		}
		if restoringDates && (input.PageTitle != nil || input.PageKind != nil || len(input.TextUpdates) > 0) {
			return false, graphRefused(errorCodeGraphInvalid, "Check the content change details.",
				"a restoreDateCommand goes without pageTitle, pageKind or textUpdates")
		}
		beforeContentMerge := input.Operations
		input.Operations, err = resolveGraphBlockContentUpdates(doc, input.Operations)
		if err != nil {
			return false, err
		}
		mergedBlockContent = graphBlockContentWasMerged(beforeContentMerge, input.Operations)
		// A full-page database's title and name change together.
		if !hooks.WorkspaceImportReplaceRollback && hooks.WorkspaceImportReplaceJobID == "" {
			if input, err = alignFullPageDatabaseName(ctx, txApp, contentOwnerID, authorization, principalID, input); err != nil {
				return false, err
			}
		}
		if hooks.EnforcePageLock {
			if err := refusePageLockedContent(txApp, contentOwnerID, authorization.Page, principalID, input.Operations); err != nil {
				return false, err
			}
		}
		// What the databases' locks, property access rules, their unlinked
		// items' sharing and People's own values keep this person from
		// changing (database_row_value_write_access.go).
		if input.Operations, err = refusePersonRowValueWrites(txApp, principalID, hooks, input.Operations); err != nil {
			return false, err
		}
		if !restoringDates && !hooks.WorkspaceImportReplaceRollback && !hooks.SuppressAutomationEvents && principalID != "" {
			asked := len(input.Operations)
			var denied []graphOperation
			input.Operations, denied, err = expandDatabaseDependencyDateShifts(txApp, pageID, contentOwnerID, input.Operations,
				rowValueWritable(txApp, principalID, hooks.EnforcePageLock))
			if err != nil {
				return false, err
			}
			// The rows it moved besides those asked for, and those it left
			// because this person may not change them, which the writer's
			// screen tells, offering to undo the moves, as Notion's notice
			// does, by what they are.
			dependencyShifts, dependencyShiftsDenied = len(input.Operations)-asked, len(denied)
			reached := append(append([]graphOperation{}, input.Operations[asked:]...), denied...)
			if dependencyShiftTasks, err = databaseDependencyShiftsAreTasks(txApp, reached); err != nil {
				return false, err
			}
		}
		if !hooks.WorkspaceImportReplaceRollback {
			if err := enforceOrganizationDLPContent(
				txApp, authorization.Workspace.Organization.OrganizationID,
				input.Operations, input.PageTitle, input.TextUpdates,
			); err != nil {
				return false, err
			}
		} else if hooks.WorkspaceImportReplaceJobID == "" {
			return false, graphInputError(errorCodeGraphInvalid, "Check the content change details.")
		}

		page := authorization.Page
		if err := assertContentGraphFences(txApp, contentOwnerID, pageID, page, input); err != nil {
			return false, err
		}
		if input.PageKind != nil {
			if err := keepDatabaseStartWriting(txApp, doc, page, input); err != nil {
				return false, err
			}
			if err := changePageKind(txApp, authorization, input); err != nil {
				return false, err
			}
			// The new kind comes back with the page's new cursor.
			pageReceipt = contentGraphPageMutationReceipt(authorization.Page)
		}
		keptClosedTargets := 0
		if !hooks.SkipRelationPairSync && !hooks.WorkspaceImportReplaceRollback {
			if err := keepTrashedRelationTargets(txApp, input.Operations); err != nil {
				return false, err
			}
			// And the rows the writer can't open (database_relation_closed_targets.go).
			if !hooks.PreserveRowEditStamps && !hooks.MirrorsRelationPair {
				if keptClosedTargets, err = keepClosedRelationTargets(
					ctx, txApp, principalID, authorization.Page.GetString("workspace"), input.Operations,
				); err != nil {
					return false, err
				}
			}
		}
		linkedSources, linkedProperties, goneLinks, err := authorizeLinkedDatabaseReferencesKeepingGone(
			txApp, principalID, authorization, pageID, input.Operations,
		)
		if err != nil {
			return false, err
		}
		if !hooks.SkipRelationPairSync && !hooks.WorkspaceImportReplaceRollback {
			asked := len(input.Operations)
			// The rows the change names itself; those its pairs reach besides
			// keep when and by whom they were last edited (relationMirrorRows).
			if ownRows, err = graphOperationOwnRows(txApp, contentOwnerID, input.Operations); err != nil {
				return false, err
			}
			if input.Operations, err = normalizeDatabaseDependencies(txApp, contentOwnerID, pageID, input.Operations); err != nil {
				return false, err
			}
			// A person's nesting in a wiki's views is its Sub-pages' (wiki_subpage_pair.go).
			if hooks.PersonChange {
				if input.Operations, err = keepWikiPlacesOfPersonWrites(txApp, contentOwnerID, input.Operations); err != nil {
					return false, err
				}
			}
			if input.Operations, err = normalizeDatabaseSubitemParents(txApp, contentOwnerID, input.Operations); err != nil {
				return false, err
			}
			var oneWay int
			if input.Operations, oneWay, err = mirrorDatabaseRelationPairs(txApp, contentOwnerID, input.Operations); err != nil {
				return false, err
			}
			// The other side of a relation pair or dependency changed with
			// it and a relation made one-way, which the writer's screen reads
			// again.
			relatedChanges = max(0, len(input.Operations)-asked) + oneWay
		}
		relationRows, droppedTargets, err := authorizeRelationTargetReferences(
			txApp, principalID, authorization, pageID, input.Operations, linkedSources,
			hooks.PreserveRowEditStamps || hooks.WorkspaceImportReplaceRollback,
		)
		if err != nil {
			return false, err
		}
		// A value that lost rows deleted for good, or kept rows its writer
		// can't open, reads again on the writer's screen.
		relatedChanges += droppedTargets + keptClosedTargets
		if !hooks.WorkspaceImportReplaceRollback {
			// A new row's number is the server's, which the writer's screen reads again
			// (2026-11-05: a new sprint's ID stayed empty in its side peek until a reload).
			var numbered int
			input.Operations, runtimeWritablePropertyIDs, numbered, err = assignDatabaseRowUniqueIDs(
				txApp, contentOwnerID, pageID, input.Operations, runtimeWritablePropertyIDs,
			)
			if err != nil {
				return false, err
			}
			relatedChanges += numbered
		}
		// A database's new Verification property comes with its Owner, as Notion's.
		var linkVerification func(core.App) error
		if (hooks.PersonChange || hooks.LinkVerificationProperties) && principalID != "" && !hooks.WorkspaceImportReplaceRollback {
			asked := len(input.Operations)
			if input.Operations, linkVerification, err = planDatabaseVerificationProperties(ctx, txApp, principalID, input.Operations); err != nil {
				return false, err
			}
			// A new Verification's Owner came with it, which the writer's screen reads again.
			relatedChanges += max(0, len(input.Operations)-asked)
		}
		// A new property stays out of the tables that set what they show (database_new_property_hiding.go).
		if !hooks.WorkspaceImportReplaceRollback && hooks.WorkspaceImportReplaceJobID == "" {
			if input.Operations, hidingWork, err = planDatabaseNewPropertyHiding(txApp, contentOwnerID, pageID, input.Operations); err != nil {
				return false, err
			}
		}
		counts, err := validateGraphMutationWithAllReferenceCachesMode(
			txApp, contentOwnerID, page.GetString("workspace"), pageID, input.Operations,
			runtimeWritablePropertyIDs, linkedSources, nil, linkedProperties, nil, relationRows,
			hooks.AllowBlockUpdateBeforeDelete, hooks.StoredBlocks,
		)
		if err != nil {
			return false, err
		}
		goneLinks.restore(input.Operations)
		if !hooks.WorkspaceImportReplaceRollback {
			if input.Operations, err = distinguishDatabasePropertyNames(ctx, txApp, pageID, input.Operations); err != nil {
				return false, err
			}
			rewritten := 0
			input.Operations, rewritten, err = expandDatabaseFormulaReferenceRenames(txApp, pageID, input.Operations)
			if err != nil {
				return false, err
			}
			counts.Properties += rewritten
		}
		if !hooks.WorkspaceImportReplaceRollback && hooks.WorkspaceImportReplaceJobID == "" && !hooks.RemoveWiki {
			if err := guardWikiDatabaseOperations(txApp, contentOwnerID, pageID, input.Operations); err != nil {
				return false, err
			}
			if err := guardDatabaseSchemaOperations(txApp, page.GetString("workspace"), pageID, input.Operations); err != nil {
				return false, err
			}
			if sprintRowsMade, meetingRowsMade, err = prepareDatabaseRowsMade(txApp, pageID, input.Operations, hooks.FormResponse); err != nil {
				return false, err
			}
		}
		if !hooks.WorkspaceImportReplaceRollback && hooks.WorkspaceImportReplaceJobID == "" && principalID != "" {
			ownersFilled, err := prepareWikiRowValues(txApp, principalID, input.Operations)
			if err != nil {
				return false, err
			}
			// A new page's Owner, which the writer's screen reads again.
			relatedChanges += ownersFilled
			// An inline database locked or unlocked is its updates' (page_lock_records.go).
			if lockOnly, err = recordDatabaseBlockLockChanges(txApp, contentOwnerID, pageID, principalID, input.Operations); err != nil {
				return false, err
			}
		}
		if !hooks.WorkspaceImportReplaceRollback && !hooks.SuppressAutomationEvents && (principalID != "" || hooks.FormResponse) {
			automationFacts, err = prepareDatabaseAutomationEvents(
				txApp, authorization, input.MutationID, input.Operations, hooks.AutomationWrite,
			)
			if err != nil {
				return false, err
			}
			if !hooks.SuppressNotifications && hooks.WorkspaceImportReplaceJobID == "" {
				rowNotifications, err = prepareDatabaseRowNotifications(
					txApp, authorization, teller, input.MutationID, input.Operations,
				)
				if err != nil {
					return false, err
				}
			}
		}
		if err := hooks.beforeOperations(txApp, authorization, doc, input.Operations); err != nil {
			return false, err
		}
		childPages := childPageBlockPlan{}
		if hooks.PersonChange {
			childPages, err = prepareChildPageBlocks(
				txApp, principalID, authorization, doc, input.MutationID, input.Operations,
			)
			if err != nil {
				return false, err
			}
		}
		if !hooks.SkipRelationPairSync && !hooks.WorkspaceImportReplaceRollback && !hooks.RelationPairsAsWritten {
			relationPairWork, err = planDatabaseRelationPairWork(txApp, contentOwnerID, input.Operations)
			if err != nil {
				return false, err
			}
		}
		if err := ensurePageHistoryBaseline(txApp, pageID); err != nil {
			return false, err
		}
		deletedTextBlockIDs, err = contentGraphDeletedTextBlockIDs(doc, input.Operations)
		if err != nil {
			return false, err
		}
		historyCapture, err := beginPageHistoryGraphCapture(doc, input.Operations)
		if err != nil {
			return false, err
		}
		if historyDatabases, err = beginPageHistoryDatabaseCapture(txApp, contentOwnerID, pageID); err != nil {
			return false, err
		}
		if historyRows, err = beginPageHistoryRowCapture(txApp, pageID, input.Operations, hooks.HistoryRows...); err != nil {
			return false, err
		}
		if !restoringDates && !hooks.SuppressAutomationEvents && !hooks.WorkspaceImportReplaceRollback {
			if dateCommand, err = captureDatabaseDateCommand(txApp, pageID, historyRows, principalID, hooks.DateCommandRowID, input.Operations); err != nil {
				return false, err
			}
		}
		roomChange, err := beginGraphRoomChange(txApp, contentOwnerID, pageID, input.Operations, hooks)
		if err != nil {
			return false, err
		}
		deleted, err := applyStructuralGraphMutation(
			txApp, doc, contentOwnerID, page.GetString("workspace"), pageID,
			principalID, input.Operations,
			hooks.StructuralAppend, hooks.ExplicitStartBlockIDs,
		)
		if err != nil {
			return false, err
		}
		historyGraphDeltas, err = historyCapture.finish(txApp, pageID)
		if err != nil {
			return false, err
		}
		counts.Blocks += deleted
		childPageJobs, err = childPages.finish(txApp, principalID, authorization, doc, input.MutationID)
		if err != nil {
			return false, err
		}
		if err := applyDatabaseGraphMutationWithOptions(
			txApp, doc, contentOwnerID, page.GetString("workspace"), pageID,
			principalID, input.Operations, nil,
			databaseRecordMutationOptions{
				PreserveRowEditStamps: hooks.PreserveRowEditStamps, PreserveEditStampsOf: relationMirrorRows(ownRows, input.Operations),
				SettingsAsAsked: hooks.SettingsAsAsked, CommentMarks: hooks.CommentMarks, LimitViews: hooks.WorkspaceImportReplaceJobID == "" && !hooks.WorkspaceImportReplaceRollback,
				RowVersionsBy: leafenOwnRowVersionsBy(hooks),
			},
		); err != nil {
			return false, err
		}
		if err := deleteOrphanedLinkedDatabaseViews(
			txApp, doc, contentOwnerID, page.GetString("workspace"), pageID,
			principalID, input.Operations,
		); err != nil {
			return false, err
		}
		if err := deleteOrphanedDashboardWidgets(
			txApp, doc, contentOwnerID, page.GetString("workspace"), pageID,
			principalID, input.Operations,
		); err != nil {
			return false, err
		}
		textUpdate, err = applyContentGraphTextUpdates(
			txApp, pageID, principalID, input.TextUpdates, hooks.LiftedTextBlockIDs,
			deletedTextBlockIDs, hooks.TextPieces, hooks.ReplaceExistingText, hooks.TextSourceCopies,
		)
		if err != nil {
			return false, err
		}
		if hooks.ApplyAdditionalText != nil {
			if textUpdate != "" {
				return false, graphConflictError(
					errorCodeGraphTextTargetConflict,
					"One change can't use both text recovery paths.",
				)
			}
			textUpdate, err = hooks.ApplyAdditionalText(txApp, authorization)
			if err != nil {
				return false, err
			}
		}
		if wordsStored, err = storeGraphMutationWords(ctx, txApp, principalID, authorization, hooks.Words); err != nil {
			return false, err
		}
		if hooks.PersonChange {
			if err := childPages.settleAnchors(
				txApp, principalID, authorization, input.MutationID, input.TextUpdates, hooks.TextPieces,
			); err != nil {
				return false, err
			}
		}
		if titled, err := applyContentGraphPageTitle(txApp, authorization, principalID, input); err != nil {
			return false, err
		} else if titled != nil {
			pageReceipt = titled
		}
		if linkVerification != nil {
			if err := linkVerification(txApp); err != nil {
				return false, err
			}
		}
		if hooks.AfterApply != nil {
			if err := hooks.AfterApply(txApp, authorization, doc); err != nil {
				return false, err
			}
		}
		if err := giveDatabaseRowsMadeIcons(txApp, sprintRowsMade, meetingRowsMade); err != nil {
			return false, err
		}
		if err := seedDatabaseSprintRowsMade(ctx, txApp, pageID, principalID, sprintRowsMade); err != nil {
			return false, err
		}
		// Search makes the blocks it named again, not the whole page; a title
		// set with them marked the page's fields alone.
		if blocks, named := graphWriteSearchBlocks(input, hooks); named {
			searchContext := ctx
			if searchContext == nil {
				searchContext = context.Background()
			}
			if err := markSearchBlocksDirtyContext(searchContext, txApp, pageID, blocks); err != nil {
				return false, err
			}
		} else if err := markSearchDirty(txApp, page); err != nil {
			return false, err
		}
		if err := markDatabaseRowsSearchDirty(
			ctx, txApp, authorization.Workspace.WorkspaceID, graphMutationRowIDs(input.Operations),
		); err != nil {
			return false, err
		}
		if err := markSearchInlineDatabaseNamesDirty(txApp, input.Operations); err != nil {
			return false, err
		}
		cursor, err := nextGraphMutationCursor(txApp, contentOwnerID, pageID)
		if err != nil {
			return false, err
		}
		conversions, err := pendingDatabasePropertyConversions(txApp, pageID, input.Operations)
		if err != nil {
			return false, err
		}
		receipt = graphMutationReceipt{
			MutationID: input.MutationID, Cursor: cursor, Counts: counts,
			TextUpdate: textUpdate, Page: pageReceipt, PropertyConversions: conversions,
			MergedBlockContent: mergedBlockContent, DependencyShifts: dependencyShifts, RelatedChanges: relatedChanges,
			DependencyShiftTasks: dependencyShiftTasks, DependencyShiftsDenied: dependencyShiftsDenied, WordsStored: len(hooks.Words)+len(hooks.Typing) > 0,
		}
		if err := roomChange.tell(txApp, contentOwnerID, pageID, cursor, hooks.Words, textUpdate); err != nil {
			return false, err
		}
		if dateCommand != nil {
			receipt.DateCommand = input.MutationID
		}
		createdAt = time.Now().UTC().UnixMilli()
		return true, nil
	}, func(operationContext context.Context, txApp core.App, update, stateVector []byte) error {
		return contentGraphMutationRecord{
			pageID: pageID, principalID: principalID, teller: teller, contentOwnerID: contentOwnerID, requestHash: requestHash,
			input: input, hooks: hooks, receipt: &receipt, dateCommand: dateCommand, sprintRowsMade: sprintRowsMade,
			historyDatabases: historyDatabases, historyRows: historyRows, historyGraphDeltas: historyGraphDeltas, createdAt: createdAt,
			interactionActorID: interactionActorID, interactionOrganizationID: interactionOrganizationID, interactionWorkspaceID: interactionWorkspaceID,
			automationAuthorization: automationAuthorization, automationFacts: automationFacts, rowNotifications: rowNotifications,
			relationPairWork: append(relationPairWork, hidingWork...), lockOnly: lockOnly,
			automationEventsAdmitted: &automationEventsAdmitted, addedRows: &addedRows,
		}.persist(operationContext, txApp, update, stateVector)
	})
	if err == nil {
		p.settleStoredText(pageID, typingStored.and(wordsStored))
	}
	if err == nil && len(childPageJobs.moves) > 0 {
		p.settleChildPageBlockMoves(ctx, childPageJobs.moves)
	} else if err == nil && (childPageJobs.copies || automationEventsAdmitted) {
		p.notifyJobsAfterCommit()
	}
	// Pages made in a wiki's page become the wiki's, and a wiki's pages
	// follow their links the change took or put in (page_block_trash.go).
	err = p.settleChildPageWikiJobs(ctx, principalID, pageID, childPageJobs, err)
	// A page a person adds to a wiki's view sits in the body above it too.
	if err == nil && len(addedRows) > 0 {
		if linkErr := p.keepWikiPageLinks(ctx, principalID, addedRows); linkErr != nil {
			p.app.Logger().Warn("leafen wiki page link not kept", "page", pageID, "error", linkErr)
		}
	}
	return receipt, err
}
