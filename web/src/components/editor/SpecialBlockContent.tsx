import { compareGraphOrder } from "@/contentGraph";
import { useRef, useState, type CSSProperties, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";
import { api } from "../../api";
import type { GraphBlock, GraphTextSpan } from "@/contentGraph";
import { Plus } from "@/icons/leafen";
import { newId } from "@/lib/ids";
import { blockItemText, blockTypeLabel } from "./blockItemLabels";
import { EquationPreview } from "./EquationPreview";
import { InlineEquationEditor } from "./InlineEquationEditor";
import { useBlockSetupRequest } from "./blockSetup";
import { requestEditorBlockSelection } from "./editorCommands";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { SIMPLE_TABLE_MAX_WIDTH } from "./databaseSimpleTable";
import { ButtonBlock } from "./PageButtonBlock";
import { simpleTableColumnStyle, simpleTableColumnWidths, simpleTableWidthsAfterResize } from "./simpleTableColumns";
import { useSimpleTableColumnResize } from "./useSimpleTableColumnResize";
import { usePageWideSimpleTable } from "./usePageWideSimpleTable";
import { useSimpleTableAutoColumns } from "./useSimpleTableAutoColumns";
import { SimpleTableHandles } from "./SimpleTableHandles";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import emptyStyles from "./BlockEmptyPopover.module.css";
import { emptyRowMark } from "./emptyRowMarks";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// A math block shows its formula, or a grey "Add a TeX equation" row when
// empty, and is edited in a panel under it, as Notion's (seen 2026-10-16):
// a click opens it, so does making the block from the "/" menu; the block
// shows what is typed as it is typed.
function EquationBlock({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const expression = String(block.content.expression ?? "");
  const ref = useRef<HTMLDivElement>(null);
  const [editing, setEditing] = useState<PopoverRect | null>(null);
  const [typed, setTyped] = useState<string | null>(null);
  const open = () => {
    // The panel hangs from the formula's grey, as Notion's (seen 2026-11-01).
    const rect = (ref.current?.firstElementChild ?? ref.current)?.getBoundingClientRect();
    if (adapter.readOnly || !rect || editing) return;
    setEditing({ top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right, contextElement: ref.current ?? undefined });
  };
  useBlockSetupRequest(block.id, open);
  // Closed, the panel leaves the block selected, as Notion's (seen 2026-11-01),
  // with the blocks chosen with it when Enter opened it (2026-11-06).
  const close = () => {
    setEditing(null);
    setTyped(null);
    requestEditorBlockSelection(block.id, true);
  };
  const shown = typed ?? expression;
  // The formula's box is a button, as Notion's (seen 2026-11-01): a press on
  // it is the click's and leaves the blocks' selection as it was. Selecting
  // the block on the press redrew the formula under the pointer, and the
  // click that opens the panel was lost. The block's own 8px round the box,
  // on every side, is not the button's: a press there chooses the block and
  // a key typed at once goes nowhere, as Notion's (seen 2026-11-20 at 1440:
  // its button 8px in from its block, a press 4px over the block's foot, by
  // its side or under its top chose the block). The panel, drawn elsewhere,
  // keeps its presses from the block it hangs from.
  return <div ref={ref} className={styles.equationBlock} contentEditable={false} data-editing={editing ? "true" : undefined}
    data-readonly={adapter.readOnly ? "true" : undefined}
    onMouseDown={(event: MouseEvent<HTMLDivElement>) => { if (!event.currentTarget.contains(event.target as Node)) event.stopPropagation(); }}>
    <div className={styles.equationButton} onClick={open}
      {...(adapter.readOnly ? {} : { role: "button", tabIndex: 0, onMouseDown: (event: MouseEvent<HTMLDivElement>) => event.stopPropagation(),
        onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => {
          if (event.target !== event.currentTarget || !fieldEnter(event) && event.key !== " ") return;
          event.preventDefault();
          open();
        } })}>
      {/* Spaces alone are a formula of nothing, drawn blank, as Notion's (seen 2026-11-10, its API's "   "). */}
      {shown ? <EquationPreview className={styles.equationPreview} expression={shown} />
        // Notion's empty block row, as the empty media and link blocks (seen 2026-10-17), marked
        // with the TeX logo rather than the "/" menu's Σ (seen 2026-11-20).
        : <div className={`${emptyStyles.row} ${styles.equationEmpty}`}>
          <span className={emptyStyles.rowIcon} aria-hidden="true">{emptyRowMark("equation")}</span>
          <span className={emptyStyles.rowLabel}>{blockItemText("equation.empty")}</span>
        </div>}
    </div>
    {editing ? <InlineEquationEditor anchor={editing} initial={expression} block onPreview={setTyped}
      onDone={(tex) => {
        close();
        if (tex !== expression) adapter.updateBlock(block, { expression: tex });
      }}
      onCancel={close} /> : null}
  </div>;
}

function tableCellSpans(value: unknown): GraphTextSpan[] {
  if (!Array.isArray(value)) return [];
  return value.filter((span): span is GraphTextSpan => !!span && typeof span === "object" && typeof span.text === "string");
}

function orderedTableRows(blocks: readonly GraphBlock[], tableId: string) {
  const fallback = blocks
    .filter((candidate) => candidate.parentId === tableId && candidate.type === "table_row")
    .sort(compareGraphOrder);
  const remaining = new Map(fallback.map((row) => [row.id, row]));
  const ordered: GraphBlock[] = [];
  let afterId: string | null = null;
  while (remaining.size > 0) {
    const matches = [...remaining.values()].filter((row) => row.afterId === afterId);
    if (matches.length !== 1) return fallback;
    const row = matches[0];
    ordered.push(row);
    remaining.delete(row.id);
    afterId = row.id;
  }
  return ordered;
}

type StructuredTableProps = { adapter: NativeEditorGraphAdapter; block: GraphBlock; rowsContent: ReactNode };

// A table without width — Notion's Markdown makes one without rows, its rows
// then without cells — is its block's box alone, nothing drawn in it, as
// Notion's (seen 2026-11-14: 16px, its rows not drawn); its rows stay in the
// editor's document, unseen.
export function StructuredTableBlock(props: StructuredTableProps) {
  if (Number(props.block.content.tableWidth) >= 1) return <SimpleTableBlock {...props} />;
  return <div className={styles.simpleTableWrap} role="region" aria-label={blockTypeLabel("table")} data-structured-table
    data-table-without-width="true"><div hidden>{props.rowsContent}</div></div>;
}

function SimpleTableBlock({ adapter, block, rowsContent }: StructuredTableProps) {
  const width = Math.max(1, Math.min(SIMPLE_TABLE_MAX_WIDTH, Number(block.content.tableWidth) || 1));
  const rows = orderedTableRows(adapter.graph.blocks, block.id);
  const canvasRef = useRef<HTMLDivElement>(null);
  const scrollerRef = useRef<HTMLDivElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  usePageWideSimpleTable(scrollerRef);
  const columnWidths = simpleTableColumnWidths(block.content, width);
  const resize = useSimpleTableColumnResize({ canvasRef, enabled: !adapter.readOnly,
    onResize: (index, columnWidth) => adapter.updateBlock(block, { columnWidths: simpleTableWidthsAfterResize(columnWidths, index, columnWidth) }) });
  const shownWidths = resize.dragging ? columnWidths.map((current, index) => index === resize.dragging!.index ? resize.dragging!.width : current) : columnWidths;
  useSimpleTableAutoColumns(wrapRef, scrollerRef, shownWidths);

  function cellsFor(row: GraphBlock) {
    const raw = Array.isArray(row.content.cells) ? row.content.cells : [];
    return Array.from({ length: width }, (_, index) => tableCellSpans(raw[index]));
  }

  return <div ref={wrapRef} className={styles.simpleTableWrap} role="region" aria-label={blockTypeLabel("table")} data-structured-table
    style={simpleTableColumnStyle(shownWidths) as CSSProperties}>
    {/* Read-only, keys scroll it; editing, a press in a cell must give that cell the caret, not this box focus. */}
    <div ref={scrollerRef} className={styles.simpleTableScroller} tabIndex={adapter.readOnly ? 0 : undefined} aria-label={blockItemText("simpleTable.scrollRegion")}>
      <div ref={canvasRef} className={styles.simpleTableCanvas} {...resize.canvasProps}>
        {rowsContent}
        {resize.edge ? <div className={styles.simpleTableColumnEdge} contentEditable={false} aria-hidden="true"
          style={{ left: resize.edge.x - 1, top: resize.edge.top, height: resize.edge.height }} /> : null}
        <SimpleTableHandles adapter={adapter} block={block} rows={rows} width={width} canvasRef={canvasRef} cellsFor={cellsFor} />
      </div>
    </div>
    {/* Its header row and column switch from its block menu, as Notion's (seen 2026-10-11); its rows and columns from their handles. */}
  </div>;
}

function LegacyTemplateBlock({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const [executing, setExecuting] = useState(false);
  const [status, setStatus] = useState("");
  const title = adapter.text(block);
  const label = title || blockItemText("legacyTemplate.label");
  const unavailable = adapter.readOnly || executing || !adapter.session;

  async function useTemplate() {
    if (unavailable || !adapter.session) return;
    setExecuting(true);
    setStatus("");
    try {
      await api.executeLegacyTemplate(
        adapter.session,
        adapter.pageId,
        block.id,
        { executionId: newId() },
        adapter.signal,
      );
      if (!adapter.isCurrent()) return;
      const added = blockItemText("legacyTemplate.added");
      setStatus(added);
      adapter.moveContext?.onStatus?.(added, "success");
    } catch {
      if (!adapter.isCurrent() || adapter.signal.aborted) return;
      const failed = blockItemText("legacyTemplate.failed");
      setStatus(failed);
      adapter.moveContext?.onStatus?.(failed, "error");
    } finally {
      if (adapter.isCurrent()) setExecuting(false);
    }
  }

  return <div className={styles.legacyTemplateBlock} data-legacy-template-block aria-busy={executing}>
    <button type="button" className={styles.buttonAction} disabled={unavailable}
      aria-label={blockItemText("legacyTemplate.use", { label })} onClick={() => void useTemplate()}>
      <Plus size="small" aria-hidden="true" /><span>{executing ? blockItemText("legacyTemplate.adding") : label}</span>
    </button>
    {!adapter.readOnly ? <input className={styles.legacyTemplateTitle} value={title} placeholder={blockItemText("legacyTemplate.label")}
      aria-label={blockItemText("legacyTemplate.titleInput")} disabled={executing}
      onChange={(event) => adapter.changeText(block, event.currentTarget.value)} /> : null}
    <span className={styles.legacyTemplateStatus} role="status" aria-live="polite">{status}</span>
  </div>;
}

export function SpecialBlockContent({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  if (block.type === "equation") return <EquationBlock adapter={adapter} block={block} />;
  if (block.type === "button") return <ButtonBlock adapter={adapter} block={block} />;
  if (block.type === "template") return <LegacyTemplateBlock adapter={adapter} block={block} />;
  return <div className={styles.dividerWrap}><hr className={styles.divider} /></div>;
}
