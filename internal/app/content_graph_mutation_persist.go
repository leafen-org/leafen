package app

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// contentGraphMutationRecord is what applying a page's content graph
// mutation (content_graph_mutation_owner.go) leaves for persisting it in the
// same transaction: its receipt, the page's edit, its notices, the work it
// sets off and its history and updates. Persisting it says whether it set off
// automations and which rows a person added, for the mutation to settle after
// it commits.
type contentGraphMutationRecord struct {
	pageID, principalID, teller, contentOwnerID, requestHash              string
	input                                                                 graphMutationInput
	hooks                                                                 contentGraphMutationHooks
	receipt                                                               *graphMutationReceipt
	dateCommand                                                           *databaseDateCommand
	sprintRowsMade                                                        []string
	historyDatabases                                                      *pageHistoryDatabaseCapture
	historyRows                                                           *pageHistoryRowCapture
	historyGraphDeltas                                                    []pageHistoryGraphDelta
	createdAt                                                             int64
	interactionActorID, interactionOrganizationID, interactionWorkspaceID string
	automationAuthorization                                               pageAuthorization
	automationFacts                                                       databaseAutomationMutationFacts
	rowNotifications                                                      databaseRowNotificationPlan
	relationPairWork                                                      []databaseRelationPairWork
	lockOnly                                                              bool
	automationEventsAdmitted                                              *bool
	addedRows                                                             *[]string
}

func (m contentGraphMutationRecord) persist(operationContext context.Context, txApp core.App, update, stateVector []byte) error {
	m.receipt.StateVector = base64.StdEncoding.EncodeToString(stateVector)
	err := storeGraphMutationReceipt(
		operationContext, txApp, m.contentOwnerID, m.pageID, m.requestHash, *m.receipt, m.dateCommand,
		graphChangeParts(m.input, m.hooks, *m.receipt, m.sprintRowsMade, m.historyDatabases, m.historyGraphDeltas), m.createdAt,
	)
	if err != nil {
		return err
	}
	// The change edits the page where Notion's last edited time counts it.
	if err := noteGraphPageEdited(
		operationContext, txApp, m.pageID, m.principalID, *m.receipt, m.input.Operations, m.historyRows, m.hooks,
	); err != nil {
		return err
	}
	// Leafen's own upkeep is no page interaction; its version is Leafen's
	// (m.teller), and no app hears of it (content_graph_mutation_leafen_own.go).
	if m.interactionActorID != "" && !m.hooks.LeafenOwn {
		if err := recordNotionAdminLegalHoldPageInteraction(txApp, pageEditedInteraction(
			"content-graph", m.pageID+"\x00"+m.interactionActorID+"\x00"+m.input.MutationID,
			m.interactionOrganizationID, m.interactionWorkspaceID,
			m.pageID, m.interactionActorID, m.createdAt,
		)); err != nil {
			return err
		}
	}
	*m.automationEventsAdmitted, err = admitDatabaseAutomationEvents(
		txApp, m.automationAuthorization,
		m.principalID, m.input.MutationID, m.requestHash, m.automationFacts,
	)
	if err != nil {
		return err
	}
	if err := deliverDatabaseRowNotifications(operationContext, txApp, m.rowNotifications, m.createdAt); err != nil {
		return err
	}
	if err := admitDatabaseRowFollowerFanouts(txApp, m.rowNotifications, m.createdAt); err != nil {
		return err
	}
	if err := admitDatabaseRelationPairWork(
		txApp, m.contentOwnerID, m.interactionWorkspaceID, m.principalID, m.pageID+":"+m.input.MutationID, m.relationPairWork,
	); err != nil {
		return err
	}
	if m.interactionActorID != "" {
		if err := reconcileDatabaseDateReminders(
			operationContext, txApp, m.interactionWorkspaceID, m.pageID, m.interactionActorID,
			m.input.Operations, time.UnixMilli(m.createdAt),
		); err != nil {
			return err
		}
		if err := reconcileDatabaseDeadlines(
			operationContext, txApp, m.interactionWorkspaceID, m.pageID, m.interactionActorID,
			m.input.Operations, time.UnixMilli(m.createdAt),
		); err != nil {
			return err
		}
	}
	if err := insertStructuralGraphChangeContext(
		operationContext,
		txApp, m.contentOwnerID, m.pageID, m.input.MutationID, m.requestHash,
		m.receipt.Cursor, update, m.createdAt,
	); err != nil {
		return err
	}
	publishGraphWordsAfterCommit(txApp, m.pageID, m.receipt.Cursor, m.hooks.Words, m.receipt.TextUpdate != "")
	databases, err := m.historyDatabases.finish(txApp)
	if err != nil {
		return err
	}
	rows, err := m.historyRows.finish(txApp)
	if err != nil {
		return err
	}
	for _, change := range rows {
		if m.hooks.PersonChange && change.Change == pageHistoryChangeRowAdded {
			*m.addedRows = append(*m.addedRows, change.ID)
		}
	}
	// A page a person adds to a wiki goes last under the page above it (wiki_page_links.go).
	if err := placeAddedWikiPagesLast(txApp, *m.addedRows); err != nil {
		return err
	}
	rows = append(rows, m.hooks.HistoryRows...)
	return recordPageHistoryGraphMutation(
		txApp, m.pageID, m.teller, update, m.historyGraphDeltas, databases, rows,
		workspaceActivityGraphContext{Databases: m.historyDatabases, Rows: m.historyRows, LockOnly: m.lockOnly, CommentMarks: m.hooks.CommentMarks,
			Maintenance: m.hooks.LeafenOwn, BecameDatabase: m.input.PageKind != nil && *m.input.PageKind == pageKindDatabase, PagesMadeRows: m.hooks.PagesMadeRows, WikiHome: m.hooks.WikiHomeMade},
	)
}
