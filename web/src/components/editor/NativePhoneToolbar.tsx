import { useCallback, useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Editor } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PHONE_TOOLS_FOOT_GAP, placePhoneTools } from "@/lib/phoneTools";
import { bringLineOver, LINE_GAP } from "@/lib/phoneLineInSight";
import { softKeyboardShown } from "@/lib/softKeyboard";
import { ArrowLeftToLine, ArrowRightToLine, AtIcon, ChevronDown, CommentIcon, ImageIcon, KeyboardIcon, MoveDownIcon,
  MoveUpIcon, ToolPlusIcon, Trash, UndoIcon, XCircleOutlineIcon } from "@/icons/leafen";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { nativeTiptapNestingTransaction } from "./nativeTiptapStructureKeys";
import { projectedBlocks } from "./nativeTiptapBinding";
import { requestEditorBlockCommand } from "./editorCommands";
import { requestNativePhoneMenu } from "./nativePhoneMenuEvents";
import { useSuggestingMode } from "./suggestingMode";
import { phoneTitleCaretKept, phoneTitleToolsBeside, type PhoneTitleTools } from "./phoneTitleTools";
import { blockTakesComments } from "./blockMenuShape";
import { commentOnOwnBlock } from "./blockOwnComment";
import { chosenWords, usePhoneChosenWords, type PhoneTool } from "./PhoneSelectionTools";
import { wordsChosenByPerson } from "./inputMethodChoice";
import { phoneStripTools } from "./phoneStripTools";
import { closeStripPanel, currentStripPanel, useStripPanel, watchStripPanel, withStripPanels } from "./stripPanelState";
import { usePhoneMedia } from "./PhoneMediaSheet";
import styles from "./NativePhoneToolbar.module.css";

// The tools' height (NativePhoneToolbar.module.css); a touch screen wider
// than a phone's has them stand LINE_GAP under the caret's line, a phone's
// strip (its 1px line on top within) at the foot of what shows.
const TOOLS_HEIGHT = 42, STRIP_HEIGHT = 44;

/**
 * Phone caret tools route through the same native editor and block owners.
 * A page's own tools (`title`) show for its title's caret too, as Notion's
 * do on a page with something in it, and on a database's page and a
 * template's with nothing in it (the Notion Android app, seen 2026-11-25 on
 * the A90); an empty page of its own has Notion's template bar there, which
 * Leafen leaves out.
 * A phone's are a strip docked on the keyboard, the screen's width, wherever
 * the line is, as the Notion Android app's (measured 2026-11-21 on the A90 —
 * phones follow the app, phoneStripTools.tsx); its "+", "전환" and "A" open
 * their lists in the keyboard's place, the strip on them (stripPanelState.ts),
 * and its picture the media sheet (PhoneMediaSheet). A touch screen wider than a
 * phone's (a tablet, an opened fold) has them as wide as its tools, from 12
 * in, 10 under the caret's line, a soft keyboard up or none, as Notion's iPad
 * (seen 2026-11-13 in iPad Safari at 834 and 1210: 720.8 wide at x 12), the
 * lists its tools open there in the middle as sheets (PhoneMenu.module.css).
 */
