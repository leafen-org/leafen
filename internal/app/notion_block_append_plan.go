package app

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
)

const notionBlockAppendMaximumTotal = 1000

// notionBlockRequestMaximumDepth is how deep a request's blocks may carry
// children, as Notion's API takes them (measured 2026-11-08 on page create
// and block append): top-level blocks and their children may hold children,
// their grandchildren may not — a toggle's table takes its rows, a toggle in
// a toggle takes a line, a third toggle down is refused.
const notionBlockRequestMaximumDepth = 2

type notionBlockAppendPlan struct {
	Operations       []graphOperation
	TextUpdates      []graphTextUpdate
	ResponseBlockIDs []string
	ExplicitStartIDs map[string]struct{}
	// ContentFaults are the faults in content Notion would make, in the
	// request's order, which it says together (notion_block_content_faults.go).
	ContentFaults []string
	// TableWidths are the widths of the tables rows go under: the request's
	// own, and the one blocks are appended to, which its caller gives.
	TableWidths map[string]int
	// ContentCheck, when its caller gives one, is a block's fault in content
	// that only the workspace can tell, said with the others, or "".
	ContentCheck func(block *graphBlock) string
}

// notionTableRowCellsRefused is Notion's refusal of a table row whose cells
// aren't as many as its table is wide (seen 2026-11-10): a new row's a fault
// in its content, a changed row's said alone, with a full stop.
const notionTableRowCellsRefused = "A table row has as many cells as its table is wide"

// notionTableWithoutWidthRefused is Notion's refusal of a row appended to a
// table without width, one its Markdown made without rows (seen 2026-11-14),
// said before its cells are counted.
const notionTableWithoutWidthRefused = "The table has no width yet: a table is at least 1 wide"

// notionStoredTableWidth is the width a table's content keeps.
func notionStoredTableWidth(content json.RawMessage) (int, bool) {
	stored := map[string]any{}
	if json.Unmarshal(content, &stored) != nil {
		return 0, false
	}
	return notionIntegerContentValue(stored["tableWidth"])
}

// notionTableRowCellCount is how many cells a table row's request gives, or
// -1 where it gives none or not as a list.
func notionTableRowCellCount(payload json.RawMessage) int {
	content := struct {
		Cells *[]json.RawMessage `json:"cells"`
	}{}
	if json.Unmarshal(payload, &content) != nil || content.Cells == nil {
		return -1
	}
	return len(*content.Cells)
}

// appendRequestBlocks plans children under a parent of parentType after
// afterID, refusing them as Notion's API does (notionBlockRequestRefusal,
// then the faults in their content together).
func (plan *notionBlockAppendPlan) appendRequestBlocks(
	children []json.RawMessage,
	parentID string,
	parentType string,
	afterID *string,
) error {
	if refusal := notionBlockRequestRefusal(children, parentType); refusal != "" {
		return errors.New(refusal)
	}
	for index, raw := range children {
		blockID, err := plan.appendRequestBlock(
			raw, parentID, parentType, afterID, 0, notionBlockRequestMaximumDepth, index,
		)
		if err != nil {
			return err
		}
		plan.ResponseBlockIDs = append(plan.ResponseBlockIDs, blockID)
		afterID = &plan.ResponseBlockIDs[len(plan.ResponseBlockIDs)-1]
	}
	if len(plan.ContentFaults) > 0 {
		return errors.New(notionContentCreationFailed(plan.ContentFaults...))
	}
	return nil
}

