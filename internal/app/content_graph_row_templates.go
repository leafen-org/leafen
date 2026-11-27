package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A row made on a device first (web lib/rowMadeHere.ts) starts from a
// template — its database's default (the sidebar's "+", "내 작업"), a view's,
// the one "새로 만들기" was chosen with — and the change that makes it says
// so (rowTemplates), kept with it on the device until it goes. The server
// fills the row from that template as it makes it, in the same request:
// the values and icon the row leaves empty, and its body. Filled by a request
// of its own from the page that made the row, once the row was saved, a row
// whose making went later — its tab reloaded or closed first, the device
// offline — was made with an empty title and no icon or body, its fill gone
// with the page (2026-11-25).
//
// A body the device copied from the template itself and shows at once
// (rowTemplateHere.ts) comes along as its blocks and their words, written as
// the row page's own change from the person, as the route takes one; the
// template then fills the rest without its body.
//
// A template chosen on the empty body of such a row's page before the
// server has the row goes the same way (web lib/rowTemplateChosenHere.ts): a
// change of the row kept behind its making in the outbox names it, and the
// server applies it as it takes that change, as it applies one chosen for a
// row it has (database_row_template_apply.go). Waiting for the row in the
// page's memory, the choice was lost to a reload or a closed tab
// (2026-11-25). So a fill goes with the change that names it, whether that
// change makes its row or writes one made before; it is filled once — the
// same change sent again is answered as made, not made again
// (receipt.Duplicate).
//
// A view's own default named before the device read its templates
// (orDefault) stands for its database's default once it is gone, as the view
// names its default once they are read (web viewDefaultTemplate.ts): a row
// made then, in a view whose own default was deleted, named the deleted one
// and was left unfilled, the server not knowing the view (2026-11-25). A
// template named alone — one a person picked — that is gone fills nothing.

// maxRowTemplateTimeZoneBytes bounds a fill's time zone, as the template route's.
const maxRowTemplateTimeZoneBytes = 64

// takeRowTemplateFills reads the templates the rows a change writes start
// from or were chosen for, and takes them off the input, which the rest knows without them.
// Each names a row the change writes (upsertRow), once; a body copied on the
// device names the template it was copied from.
func takeRowTemplateFills(input *graphMutationInput) ([]graphRowTemplateFill, map[string]string, error) {
	fills := input.RowTemplates
	input.RowTemplates = nil
	if len(fills) == 0 {
		return nil, nil, nil
	}
	rows := map[string]string{}
	for _, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			rows[operation.Row.ID] = operation.Row.DataSourceID
		}
	}
	refused := func(reason string) error {
		return graphRefused(errorCodeGraphInvalid, "Check the content change details.", "%s", reason)
	}
	seen := map[string]struct{}{}
	for index := range fills {
		fill := &fills[index]
		fill.RowID, fill.TemplateID, fill.TimeZone = strings.TrimSpace(fill.RowID), strings.TrimSpace(fill.TemplateID), strings.TrimSpace(fill.TimeZone)
		if _, written := rows[fill.RowID]; !written {
			return nil, nil, refused("rowTemplates[].rowId names a row this change writes with upsertRow")
		}
		if _, twice := seen[fill.RowID]; twice {
			return nil, nil, refused("rowTemplates names each row once")
		}
		seen[fill.RowID] = struct{}{}
		if len(fill.TemplateID) > maxGraphIdentifierLength || len(fill.TimeZone) > maxRowTemplateTimeZoneBytes {
			return nil, nil, refused("rowTemplates[].templateId is one of the database's templates and timeZone an IANA name")
		}
		if (len(fill.Blocks) > 0 || fill.Words != "") && fill.TemplateID == "" {
			return nil, nil, refused("rowTemplates[].blocks are the body of the template templateId names")
		}
		if len(fill.Blocks) > maxGraphMutationOperations {
			return nil, nil, refused("rowTemplates[].blocks hold at most as many blocks as one change")
		}
	}
	return fills, rows, nil
}

// fillRowsFromTemplates fills each row the change names from the template
// it names, or its database's default when it names none. The rows are
// written whatever becomes of a fill, which is told in the log.
func (p *pocketBaseYgoPersistence) fillRowsFromTemplates(
	event *core.RequestEvent, principalID, mutationID string, fills []graphRowTemplateFill, dataSources map[string]string,
) {
	for _, fill := range fills {
		if err := p.fillRowMadeFromTemplate(event, principalID, mutationID, fill, dataSources[fill.RowID]); err != nil {
			event.App.Logger().Warn("leafen row made here not filled from its template", "row", fill.RowID, "error", err)
		}
	}
}

func (p *pocketBaseYgoPersistence) fillRowMadeFromTemplate(
	event *core.RequestEvent, principalID, mutationID string, fill graphRowTemplateFill, dataSourceID string,
) error {
	templateID := fill.TemplateID
	if templateID == "" {
		var err error
		if templateID, err = databaseDefaultTemplateID(event.App, dataSourceID); err != nil || templateID == "" {
			return err
		}
	}
	bodyHere := len(fill.Blocks) > 0
	err := p.fillDatabaseRowFromTemplate(event.Request.Context(), event.App, principalID, fill.RowID, templateID, fill.TimeZone, bodyHere)
	// A view's default gone since gives way to its database's, if it has one;
	// a body copied here stays as the person saw it (below).
	if fill.OrDefault && !bodyHere && errors.Is(err, errDatabaseRowTemplateNotFound) {
		defaultID, defaultErr := databaseDefaultTemplateID(event.App, dataSourceID)
		if defaultErr != nil || defaultID == "" {
			return defaultErr
		}
		err = p.fillDatabaseRowFromTemplate(event.Request.Context(), event.App, principalID, fill.RowID, defaultID, fill.TimeZone, false)
	}
	// A template gone since the device copied it leaves the body the person saw.
	if err != nil && !(bodyHere && errors.Is(err, errDatabaseRowTemplateNotFound)) {
		return err
	}
	if !bodyHere {
		return nil
	}
	return errors.Join(err, p.writeRowBodyMadeHere(event, principalID, mutationID, fill))
}

// writeRowBodyMadeHere writes the body a device copied from a template into
// the row it made, its blocks and their words, as the row page's own change
// from the person, taken as the route takes a page's change
// (createContentGraphMutation); its mutation is the row's, so it is written once.
func (p *pocketBaseYgoPersistence) writeRowBodyMadeHere(event *core.RequestEvent, principalID, mutationID string, fill graphRowTemplateFill) error {
	input := graphMutationInput{
		MutationID: "row-template-body:" + derivedPageID("row-template-body", mutationID, fill.RowID),
		Operations: make([]graphOperation, 0, len(fill.Blocks)),
		Words:      fill.Words,
	}
	for index := range fill.Blocks {
		input.Operations = append(input.Operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &fill.Blocks[index]})
	}
	words, _, err := takeGraphMutationText(&input)
	if err != nil {
		return err
	}
	lifted, err := liftGraphBlockStoredText(&input)
	if err != nil {
		return err
	}
	if err := validateContentGraphCompoundInput(&input); err != nil {
		return err
	}
	restoreGraphWriteAsStored(p.app, &input)
	appGraphWriteInMadeNames(p.app, event.Get(userKeyAuthorityKey), &input)
	_, err = p.applyContentGraphMutationContext(event.Request.Context(), principalID, fill.RowID, input,
		contentGraphMutationHooks{EnforcePageLock: true, PersonChange: true, LiftedTextBlockIDs: lifted, SearchFromNamedBlocks: true,
			StructuralAppend: true, Words: words, TellRoom: true})
	return err
}