export function NativePhoneToolbar({ title: withTitle = false }: { title?: boolean }) {
  const { phone, touch } = useSidebarLayout(true);
  const live = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live.adapter);
  const controller = live.controller, editor = controller.editor;
  const { t } = useTranslation("editor");
  const suggesting = useSuggestingMode();
  const toolbar = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState<number | null>(null);
  const [inTitle, setInTitle] = useState(false);
  // The line whose "Aa" turned a phone's strip to its marks, until typing or the caret's going elsewhere.
  const [styling, setStyling] = useState<string | null>(null);
  const [, refresh] = useState(0);
  // The tools are set only when they come, go or move, and drawn again for a
  // change while they show: a desktop or hidden toolbar sets nothing as the
  // page is typed in. The adapter is read as it is, not watched: each change
  // to the page's blocks is a new adapter, and the watch below outlives it, so
  // a line Enter made is still brought up (2026-11-08: a new list line stayed
  // under the tools on Android).
  const topRef = useRef(top), titleShown = useRef(inTitle), latest = useRef(adapter), titled = useRef(withTitle), docked = useRef(phone);
  latest.current = adapter;
  titled.current = withTitle;
  docked.current = phone;
  const place = useCallback((next: number | null, title = false) => {
    if (topRef.current === next && titleShown.current === title) return false;
    topRef.current = next; titleShown.current = title;
    setTop(next); setInTitle(title);
    if (next === null) setStyling(null);
    return true;
  }, []);
  const readOnly = adapter.readOnly;
  useEffect(() => {
    if (!touch || readOnly || suggesting.active) { place(null); return; }
    // The keyboard opening is followed by one reveal once the window settles.
    // A line changed while a sheet held focus ("/", "전환") is revealed
    // once the line has it again (2026-11-01: a heading chosen from "/" stayed
    // under the tools until typed in).
    let typing = false, opening = false, settled = 0, owed = false, drawn: unknown = editor.state;
    // The title's field beside the body is heard while it is found, for its
    // caret and words.
    // A database name's caret kept with the keyboard away keeps its tools,
    // and its going takes them (richCellCaretKept.ts marks the field).
    let heard: Editor | null = null, kept: MutationObserver | null = null;
    const hear = (next: Editor | null) => {
      if (heard === next) return;
      heard?.off("transaction", changed);
      kept?.disconnect();
      next?.on("transaction", changed);
      kept = next ? new MutationObserver(() => schedule()) : null;
      if (next) kept?.observe(next.view.dom, { attributes: true, attributeFilter: ["data-caret-kept"] });
      heard = next;
    };
    // The field the caret is in: the body, or the page's title beside it; a
    // tool a key's focus is on keeps the field it was shown for.
    function caretField(): { field: Editor; title: PhoneTitleTools | null } | null {
      const inTools = Boolean(toolbar.current?.contains(document.activeElement));
      if (editor.isFocused || inTools && !titleShown.current) return { field: editor, title: null };
      const title = titled.current ? phoneTitleToolsBeside(editor.view.dom) : null;
      hear(title?.editor ?? null);
      return title && (title.editor.isFocused || inTools && titleShown.current || phoneTitleCaretKept(title)) ? { field: title.editor, title } : null;
    }
    function update(reveal = false) {
      const at = editor.isDestroyed || !latest.current.isCurrent() ? null : caretField();
      if (!at || !(at.field.state.selection instanceof TextSelection)) {
        owed ||= reveal;
        place(null); return;
      }
      reveal ||= owed;
      owed = false;
      const { field, title } = at;
      const target = title ? null : nativeTiptapSelectionContext(editor.state);
      // The caret gone to another line takes that line's tools, not the marks "Aa" showed.
      setStyling(was => was && was !== target?.block.nativeId ? null : was);
      // Chosen words in the page take their own tools in the same place
      // (PhoneSelectionTools), a caption's and words across lines too; what
      // an input method holds chosen is the caret's (wordsChosenByPerson).
      const choosing = !field.state.selection.empty && wordsChosenByPerson(field.view);
      if (!choosing ? !title && (!target || target.field !== "text") : title && !title.words || !chosenWords(field)) { place(null); return; }
      const viewport = window.visualViewport;
      const height = viewport?.height ?? window.innerHeight;
      // Places are taken from the visual viewport's top, and the tools'
      // fixed place is that plus how far it is panned in the window: iPhone
      // Safari pans it as it scrolls to a field, before the window is put
      // back (phoneWindowHold.ts), and a pinch does; Android Chrome's window
      // shrinks with the keyboard instead (iosViewport.ts resizes-content).
      const shift = document.documentElement.getBoundingClientRect().top + (viewport?.pageTop ?? window.scrollY);
      const caret = field.view.coordsAtPos(field.state.selection.head).bottom - shift;
      const keyboard = softKeyboardShown();
      if (keyboard && !typing) opening = true;
      typing = keyboard;
      // A phone's strip stands at the foot of what shows, on the keyboard,
      // the line high or low (the Notion Android app's). A wider touch
      // screen's tools stand 10 under the caret's line, over the rows of its
      // block under it, with a soft keyboard up or none, as Notion's iPad (seen
      // 2026-11-13); where that is past the foot of what shows they stand at
      // that foot.
      // A list a strip's tool opened stands in the keyboard's place, the
      // strip on it, once the keyboard has gone (stripPanelState.ts).
      const opened = docked.current && !keyboard ? currentStripPanel() : null;
      const spot = docked.current ? height - STRIP_HEIGHT - (opened?.height ?? 0)
        : Math.max(44, Math.min(caret + LINE_GAP, height - TOOLS_HEIGHT - PHONE_TOOLS_FOOT_GAP));
      // Unmoved, the tools draw again only for a change of the page.
      if (!place((viewport?.offsetTop ?? 0) + spot, Boolean(title)) && drawn !== field.state) refresh(value => value + 1);
      drawn = field.state;
      // At that foot, the tools cover the page's foot: the caret's line is
      // brought up to stand 10 over them after Enter, typing, pasting, a
      // delete or a caret move, and once the keyboard has opened (the browser
      // brings the line up first: iPhone Safari to the foot of what shows,
      // Android Chrome to the middle of the page).
      if (reveal) bringLineOver(field.view.dom, caret, spot);
    }
    // The editor's blur transaction precedes the button receiving focus.
    // Measure after that focus transfer, so a keyboard-activated tool stays
    // mounted until its native click runs.
    let frame = 0, revealing = false;
    // A change or a caret move asks for the reveal until the frame runs: a
    // paste comes into the page as its new state, then its caret.
    const schedule = (reveal = false) => {
      revealing ||= reveal;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => { const shown = revealing; revealing = false; update(shown); });
    };
    // Typing ends a strip's "Aa" marks, as the app's (a letter typed after
    // its B went in bold and the line's tools came back, 2026-11-21).
    const changed = ({ transaction }: { transaction: { docChanged: boolean; selectionSet: boolean } }) => {
      if (transaction.docChanged) setStyling(null);
      schedule(transaction.docChanged || transaction.selectionSet);
    };
    const settle = () => schedule();
    const moved = () => {
      update();
      if (!opening) return;
      window.clearTimeout(settled);
      settled = window.setTimeout(() => { opening = false; update(true); }, 250);
    };
    editor.on("transaction", changed); editor.on("focus", settle); editor.on("blur", settle);
    const unwatch = watchStripPanel(settle);
    // The title's focus comes and goes as the page's does.
    document.addEventListener("focusin", settle); document.addEventListener("focusout", settle);
    window.addEventListener("scroll", moved, true); window.addEventListener("resize", moved);
    window.visualViewport?.addEventListener("resize", moved); window.visualViewport?.addEventListener("scroll", moved);
    update();
    return () => {
      cancelAnimationFrame(frame); window.clearTimeout(settled); hear(null); unwatch();
      editor.off("transaction", changed); editor.off("focus", settle); editor.off("blur", settle);
      document.removeEventListener("focusin", settle); document.removeEventListener("focusout", settle);
      window.removeEventListener("scroll", moved, true); window.removeEventListener("resize", moved);
      window.visualViewport?.removeEventListener("resize", moved); window.visualViewport?.removeEventListener("scroll", moved);
    };
  }, [touch, editor, readOnly, suggesting.active, place]);
  // Where the tools stand while drawn, for a list opened by the line to keep
  // clear of them (phoneTools.ts).
  useLayoutEffect(() => {
    placePhoneTools(toolbar, toolbar.current && top !== null ? { top, bottom: top + toolbar.current.offsetHeight } : null);
  });
  useEffect(() => () => placePhoneTools(toolbar, null), []);
  // Chosen words' tools take a line in or out as the caret's do.
  const chosen = usePhoneChosenWords(editor, adapter, lift => {
    const at = nativeTiptapSelectionContext(editor.state);
    return at ? nativeTiptapNestingTransaction(editor.state, projectedBlocks(editor.state.doc), at.block, lift) : null;
  });
  const panel = useStripPanel();
  // A phone strip's picture opens the media sheet, which outlives the tools (the keyboard goes as it opens).
  const media = usePhoneMedia(controller, latest);
  const sheets = <>{chosen.sheet}{media.sheet}</>;
  if (!touch || top === null || adapter.readOnly || !adapter.isCurrent() || suggesting.active) return sheets;
  const title = inTitle && withTitle ? phoneTitleToolsBeside(editor.view.dom) : null;
  const words = chosenWords(title?.editor ?? editor);
  const target = title ? null : nativeTiptapSelectionContext(editor.state);
  const block = target ? adapter.graph.blocks.find(block => block.id === target.block.nativeId) : undefined;
  if (!title && !block && !words) return sheets;
  const field = title?.editor ?? editor;
  const usable = () => !editor.isDestroyed && !field.isDestroyed && adapter.isCurrent() && !adapter.readOnly && !suggesting.active;
  function command(kind: Parameters<typeof requestEditorBlockCommand>[1]) {
    if (!usable() || !target || !block) return;
    const source = editor.view.dom.querySelector(`[data-native-occurrence="${CSS.escape(target.block.id)}"]`) ?? editor.view.dom;
    requestEditorBlockCommand(block.id, kind, { x: 12, y: toolbar.current?.getBoundingClientRect().bottom ?? top! + 42, sheet: true }, source);
  }
  const blocks = target ? projectedBlocks(editor.state.doc) : [];
  const nest = (lift: boolean) => target ? nativeTiptapNestingTransaction(editor.state, blocks, target.block, lift) : null;
  const siblings = target ? blocks.filter(value => value.projectionParentId === target.block.projectionParentId) : [];
  const index = target ? siblings.findIndex(value => value.id === target.block.id) : -1;
  function move(step: -1 | 1) {
    if (!usable() || !block) return;
    const after = siblings[index + step - (step === -1 ? 1 : 0)];
    controller.run(() => adapter.move(block.id, block.parentId, after?.nativeId ?? null));
    editor.view.focus();
  }
  // "@" is typed into the line, after a space where the caret follows a
  // letter, and opens the menu a typed "@" does, as Notion's: closing that
  // leaves the "@" (seen 2026-11-08 at 375).
  function typeMention() {
    const at = nativeTiptapSelectionContext(editor.state);
    if (!at || at.field !== "text" || !editor.state.selection.empty) return;
    const before = at.text[at.start - 1];
    editor.view.dispatch(editor.state.tr.insertText(before === undefined || /\s/u.test(before) ? "@" : " @"));
  }
  // In a title the tools are a line's, but only its "@", a comment on the
  // page and the page's own undo and redo work there, as Notion's (seen
  // 2026-11-10 on an iPhone: the rest dimmed).
  const comment = title?.comment();
  // A line's comment quotes its words, as its block's "댓글" does, but marks
  // none while it is written, as Notion's (seen 2026-11-10 on an iPhone).
  function lineComment() {
    if (usable() && block && blockTakesComments(block)) commentOnOwnBlock(adapter, block, true);
  }
  // A tool leaves the keyboard as it is, down or up, as Notion's every tool
  // does (seen 2026-11-14 in iPhone Safari with it down: B, "@" and 댓글
  // changed the page, opened the "@" menu and the comment field, the keyboard
  // staying down): its press keeps the field's focus (the tools' mousedown,
  // as Notion's), gives it back only from a tool a key's focus is on, and
  // the tool acts on the next frame. Focused again in the press, or its
  // selection drawn again there, the field brought the keyboard up.
  function pressTool(tool: PhoneTool, button: HTMLElement) {
    if (!field.view.hasFocus()) field.view.focus();
    requestAnimationFrame(() => { if (usable()) tool.run(button); });
  }
  // A phone's strip takes the app's order (phoneStripTools.tsx); its "Aa"
  // turns it to the line's marks until typing or the caret's going elsewhere.
  const strip = phone;
  const emptyLine = Boolean(target && target.block.container.attrs.kind === "paragraph" && target.node.content.size === 0);
  // A database's name has its "@" alone, the keyboard's beside it, as Notion's (seen 2026-11-16 in iPad Safari at 834).
  const wide: PhoneTool[] = [
    { key: "add", icon: ToolPlusIcon, disabled: Boolean(title), run: () => requestNativePhoneMenu(editor), arrow: true },
    { key: "turn", label: t("phoneToolbar.turn"), disabled: Boolean(title), run: () => command("turn"), arrow: true },
    { key: "mention", icon: AtIcon, run: title ? title.mention : typeMention },
    ...(adapter.session ? [{ key: "comment", icon: CommentIcon, disabled: Boolean(title) && !comment,
      run: () => comment ? comment() : lineComment() }] : []),
    // A strip's picture opens the app's media sheet, in the title too (PhoneMediaSheet).
    { key: "image", icon: ImageIcon, disabled: Boolean(title) && !strip,
      run: () => strip ? media.open(title ? null : block?.id ?? null, !title && emptyLine) : requestNativePhoneMenu(editor, "image") },
    { key: "delete", icon: Trash, disabled: Boolean(title), run: () => command("deleteToLineAbove") },
    { key: "outdent", icon: ArrowLeftToLine, group: "nest", disabled: !nest(true), run: () => { const tr = nest(true); if (tr) editor.view.dispatch(tr); } },
    { key: "indent", icon: ArrowRightToLine, group: "nest", disabled: !nest(false), run: () => { const tr = nest(false); if (tr) editor.view.dispatch(tr); } },
    { key: "moveUp", icon: MoveUpIcon, group: "nest", disabled: index < 1, run: () => move(-1) },
    { key: "moveDown", icon: MoveDownIcon, group: "nest", disabled: index < 0 || index === siblings.length - 1, run: () => move(1) },
    { key: "undo", icon: UndoIcon, group: "history", disabled: !editor.can().undo(), run: () => editor.commands.undo() },
    { key: "redo", icon: UndoIcon, group: "history", disabled: !editor.can().redo(), run: () => editor.commands.redo(), flipped: true },
    { key: "color", label: t("phoneToolbar.color"), disabled: Boolean(title), run: () => command("colorMenu"), arrow: true },
    { key: "more", label: t("phoneToolbar.more"), disabled: Boolean(title), run: () => command("menu"), arrow: true },
  ];
  const shown: PhoneTool[] = words ? chosen.tools(title, strip) : title?.name ? [{ key: "mention", icon: AtIcon, run: title.mention }]
    : !strip ? wide
    : block && !title && styling === block.id ? chosen.caretTools(() => setStyling(null), () => command("colorMenu"))
    : phoneStripTools(wide, { title: Boolean(title), blockType: block?.type, textStyle: () => { if (block) setStyling(block.id); } });
  // Its "+", "전환" and "A" open their lists in the keyboard's place (stripPanelState.ts).
  const tools = strip ? withStripPanels(shown, field.view.dom, panel) : shown;
  return <>{createPortal(<div ref={toolbar} className={styles.toolbar} style={{ top }} data-native-phone-toolbar
    data-wide-touch={phone ? undefined : "true"} data-phone-strip={strip ? "" : undefined}
    data-phone-tools-field={words ? "words" : title?.name ? "name" : title ? "title" : "line"}
    // iOS Safari still sends a touch's mousedown after a pointerdown held
    // back, and its focus went to the tool (2026-11-14): both are held back.
    role="toolbar" aria-label={t("phoneToolbar.ariaLabel")} onPointerDown={event => event.preventDefault()}
    onMouseDown={event => event.preventDefault()}
    onKeyDown={event => {
      // A tool's Enter/Space belongs to its button, not the page's block keys.
      event.stopPropagation();
      if (event.key === "Escape") { event.preventDefault(); field.view.dom.blur(); place(null); }
    }}>
    <div className={styles.tools}>
      {/* Outdent, indent and the moves share one cell, as do undo and redo, as Notion's (seen 2026-11-13). */}
      {toolCells(tools).map(cell => {
        const buttons = cell.map(tool => <button type="button" key={tool.key} data-phone-tool={tool.key} disabled={tool.disabled}
          aria-label={tool.name ?? t(`phoneToolbar.${tool.key}`)} title={tool.name ?? t(`phoneToolbar.${tool.key}`)}
          aria-pressed={tool.pressed} onClick={event => { if (usable()) pressTool(tool, event.currentTarget); }}>
          {tool.icon ? <tool.icon size={strip ? "large" : "regular"} style={tool.flipped ? { transform: "scaleX(-1)" } : undefined} /> : tool.label}
          {tool.arrow ? <ChevronDown size="small" weight="regular" className={styles.arrow} /> : null}
        </button>);
        return cell[0].group ? <div key={cell[0].group} className={styles.group} data-group={cell[0].group} role="group">{buttons}</div> : buttons;
      })}
    </div>
    {/* A list open in the keyboard's place turns the keyboard's cell to ⓧ, which brings the keyboard back, as the app's. */}
    {strip && panel ? <button type="button" className={styles.keyboard} data-phone-tool="closePanel" aria-label={t("phoneToolbar.closePanel")}
      onClick={() => closeStripPanel(true)}><XCircleOutlineIcon size="large" /></button>
    : <button type="button" className={styles.keyboard} data-phone-tool="keyboard" aria-label={t("phoneToolbar.keyboard")}
      onClick={() => {
        // A name's caret goes with its tools, as Notion's (seen 2026-11-16 in iPad Safari): kept, they would stay.
        if (title?.name) window.getSelection()?.removeAllRanges();
        field.view.dom.blur();
        if (document.activeElement instanceof HTMLElement && toolbar.current?.contains(document.activeElement)) document.activeElement.blur();
        place(null);
      }}><KeyboardIcon size={strip ? "large" : "regular"} /></button>}
  </div>, document.body)}{sheets}</>;
}

/** The tools in the cells they are drawn in: those of one group together, each other alone. */
function toolCells<T extends { group?: string }>(tools: readonly T[]) {
  const cells: T[][] = [];
  for (const tool of tools) {
    const last = cells.at(-1);
    if (tool.group && last?.[0].group === tool.group) last.push(tool);
    else cells.push([tool]);
  }
  return cells;
}