func (plan *notionBlockAppendPlan) appendRequestBlock(
	raw json.RawMessage,
	parentID string,
	parentType string,
	afterID *string,
	depth int,
	maximumDepth int,
	index int,
) (string, error) {
	blockType, payload, err := notionBlockRequestValue(raw)
	if err != nil {
		return "", err
	}
	if parentType == graphBlockTypeTemplate {
		return "", errors.New("template block blueprints cannot be changed through block append")
	}
	// A block Notion's API can't make is a fault in the content, said with
	// the request's others; nothing under it is planned.
	if fault := notionUncreatableBlockFault(blockType); fault != "" {
		plan.ContentFaults = append(plan.ContentFaults, string(fault))
		return "", nil
	}
	// A column list, a tab and a table take only their items, refused as
	// Notion's API refuses others (seen 2026-11-14), in Leafen's words.
	for parent, item := range map[string]string{
		graphBlockTypeColumnList: graphBlockTypeColumn, graphBlockTypeTab: graphBlockTypeParagraph,
		graphBlockTypeTable: graphBlockTypeTableRow,
	} {
		if parentType == parent && blockType != item {
			return "", errors.New("A " + parent + " block takes only " + item + " blocks as its children")
		}
	}
	if width, known := plan.TableWidths[parentID]; known && blockType == graphBlockTypeTableRow {
		if width < minGraphTableWidth {
			plan.ContentFaults = append(plan.ContentFaults, notionTableWithoutWidthRefused)
			return "", nil
		}
		if cells := notionTableRowCellCount(payload); cells >= 0 && cells != width {
			plan.ContentFaults = append(plan.ContentFaults, notionTableRowCellsRefused)
			return "", nil
		}
	}
	if blockType == graphBlockTypeColumnList && depth != 0 {
		return "", errors.New("column_list blocks must be request root blocks")
	}
	if blockType == graphBlockTypeColumnList {
		maximumDepth = 3
	}
	if blockType == graphBlockTypeTab {
		maximumDepth = 2
	}
	if blockType == graphBlockTypeColumn && depth == 0 {
		maximumDepth = 2
	}
	content := map[string]json.RawMessage{}
	if json.Unmarshal(payload, &content) != nil || content == nil {
		return "", errors.New("block content is invalid")
	}
	// Children may be none, as Notion's API takes them (seen 2026-11-10: a
	// column's, a paragraph's, a toggle's, a synced block's); a tab, a table
	// and a column list hold their least in the request check.
	children := []json.RawMessage{}
	if rawChildren, nested := content["children"]; nested {
		if json.Unmarshal(rawChildren, &children) != nil || len(children) > 100 {
			return "", errors.New("nested children must contain at most 100 blocks")
		}
		if len(children) > 0 && depth >= maximumDepth {
			return "", errors.New("nested children exceed the supported request depth")
		}
		delete(content, "children")
	}
	if blockType == graphBlockTypeColumnList {
		if len(children) < 2 {
			return "", errors.New("column_list blocks require at least two column children")
		}
	}
	if blockType == graphBlockTypeTab && len(children) == 0 {
		return "", errors.New("tab blocks require at least one paragraph child")
	}
	if blockType == graphBlockTypeTable && len(children) == 0 {
		return "", errors.New("table blocks require at least one table_row child")
	}
	if blockType == graphBlockTypeTableRow && len(children) != 0 {
		return "", errors.New("table_row blocks cannot contain children")
	}
	cleanPayload, err := json.Marshal(content)
	if err != nil {
		return "", errors.New("block content is invalid")
	}
	blockID := uuid.NewString()
	parsed, err := parseNotionBlockMutation(
		blockID, parentID, "notion-"+strconv.Itoa(depth)+"-"+strconv.Itoa(index),
		blockType, cleanPayload, nil,
	)
	if fault := notionContentFault(""); errors.As(err, &fault) {
		plan.ContentFaults = append(plan.ContentFaults, string(fault))
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if plan.ContentCheck != nil {
		if fault := plan.ContentCheck(parsed.Block); fault != "" {
			plan.ContentFaults = append(plan.ContentFaults, fault)
			return "", nil
		}
	}
	parsed.Block.AfterID = afterID
	if parsed.Block.Type == graphBlockTypeCallout {
		// Its text is its own, its children its blocks (notion_block_callout_line.go).
		parsed.Block.Content = withCalloutText(parsed.Block.Content, calloutTextOwn)
	}
	if blockType == graphBlockTypeTable {
		if width, ok := notionStoredTableWidth(parsed.Block.Content); ok {
			if plan.TableWidths == nil {
				plan.TableWidths = map[string]int{}
			}
			plan.TableWidths[blockID] = width
		}
	}
	plan.Operations = append(plan.Operations, graphOperation{
		Kind: graphOperationKindUpsertBlock, Block: parsed.Block,
	})
	if len(plan.Operations) > notionBlockAppendMaximumTotal {
		return "", errors.New("nested block request is too large")
	}
	if parsed.TextSpans != nil {
		plan.TextUpdates = append(plan.TextUpdates, graphTextUpdate{
			BlockID: blockID, Spans: *parsed.TextSpans,
		})
	}
	var childAfterID *string
	for childIndex, child := range children {
		childID, err := plan.appendRequestBlock(
			child, blockID, blockType, childAfterID, depth+1, maximumDepth, childIndex,
		)
		if err != nil {
			return "", err
		}
		childAfterID = &childID
	}
	return blockID, nil
}
