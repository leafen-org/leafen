import { useContext, useId, useSyncExternalStore } from "react";
import { NodeViewContent, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import type { GraphBlock } from "@/contentGraph";
import { bulletGlyph, quoteSize } from "@/lib/blockMarkerFormats";
import { ToggleCaretIcon } from "@/icons/leafen";
import { blockTextDirection, TEXT_DIRECTION_BLOCK_TYPES, wordsDirection, wordsNameDirection } from "@/lib/textDirection";
import { numberedListMarkerColumn, type NumberedListMarker } from "@/lib/numberedList";
import { CodeBlock } from "./CodeBlock";
import { CalloutIcon } from "./CalloutIcon";
import { blockItemText, blockTypePlaceholder } from "./blockItemLabels";
import { useCaretInsideNode } from "./nativeTiptapCaretInside";
import { useTellLineWay, useWordlessLineDirection } from "./nativeTiptapLineWay";
import { NativeTiptapReactContext, useNativeTiptapNode } from "./nativeTiptapReactContext";
import { lineEndingSuggestion, suggestedBreakOf } from "./suggestingInputModel";
import { useSuggestingMode } from "./suggestingMode";
import { TodoTaskLine } from "./TodoTaskChips";
import styles from "./editorStyles.generated.module.css";

/** Block chrome surrounds contentDOM; it never reads or replaces typed HTML. */
// Blocks that name themselves while empty wherever the caret is, as Notion's
// do (seen 2026-10-31; a toggle heading by its heading's name); an empty text
// line shows its hint only under the caret, so a line nested in one of these
// doesn't take its parent's. An empty code block shows none, as Notion's.
// An empty to-do shows its hint only on the caret's line, as a paragraph,
// while a list item, a heading, a quote or a toggle shows its own always
// (Notion's, seen 2026-11-08).
const PLACEHOLDER_ALWAYS = new Set(["heading_1", "heading_2", "heading_3", "heading_4", "toggle_heading_1", "toggle_heading_2",
  "toggle_heading_3", "toggle_heading_4", "bulleted_list_item", "numbered_list_item", "quote", "callout", "toggle"]);

export function NativeTiptapTextView(props: NodeViewProps) {
  const { adapter, graphIndex, block, controller, occurrenceId } = useNativeTiptapNode(props);
  const code = props.node.type.name === "nativeCode";
  const engineFocused = useCaretInsideNode(props, code);
  const suggesting = useSuggestingMode().active;
  const textId = useId();
  const direction = code ? "ltr" : block && TEXT_DIRECTION_BLOCK_TYPES.has(block.type) ? blockTextDirection(block.content) : undefined;
  // A line with no letter of its own reads as the line above it where the
  // app reads from the right, set as its `dir` (nativeTiptapLineWay.ts).
  const words = props.node.textContent;
  const wordless = useWordlessLineDirection(props.editor, props.getPos, direction === "auto" && !wordsNameDirection(words));
  // The way its words read, found as dir="auto" finds it, for the CSS that
  // sets its marks' sides and arrows (styles.css --inline-sign, a quote's
  // line), and for its children's side (NativeTiptapChildrenView).
  const reads = wordless ?? (direction === "auto" ? wordsDirection(words) : direction);
  useTellLineWay(props.editor, occurrenceId, block && TEXT_DIRECTION_BLOCK_TYPES.has(block.type) ? reads : undefined);
  if (!block) return <NodeViewWrapper><NodeViewContent /></NodeViewWrapper>;
  const suggestedBreak = suggestedBreakOf(adapter.graph, block, lineEndingSuggestion(
    props.node.lastChild?.marks.find(mark => mark.type.name === "nativeAnnotation")?.attrs.stringValue));
  // Code takes its wrapping from its block (off: one long line that scrolls),
  // not the pre-wrap Tiptap puts on every node view's text (2026-11-09: wrap
  // off still wrapped while edited, and showed through the resting copy).
  const editable = <NodeViewContent<"code" | "div"> as={code ? "code" : "div"} className={styles.editable}
    id={block.type === "to_do" ? textId : undefined}
    style={code ? { whiteSpace: "inherit" } : undefined}
    data-native-code={code ? "true" : undefined} data-native-text={code ? undefined : "true"}
    data-rt-editable="true" data-page-placeholder={block.id === adapter.pagePlaceholderBlockId ? "true" : undefined}
    data-placeholder-always={PLACEHOLDER_ALWAYS.has(block.type) ? "true" : undefined}
    // A text line's hint asks for "/", which a phone's line doesn't open (EditorText.module.css).
    data-slash-hint={block.type === "paragraph" ? "true" : undefined}
    data-empty={props.node.content.size === 0 ? "true" : "false"}
    // A block suggested after this one ends this one in a blue "⏎", one
    // suggestion with it, as Notion's (seen 2026-09-29), unless the line
    // ends in that suggestion's own text; settling the suggestion here takes
    // it away at the press (SettlingSuggestionMarks).
    data-suggested-break={suggestedBreak ? "true" : undefined}
    data-suggested-break-by={suggestedBreak?.id} data-suggested-split={suggestedBreak?.split ? "true" : undefined}
    // A block suggested in names itself and marks its list in the suggestion's blue (EditorInlineDecorations.module.css).
    data-suggested-block={typeof block.content.suggestionId === "string" && block.content.suggestionId ? "true" : undefined}
    // A line straight in blocks a button inserts asks for "/" to insert, as
    // Notion's (seen 2026-11-08: "/를 눌러 블록을 삽입하세요"), and so does any
    // empty text line under the caret while suggesting, where "/" lists the
    // text blocks it suggests (seen 2026-11-18, a page and a database page).
    // An empty to-do says what its "@" does, in English on a Korean screen
    // too (Notion's, seen 2026-11-08/09), where a mention can be made.
    data-placeholder={adapter.readOnly && !suggesting ? undefined : block.id === adapter.pagePlaceholderBlockId && !suggesting
      ? blockItemText("block.pagePlaceholder")
      : block.type === "paragraph" && (suggesting || block.parentId && graphIndex.byId.get(block.parentId)?.type === "template")
        ? blockItemText("block.insertPlaceholder")
        : block.type === "to_do" && adapter.mentionSnapshot ? blockItemText("todo.assignPlaceholder") : blockTypePlaceholder(block.type)} />;
  const toggle = block.type === "toggle" || block.type.startsWith("toggle_heading_");
  // A toggle with nothing in it, open or closed, draws its caret half clear
  // (EditorText .toggleCaret[data-empty]).
  const emptyToggle = toggle && !graphIndex.children.get(block.id)?.length;
  // A callout's icon stands at the frame's start and only its text finds its
  // direction, as Notion's (seen 2026-11-09 in Hebrew: an English callout's
  // icon on the right); one set to a direction turns over whole.
  const ownWords = block.type === "callout" && direction === "auto";
  const body = code ? <CodeBlock adapter={adapter} block={block} editable={editable} engineFocused={engineFocused} />
    : block.type === "quote" ? <blockquote className={styles.quote} data-size={quoteSize(block) ?? undefined}>{editable}</blockquote>
    : block.type === "callout" ? <div className={styles.callout}><span contentEditable={false}><CalloutIcon adapter={adapter} block={block} /></span>
      <div className={styles.calloutContent} dir={ownWords ? wordless ?? "auto" : undefined} data-reads={ownWords ? reads : undefined}>{editable}</div></div>
    : block.type === "to_do" ? <div className={styles.todo} data-checked={block.content.checked === true}>
      {/* Named by its own line's words, as Notion's (seen 2026-11-19: aria-labelledby its text, no label or
          tooltip of its own; an empty to-do's box has an empty name). */}
      <input type="checkbox" contentEditable={false} checked={block.content.checked === true} disabled={adapter.readOnly}
        aria-labelledby={textId}
        onChange={event => adapter.updateBlock(block, { checked: event.currentTarget.checked })} />
      <TodoTaskLine adapter={adapter} block={block} run={controller.run}>{editable}</TodoTaskLine></div>
    : block.type === "numbered_list_item" ? <div className={styles.bullet}><NumberedMarker block={block} />{editable}</div>
    : block.type === "bulleted_list_item" ? <div className={styles.bullet}>
      <span className={styles.bulletDot} aria-hidden="true" contentEditable={false} data-list-marker="">{bulletGlyph(block, adapter.graph.blocks)}</span>{editable}</div>
    : toggle ? <div className={block.type === "toggle" ? styles.toggle : styles.toggleHeading}
      data-level={block.type.startsWith("toggle_heading_") ? Number(block.type.at(-1)) : undefined}>
      <button type="button" contentEditable={false} className={`${styles.toggleCaret} ${block.content.collapsed === true ? "" : styles.toggleOpen}`}
        data-empty={emptyToggle ? "true" : undefined}
        // A press on it leaves a touch screen's "+ ⋮⋮" where they were (drawnHandles.ts watchPersonFocus).
        data-leaves-handles=""
        aria-label={blockItemText(block.content.collapsed === true ? "toggle.open" : "toggle.close")}
        aria-expanded={block.content.collapsed !== true}
        onClick={() => adapter.setToggleCollapsed(block, block.content.collapsed !== true)}>
        <ToggleCaretIcon className={styles.toggleCaretIcon} size="tiny" aria-hidden="true" /></button>{editable}</div>
    : editable;
  return <NodeViewWrapper className={styles.textDirection} data-native-text-view
    data-native-tab-label={block.parentId && graphIndex.byId.get(block.parentId)?.type === "tab" ? "true" : undefined}
    dir={ownWords ? undefined : wordless ?? direction} data-reads={ownWords ? undefined : reads}>{body}</NodeViewWrapper>;
}

/**
 * A numbered item's marker in its column (numberedListMarkerColumn), drawn
 * again at once when its number changes: a line made or gone above it
 * renumbers the items after it, which the page's full draw reaches only past
 * the next frame (Editor.tsx).
 */
function NumberedMarker({ block }: { block: GraphBlock }) {
  const context = useContext(NativeTiptapReactContext)!;
  const drawn = useSyncExternalStore(context.latestGraph.subscribe, () => {
    const now = context.latestGraph.current.byId.get(block.id) ?? block;
    const { text, width, roman } = numberedListMarkerColumn(context.adapter.graph.blocks, now);
    return `${text}\n${width}\n${roman}`;
  });
  const [text, width, roman] = drawn.split("\n");
  const marker: NumberedListMarker = { text, width: width === "auto" ? "auto" : Number(width), roman: roman === "true" };
  return <span className={styles.numDot} aria-hidden="true" contentEditable={false} data-list-marker="" data-roman={marker.roman || undefined}
    style={marker.width === 24 ? undefined : { width: marker.width === "auto" ? "auto" : `${marker.width}px` }}>{marker.text}</span>;
}
