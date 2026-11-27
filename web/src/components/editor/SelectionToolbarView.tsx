import { shortcutTipKeys } from "@/lib/keyboardShortcuts";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useNativeToolbar } from "@/lib/nativeToolbar";
import { SelectionToolbarLinkDialog } from "./SelectionToolbarLinkDialog";
import { SelectionToolbarLinkPicker, type SelectionLinkPages } from "./SelectionToolbarLinkPicker";
import { pageHref } from "@/lib/navigation";
import { placeAround, placeAtPoint, popoverRectOf, popoverPlacementStyle, usePopoverPlacement,
  type PopoverRect, type PopoverRequest, type PopoverSize, type PopoverViewport } from "@/lib/popoverPlacement";
import type { BlockType } from "@/lib/types";
import {
  BoldIcon, CheckIcon, ChevronDown, ChevronRight, ClearFormattingIcon, CodeIcon, CommentIcon, Copy, DotsHorizontal, InlineEquationIcon, ItalicIcon, LinkIcon,
  OpenInNew, AddReactionIcon, StrikethroughIcon, SuggestEditIcon, UnderlineIcon,
} from "@/icons/leafen";
import { type Dispatch, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type RefObject, type SetStateAction, useEffect, useLayoutEffect,
  useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { BlockPageInAnchor } from "./BlockActionMenu";
import { BlockIcon } from "./BlockIcon"; import { TurnRowTip } from "./TurnRowTip";
import { blockTurnChoiceLabel } from "./blockItemLabels";
import { blockTurnChoices, type BlockTurnChoice } from "./blockTurnInto";
import { recentEditorColors } from "./colorMemory";
import {
  BACKGROUND_COLORS,
  TEXT_COLORS,
  TURN_OPTIONS,
  type SelectionColorChoice,
  type SelectionToolbarActiveColor,
  type SelectionToolbarActiveMarks,
  type SelectionToolbarMark,
} from "./selectionToolbarModel";
import styles from "./editorStyles.generated.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";
import { placeCommentRow } from "./selectionToolbarRow";
import { useOpenerAtOpen } from "./selectionToolbarPlace";

/** The page's suggesting mode the toolbar turns on or off. */
export type SelectionToolbarSuggesting = { active: boolean; onChange(active: boolean): void };

type SelectionToolbarViewProps = {
  /** The selected text's color, and apart from it its background. */
  activeColor: SelectionToolbarActiveColor;
  activeBackground?: SelectionToolbarActiveColor;
  activeMarks: SelectionToolbarActiveMarks;
  /** Marks the chosen words can't take, shown off (inline code in a code block, a link across blocks). */
  offMarks?: Partial<Record<SelectionToolbarMark, boolean>>;
  activeType: BlockType | null;
  apply: (mark: SelectionToolbarMark) => void;
  applyColor: (token: string) => void;
  /** Links the selected text to the given address, or to the one typed. */
  applyLinkValue: (href?: string) => void;
  /** Where the link picker finds pages; without it only a typed address links. */
  linkPages?: SelectionLinkPages;
  /** Takes the selected text's marks and color off; not offered when left out. */
  clearFormatting?: () => void;
  closeColors: (restoreFocus?: boolean) => void;
  closeLinkEditor: (restoreFocus?: boolean) => void;
  closeTurn: (restoreFocus?: boolean) => void;
  colorButtonRef: RefObject<HTMLButtonElement | null>;
  colorMenuRef: RefObject<HTMLDivElement | null>;
  colorsOpen: boolean;
  commentOnly: boolean;
  commentSelection: () => void;
  /** Picks an emoji to react to the selected text with; not offered when left out. */
  reactToSelection?: (button: HTMLElement) => void;
  /** Text outside blocks, such as a database cell, can't turn into a block
   * or take a comment. */
  blockActions?: boolean;
  /** A database cell's text takes a comment all the same, as Notion's (seen 2026-10-31). */
  commentAction?: boolean;
  /** Whether turning the block into another type is offered. */
  turnIntoAvailable?: boolean;
  /** Words chosen across several blocks: their types' row and its list for
   * that many blocks, though they take no block's own actions. */
  severalBlocks?: { count: number; mixed: boolean };
  /** The inline equation shown but greyed, as Notion's across blocks. */
  inertEquation?: boolean;
  /** Gives the text the focus back, its words still chosen: Escape in the
   * link picker does, the picker staying open, as Notion's (seen 2026-11-09). */
  focusText?: () => void;
  copyCurrentLink: () => Promise<void>;
  currentLinkUrl: string;
  hadLinkOnOpen: boolean;
  linkButtonRef: RefObject<HTMLButtonElement | null>;
  linkCopied: boolean;
  linkInputRef: RefObject<HTMLInputElement | null>;
  linkOpen: boolean;
  /** The selected text, when the link picker was asked for from the keyboard rather than by the toolbar's button. */
  linkAtText?: DOMRect | null;
  linkValue: string;
  /** Makes the selected text an inline equation and edits it; not offered when left out. */
  makeEquation?: () => void;
  /** A greyed "⋯", as Notion's bar shows in a database's text (seen 2026-10-30). */
  inertMore?: boolean;
  /** Opens the menu of the block the text is in, at the press; not offered when left out. */
  openBlockMenu?: (event: ReactMouseEvent<HTMLButtonElement>) => void;
  openCurrentLink: () => void;
  rect: DOMRect;
  removeLink: () => void;
  setColorsOpen: Dispatch<SetStateAction<boolean>>;
  setLinkValue: Dispatch<SetStateAction<string>>;
  setTurnOpen: Dispatch<SetStateAction<boolean>>;
  suggesting?: SelectionToolbarSuggesting;
  toolbarLinkMenuRef: RefObject<HTMLDivElement | null>;
  toolbarRef: RefObject<HTMLDivElement | null>;
  turnButtonRef: RefObject<HTMLButtonElement | null>;
  turnInto: (choice: BlockTurnChoice) => void;
  /** "페이지 위치:" opens the pages to put the block in beside the list. */
  onPageIn?: (place: BlockPageInAnchor | null) => void;
  turnMenuRef: RefObject<HTMLDivElement | null>;
  turnOpen: boolean;
  selectionOwnerRef: RefObject<HTMLElement | null>;
  dismissToolbar: () => void;
  linkReturnFocusRef?: RefObject<HTMLElement | null>;
  selectLinkOnOpen?: boolean;
  /** "row": one row of marks over the selection, as Notion's comment box
   * shows (seen 2026-10-15), rather than the page's panel below it. */
  layout?: "panel" | "row";
  /** The page the text is in, which the panel keeps to; the window without it. */
  page?: SelectionToolbarPage;
  /** Only its link box shows, as ⌘K opens it in a database's name, which has no bar (Notion's, seen 2026-11-09). */
  linkOnly?: boolean;
};

/** The page around chosen text: its column's end edge, and the end edge and
 * top (under the bar) of the area showing it — their right edges, or their
 * left where the screen reads from the right —, and the text, whose scrolling places the panel again. */
export type SelectionToolbarPage = { column: number; right: number; top: number; owner?: Element };

// Notion's toolbar is a panel below the end of the selected text: the
// block's type, then rows of marks, then comment and suggesting.
const TOOLBAR_WIDTH = 192;
// The panel's top left is 16px right of and below the selection's end;
// below, it keeps 12px over the window's foot, by its own height (seen
// 2026-11-09 at 1054×771: the 150px panel of a page's text or of code under
// words ending at 592, over them from 593). Notion's page panel adds AI rows
// only while its workspace has AI answers left — 317px with four, 191 with
// "AI로 편집하기" alone, then none and 150 (seen 2026-11-09), choosing its
// side by whichever it draws; this one has none (approved difference), as
// Notion's last.
const PANEL_OFFSET = 16, PANEL_FOOT = 12;
// Over the text, Notion's panel sits in a 16px frame that it measures while
// opening at 98% of its size (popoverAppear.ts "panel"), so its foot comes 16px over the words less 2%
// of the frame (seen 2026-11-09 at 1054×715, double-click or keys, one block
// or several: 11.5 for its 191px panel, 12.4 for code's 150px; 9 for the
// 317px one before) — 12.4 for this one's 150px.
const PANEL_OPENING_SCALE = 0.98;

/**
 * The panel's place, as Notion's (read and seen 2026-11-09, 768×723 and
 * 1054×771): 16px right of the farthest end of the chosen lines and 16px
 * under their foot; over them, its foot 16px over their top less 2% of its
 * frame (PANEL_OPENING_SCALE), when the room under them is short of the
 * panel and the 16px over and 12px under it (not above the page's top); against the column's right edge, 16px in, when the
 * panel would pass it; and out beside the column, 2.5px over the text's top,
 * when the column has 224px beside it in the area — out once, it stays out
 * while shown.
 */
function placePanel(rect: DOMRect, size: PopoverSize, viewport: PopoverViewport, page: SelectionToolbarPage | undefined,
  outside: { current: boolean }): PopoverRequest {
  // Where the screen reads from the right it is the same place mirrored: left
  // of the lines' farthest left, its top right there (placeAtPoint), against
  // the column's left edge, or out beside the column on its left.
  const rtl = readsRightToLeft(), across = (x: number) => rtl ? viewport.width - x : x;
  const end = across(rtl ? rect.left : rect.right);
  const top = page?.top ?? 0, right = Math.min(page ? across(page.right) : viewport.width, viewport.width);
  const column = Math.min(page ? across(page.column) : right, right);
  // The room it needs under the words: its own height and the gaps over and under it.
  const room = size.height + PANEL_OFFSET + PANEL_FOOT;
  if (right - column >= TOOLBAR_WIDTH + 2 * PANEL_OFFSET && end + PANEL_OFFSET + TOOLBAR_WIDTH > column) outside.current = true;
  if (outside.current) {
    const y = Math.max(Math.min(rect.top - PANEL_OFFSET - 2.5, viewport.height - room - 8), top) + PANEL_OFFSET;
    return placeAtPoint({ x: across(column + PANEL_OFFSET), y, contextElement: page?.owner }, size, viewport, { appear: "panel" });
  }
  const x = across(end + PANEL_OFFSET + TOOLBAR_WIDTH <= column ? end + PANEL_OFFSET : column - PANEL_OFFSET - TOOLBAR_WIDTH);
  const aboveGap = PANEL_OFFSET - (1 - PANEL_OPENING_SCALE) * (size.height + 2 * PANEL_OFFSET);
  const y = rect.bottom + room <= viewport.height ? rect.bottom + PANEL_OFFSET
    : Math.max(rect.top - aboveGap - size.height, top + PANEL_OFFSET);
  return placeAtPoint({ x, y, contextElement: page?.owner }, size, viewport, { appear: "panel" });
}

// Notion's color menu is a 190px grid of 30px swatches, five to a row (seen 2026-10-08).
const COLOR_MENU_WIDTH = 190;
const COLOR_MENU_MAX_HEIGHT = 430;
// Notion's link picker is 330px, dropping from its button (seen 2026-10-08).
const LINK_MENU_WIDTH = 330;
const TURN_MENU_WIDTH = 220;
// The turn menu lists what the block menu's "전환" does for one block, as
// Notion's selection menu does (seen 2026-10-31), but a synced block; for
// several, what it does for several, a synced block and columns among them
// (seen 2026-11-09).
const TURN_CHOICES = blockTurnChoices(1).filter((choice) => choice.type !== "synced_block");
const turnChoicesFor = (count: number) => count > 1 ? blockTurnChoices(count) : TURN_CHOICES;
// It sits against its button's right edge, centred on it and at most 70% of
// the window tall, 12px from the window's edges (seen 2026-10-31), fading in as a list beside a row does (popoverAppear.ts).
const placeTurnMenu = (button: PopoverRect, viewport: PopoverViewport): PopoverRequest => ({
  anchor: button, placement: "right", gap: 0, options: { cap: Math.round(viewport.height * 0.7), appear: "fade" } });
// The pointer opens the list 300ms after it comes onto the type's row and
// closes it 200ms after it leaves the row and the list, however the list was
// opened; a press on the row leaves it open. The panel's other buttons open
// on a press alone (Notion's, seen 2026-11-13 at 1440×723).
const TURN_HOVER_OPEN_MS = 300, TURN_HOVER_CLOSE_MS = 200;

export function SelectionToolbarView(props: SelectionToolbarViewProps) {
  const { t } = useTranslation(["selectionToolbar", "common"]);
  const {
    activeColor, activeBackground = "default_background", activeMarks, offMarks, activeType, apply, applyColor, applyLinkValue, clearFormatting,
    closeColors, closeLinkEditor, closeTurn, colorButtonRef, colorMenuRef,
    colorsOpen, commentOnly, commentSelection, reactToSelection, copyCurrentLink, currentLinkUrl,
    hadLinkOnOpen, linkAtText, linkButtonRef, linkCopied, linkInputRef, linkOpen, linkValue, makeEquation,
    openBlockMenu, openCurrentLink, rect, removeLink, setColorsOpen, setLinkValue, setTurnOpen, suggesting,
    toolbarLinkMenuRef, toolbarRef, turnButtonRef, turnInto, onPageIn, turnMenuRef, turnOpen, blockActions = true, turnIntoAvailable = true,
    selectionOwnerRef, dismissToolbar, linkReturnFocusRef = linkButtonRef, selectLinkOnOpen, linkPages, page,
    severalBlocks, inertEquation, focusText,
  } = props;
  const row = props.layout === "row";
  const showTurn = !commentOnly && !row && (blockActions || Boolean(severalBlocks)) && turnIntoAvailable;
  const turnChoices = useMemo(() => turnChoicesFor(severalBlocks?.count ?? 1), [severalBlocks?.count]);
  const outside = useRef(false);
  const actionsRef = useRef<HTMLDivElement>(null);
  const { toolbarProps } = useNativeToolbar({ "aria-label": t("selectionToolbar:toolbar.ariaLabel"), orientation: row ? "horizontal" : "vertical" }, actionsRef);
  const inside = () => [toolbarRef.current, selectionOwnerRef.current];
  // Escape from a toolbar control returns to the selected text. A click
  // elsewhere or the text owner's own blur leaves focus where the user put it.
  const escaped = useRef(false);
  const toolbarPlace = usePopoverPlacement(toolbarRef, (size, viewport) => row
    ? placeCommentRow(rect, size, viewport)
    : placePanel(rect, size, viewport, page, outside), [rect, row, page?.column, page?.right, page?.top], undefined, {
      kind: "toolbar", onEscape: () => { escaped.current = true; dismissToolbar(); }, onOutsidePress: dismissToolbar,
      inside: () => [selectionOwnerRef.current], returnFocusRef: selectionOwnerRef,
      shouldRestoreFocus: () => escaped.current,
    });
  // As Notion's, the color menu drops from its "A" button, over the toolbar below it. While it or the turn list is open
  // the page beside them still takes the wheel — they stand where they opened as the toolbar follows the words
  // (useOpenerAtOpen) —, and a press there closes them and goes on — on another line's words it puts the caret there
  // (Notion, Chrome 1440, two tabs 2026-11-16, judged by one letter typed after the press).
  const colorOpener = useOpenerAtOpen(colorsOpen, colorButtonRef), turnOpener = useOpenerAtOpen(turnOpen, turnButtonRef, toolbarRef);
  const colorMenu = usePopoverPlacement(colorMenuRef, colorsOpen ? (size, viewport) => placeAround(
    colorOpener()!, size, viewport, { gap: 4, cap: COLOR_MENU_MAX_HEIGHT }) : null,
    [colorsOpen, toolbarPlace], undefined, { kind: "toolbar", onEscape: () => closeColors(false), onOutsidePress: dismissToolbar,
      inside, followsText: false, returnFocusRef: colorButtonRef, pressGoesOn: true,
      // A color chosen gives the text the caret while the menu stays open;
      // Escape there closes the menu alone, before the text takes the key.
      onCaretKeyDown: (event) => {
        if (event.key !== "Escape" || (event.target instanceof Node && colorMenuRef.current?.contains(event.target))) return;
        event.preventDefault(); event.stopPropagation(); closeColors(false);
      } });
  const turnMenu = usePopoverPlacement(turnMenuRef, turnOpen ? (_size, viewport) => placeTurnMenu(
    turnOpener()!, viewport) : null,
    [turnOpen, toolbarPlace], undefined, { kind: "toolbar", onEscape: () => closeTurn(false), onOutsidePress: dismissToolbar,
      inside, followsText: false, returnFocusRef: turnButtonRef, pressGoesOn: true });
  // From ⌘K it opens in the middle under the selected text and the toolbar already shown stays behind it, as Notion's
  // (seen 2026-11-08 after a double-click; with no toolbar shown, ⌘K adds none); from its button, under the button —
  // over it in a comment box's row, leaving the box in sight (seen 2026-11-13). Escape in its field gives the chosen words
  // the focus back, the picker staying, and the next closes it (seen 2026-11-09). While it is open the page, its words
  // too, takes no wheel or press: a press there closes it alone, the words still chosen and the toolbar back, as Notion's
  // (seen 2026-11-15 and 2026-11-16 at 1440: the wheel over the words and beside them left the page where it was).
  const [linkLeft, setLinkLeft] = useState(false);
  if (!linkOpen && linkLeft) setLinkLeft(false);
  const linkMenu = usePopoverPlacement(toolbarLinkMenuRef, linkOpen ? (size, viewport) => linkAtText
    ? { anchor: { top: linkAtText.top, bottom: linkAtText.bottom, left: linkAtText.left, right: linkAtText.right, contextElement: selectionOwnerRef.current ?? undefined }, placement: "bottom", gap: 8, options: {} }
    : placeAround(popoverRectOf(linkButtonRef.current ?? toolbarRef.current)!, size, viewport, { gap: 4, prefer: row ? "above" : undefined }) : null,
    [linkOpen, linkAtText, toolbarPlace], undefined, { kind: "menu", onEscape: () => {
      if (focusText && toolbarLinkMenuRef.current?.contains(document.activeElement)) setLinkLeft(true);
      else closeLinkEditor(false);
    }, onOutsidePress: () => closeLinkEditor(false), inside, answersThroughShield: () => [toolbarRef.current], returnFocusRef: linkReturnFocusRef });
  // The picker holds the focus no more once left: then the text takes it.
  const focusTextRef = useRef(focusText); focusTextRef.current = focusText;
  useLayoutEffect(() => { if (linkLeft) focusTextRef.current?.(); }, [linkLeft]);
  // "페이지 위치:" opens its pages beside the list, as the block menu's does.
  const pageInRef = useRef<HTMLButtonElement>(null);
  const openPageIn = () => {
    const row = pageInRef.current, menu = turnMenuRef.current;
    if (!onPageIn || !row || !menu) return false;
    onPageIn({ row: row.getBoundingClientRect(), menu: menu.getBoundingClientRect(), returnFocusRef: pageInRef,
      onLeaveList: () => closeTurn(false) });
    return true;
  };
  const turnChecked = (choice: BlockTurnChoice) => activeType === choice.type && choice.type !== "column_list" && !choice.placeIn;
  // Opened by the pointer resting on its row, the list leaves the focus with
  // the chosen words, so nothing gives it back as it closes.
  const [turnByPointer, setTurnByPointer] = useState(false);
  if (!turnOpen && turnByPointer) setTurnByPointer(false);
  const turnTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(turnTimer.current), []);
  const openTurn = () => { window.clearTimeout(turnTimer.current); closeColors(false); closeLinkEditor(false); setTurnOpen(true); };
  const turnHover = (entering: boolean) => (event: ReactPointerEvent) => {
    if (event.pointerType !== "mouse") return;
    window.clearTimeout(turnTimer.current);
    if (entering && !turnOpen) turnTimer.current = window.setTimeout(() => { setTurnByPointer(true); openTurn(); }, TURN_HOVER_OPEN_MS);
    // "페이지 위치:"'s pages beside the list are the list's.
    else if (!entering && turnOpen) turnTimer.current = window.setTimeout(() => {
      if (!document.querySelector("[data-page-in-picker]")) closeTurn(false);
    }, TURN_HOVER_CLOSE_MS);
  };
  const turnCollection = useNativeMenu({ items: turnChoices, open: turnOpen, menuRef: turnMenuRef,
    label: t("selectionToolbar:turnInto.label"), keyOf: item => item.key, textOf: item => blockTurnChoiceLabel(item),
    onAction: item => { if (!item.placeIn || !openPageIn()) turnInto(item); },
    firstKey: turnChoices.find(turnChecked)?.key, focusOnOpen: Boolean(turnMenu) && !turnByPointer, tabRings: true });
  // "최근 사용" is read as the menu opens: the colors used lately, newest first.
  const recent = useMemo(() => {
    if (!colorsOpen) return [];
    const choices: readonly SelectionColorChoice[] = [...TEXT_COLORS, ...BACKGROUND_COLORS];
    return recentEditorColors().flatMap((token) => choices.filter((choice) => choice.token === token));
  }, [colorsOpen]);
  const colorItems = [...recent.map((choice) => ({ ...choice, key: `recent:${choice.token}` })),
    ...[...TEXT_COLORS, ...BACKGROUND_COLORS].map((choice) => ({ ...choice, key: choice.token }))];
  // As Notion's, the text's own color and its background are each marked:
  // a text holding no color of a kind marks that kind's default.
  const colorChecked = (token: string) => token.endsWith("_background")
    ? activeBackground !== "mixed" && token === activeBackground
    : activeColor !== "mixed" && token === activeColor;
  const colorCollection = useNativeMenu({ items: colorItems, open: colorsOpen, menuRef: colorMenuRef,
    label: t("selectionToolbar:colors.text.heading"), keyOf: item => item.key, textOf: item => t(item.label),
    onAction: item => applyColor(item.token), firstKey: activeColor === "mixed" ? "default" : activeColor, focusOnOpen: Boolean(colorMenu) });
  const swatch = (item: (typeof colorItems)[number]) => (
    <NativeMenuItem state={colorCollection.state} itemKey={item.key} key={item.key} className={styles.tbColorItem}
      data-color-menu-item role="menuitemradio" aria-checked={colorChecked(item.token)}
      data-active={colorChecked(item.token) ? "true" : undefined}>
      {({ labelProps }) => <><span className={styles.colorSwatch} data-color={item.token} aria-hidden="true">
        {item.token.endsWith("_background") ? null : "A"}</span>
        <span {...labelProps} className={styles.tbColorName}>{t(item.label)}</span></>}
    </NativeMenuItem>
  );
  const colorGroup = (heading: string, items: typeof colorItems) => (
    <>
      <div className={styles.tbColorLabel} aria-hidden="true">{heading}</div>
      <div role="group" aria-label={heading} className={styles.tbColorGrid}>{items.map(swatch)}</div>
    </>
  );
  const activeTurn = TURN_OPTIONS.find((option) => option.type === activeType);
  // The button names a toggle heading as its heading, as Notion's (seen 2026-10-31).
  const headingType = activeType?.startsWith("toggle_heading_") ? activeType.slice("toggle_".length) : null;
  const shownTurn = activeTurn ?? TURN_OPTIONS.find((option) => option.type === headingType) ?? TURN_OPTIONS[0];
  const activeColorLabel = activeColor === "mixed"
    ? t("selectionToolbar:colors.mixed")
    : t(
        [...TEXT_COLORS, ...BACKGROUND_COLORS].find((color) => color.token === activeColor)?.label
          ?? "selectionToolbar:colors.text.default",
      );
  const toolbarColorToken = activeColor === "mixed" ? "default" : activeColor;
  const toolbarBackgroundToken = activeBackground === "mixed" || activeBackground === "default_background" ? null : activeBackground;
  const markLabels: Record<SelectionToolbarMark, string> = {
    bold: t("selectionToolbar:marks.bold"), italic: t("selectionToolbar:marks.italic"),
    underline: t("selectionToolbar:marks.underline"),
    strikethrough: t("selectionToolbar:marks.strikethrough"),
    code: t("selectionToolbar:marks.code"), link: t("selectionToolbar:marks.link"),
  };
  // Each tip as Notion's text menu has it, its shortcut under it (seen 2026-10-28).
  const markTips: Record<Exclude<SelectionToolbarMark, "link">, [string, string]> = {
    bold: [t("selectionToolbar:tips.bold"), shortcutTipKeys("bold")],
    italic: [t("selectionToolbar:tips.italic"), shortcutTipKeys("italic")],
    underline: [t("selectionToolbar:tips.underline"), shortcutTipKeys("underline")],
    strikethrough: [t("selectionToolbar:tips.strikethrough"), shortcutTipKeys("strikethrough")],
    code: [t("selectionToolbar:tips.code"), shortcutTipKeys("inlineCode")],
  };
  // Each mark is a 20px glyph, as Notion's panel draws them (seen
  // 2026-10-31); a 16px one in a comment box's row (seen 2026-11-13).
  const glyph = row ? "small" : "regular";
  const markButton = (mark: Exclude<SelectionToolbarMark, "link">, Glyph: typeof BoldIcon) => (
    <button type="button" key={mark} className={styles.tbBtn}
      aria-label={markLabels[mark]} aria-pressed={activeMarks[mark]} disabled={offMarks?.[mark] || undefined}
      onClick={() => apply(mark)} title={markTips[mark][0]} data-tip-keys={markTips[mark][1]}><Glyph size={glyph} aria-hidden="true" /></button>
  );
  const colorButton = (
    <button type="button" ref={colorButtonRef} className={`${styles.tbBtn} ${styles.tbColorBtn}`}
      aria-label={t("selectionToolbar:colorButton.ariaLabel", { color: activeColorLabel })}
      aria-haspopup="menu" aria-expanded={colorsOpen}
      onClick={() => { closeLinkEditor(false); closeTurn(false); if (colorsOpen) closeColors(false); else setColorsOpen(true); }}
      title={t("selectionToolbar:tips.color")} data-tip-keys={shortcutTipKeys("blockColor")}>
      <span className={styles.tbColorButtonMark} data-color={toolbarBackgroundToken ?? toolbarColorToken} aria-hidden="true">
        {toolbarBackgroundToken && toolbarColorToken !== "default"
          ? <span style={{ color: `var(--c-${toolbarColorToken})` }}>A</span> : "A"}</span>
    </button>
  );
  // A comment box's row draws its link as 🔗▾, the whole of it opening the
  // link picker, as Notion's (seen 2026-11-13).
  const linkButton = (
    <button type="button" ref={linkButtonRef} className={`${styles.tbBtn} ${styles.tbLinkBtn}`}
      aria-label={t("selectionToolbar:marks.link")} aria-haspopup="dialog" aria-expanded={linkOpen}
      aria-pressed={activeMarks.link} disabled={offMarks?.link || undefined} onClick={() => apply("link")} title={t("selectionToolbar:tips.link")}
      data-tip-keys={shortcutTipKeys("addLink")}>
      <LinkIcon size={glyph} aria-hidden="true" />
      {row && <ChevronDown className={styles.tbLinkCaret} size="micro" aria-hidden="true" />}
    </button>
  );
  const showMore = !commentOnly && !row && blockActions && Boolean(openBlockMenu);
  const commentable = blockActions || Boolean(props.commentAction);
  const showActions = commentable || Boolean(suggesting);
  const equationButton = (makeEquation || inertEquation) && <button type="button" className={styles.tbBtn} aria-label={t("selectionToolbar:marks.equation")}
    title={t("selectionToolbar:tips.equation")} data-tip-keys={shortcutTipKeys("inlineEquation")}
    disabled={suggesting?.active || !makeEquation} onClick={makeEquation}>
    <InlineEquationIcon size={glyph} aria-hidden="true" />
  </button>;

  return (
    <div ref={toolbarRef} className={`${styles.selToolbar} ${row ? styles.selToolbarRow : ""}`}
      style={{ ...popoverPlacementStyle(toolbarPlace), width: row ? undefined : TOOLBAR_WIDTH,
        ...(props.linkOnly ? { visibility: "hidden", pointerEvents: "none" } : {}) }}
      onMouseDown={(event) => event.preventDefault()}>
      <div {...toolbarProps} ref={actionsRef} style={{ display: "contents" }}>
      {row && <div className={styles.tbRow}>
        {markButton("bold", BoldIcon)}
        {markButton("italic", ItalicIcon)}
        {markButton("underline", UnderlineIcon)}
        {markButton("strikethrough", StrikethroughIcon)}
        {markButton("code", CodeIcon)}
        {equationButton}
        {linkButton}
        {colorButton}
      </div>}
      {showTurn && <button type="button" ref={turnButtonRef} className={`${styles.tbBtn} ${styles.tbTurn}`}
        aria-label={t("selectionToolbar:turnInto.label")} aria-haspopup="menu" aria-expanded={turnOpen}
        onClick={() => { setTurnByPointer(false); openTurn(); }} onPointerEnter={turnHover(true)} onPointerLeave={turnHover(false)}
        title={t("selectionToolbar:turnInto.label")}>
        <span className={styles.tbTurnGlyph} aria-hidden="true"><BlockIcon type={shownTurn.type} glyph={shownTurn.glyph} size="regular" /></span>
        <span className={styles.tbTurnLabel}>{t(severalBlocks?.mixed ? "selectionToolbar:turnInto.multipleTypes"
          : shownTurn.type === "paragraph" ? "selectionToolbar:turnInto.currentText" : shownTurn.label)}</span>
        <span className={styles.tbTurnCaret} aria-hidden="true"><ChevronRight size="small" /></span>
      </button>}
      {showTurn && <div className={styles.tbLine} aria-hidden="true" />}
      {!commentOnly && !row && <div className={styles.tbRow}>
        {colorButton}
        {markButton("bold", BoldIcon)}
        {markButton("italic", ItalicIcon)}
        {markButton("underline", UnderlineIcon)}
        {clearFormatting && <button type="button" className={`${styles.tbBtn} ${styles.tbClear}`} aria-label={t("selectionToolbar:clearFormatting")}
          title={t("selectionToolbar:clearFormatting")} data-tip-keys={shortcutTipKeys("clearFormatting")}
          disabled={suggesting?.active} onClick={clearFormatting}>
          <ClearFormattingIcon size="regular" aria-hidden="true" />
        </button>}
      </div>}
      {!commentOnly && !row && <div className={styles.tbRow}>
        {linkButton}
        {markButton("strikethrough", StrikethroughIcon)}
        {markButton("code", CodeIcon)}
        {equationButton}
        {(showMore || props.inertMore) && <button type="button" className={styles.tbBtn} aria-label={t("selectionToolbar:more")}
          aria-haspopup={showMore ? "menu" : undefined} disabled={!showMore} onClick={openBlockMenu}>
          <DotsHorizontal size="small" aria-hidden="true" />
        </button>}
      </div>}
      {showActions && !commentOnly && !row && <div className={styles.tbLine} aria-hidden="true" />}
      {showActions && <div className={styles.tbRow}>
        {commentable && <button type="button" className={`${styles.tbBtn} ${styles.tbComment}`}
          aria-label={t("selectionToolbar:comment.label")} onClick={commentSelection} title={t("selectionToolbar:comment.label")}>
          <CommentIcon size="regular" aria-hidden="true" /><span>{t("selectionToolbar:comment.label")}</span>
        </button>}
        {/* The reaction sits by the suggesting pencil, as Notion's (seen 2026-10-31). */}
        <span className={styles.tbSpacer} />
        {reactToSelection && <button type="button" className={styles.tbBtn}
          aria-label={t("selectionToolbar:reaction")} title={t("selectionToolbar:tips.reaction")}
          onClick={(event) => reactToSelection(event.currentTarget)}>
          <AddReactionIcon size="regular" aria-hidden="true" />
        </button>}
        {suggesting && <button type="button" className={styles.tbBtn} aria-label={t("selectionToolbar:suggestEdits")}
          aria-pressed={suggesting.active} title={t("selectionToolbar:tips.suggest")} data-tip-keys={shortcutTipKeys("suggestEdits")}
          onClick={() => suggesting.onChange(!suggesting.active)}>
          <SuggestEditIcon size="regular" aria-hidden="true" />
        </button>}
      </div>}
      </div>
      {turnOpen && <><div {...turnCollection.menuProps} ref={turnMenuRef} className={styles.tbTurnMenu}
        onPointerEnter={turnHover(true)} onPointerLeave={turnHover(false)}
        style={{ ...popoverPlacementStyle(turnMenu), width: TURN_MENU_WIDTH }}>
        {turnChoices.map(choice => <NativeMenuItem key={choice.key} state={turnCollection.state} itemKey={choice.key}
          className={styles.tbTurnItem} data-turn-menu-item={choice.key} role="menuitemradio" aria-checked={turnChecked(choice)}
          buttonRef={choice.placeIn ? pageInRef : undefined}>
          {({ labelProps }) => <><span className={styles.tbTurnGlyph} aria-hidden="true"><BlockIcon type={choice.placeIn ? "link_to_page" : choice.type} size="regular" /></span>
            <span {...labelProps}>{blockTurnChoiceLabel(choice)}</span>
            {turnChecked(choice) && <CheckIcon className={styles.colorCheck} size="small" aria-hidden="true" />}
            {choice.placeIn && <ChevronRight className={styles.tbTurnChevron} size="small" aria-hidden="true" />}</>}
        </NativeMenuItem>)}
      </div><TurnRowTip menuRef={turnMenuRef} choice={turnChoices.find(choice => choice.key === turnCollection.state.selectionManager.focusedKey)} place={turnMenu} /></>}
      {colorsOpen && <div {...colorCollection.menuProps} ref={colorMenuRef} className={styles.tbColorMenu}
        style={{ ...popoverPlacementStyle(colorMenu), width: COLOR_MENU_WIDTH }}>
        {recent.length ? colorGroup(t("selectionToolbar:colors.recent"), colorItems.slice(0, recent.length)) : null}
        {colorGroup(t("selectionToolbar:colors.text.heading"), colorItems.slice(recent.length, recent.length + TEXT_COLORS.length))}
        {colorGroup(t("selectionToolbar:colors.background.heading"), colorItems.slice(recent.length + TEXT_COLORS.length))}
      </div>}
      {linkOpen && <SelectionToolbarLinkDialog dialogRef={toolbarLinkMenuRef} placed={Boolean(linkMenu)} left={linkLeft}
        onEnter={() => setLinkLeft(false)}
        selectLinkOnOpen={selectLinkOnOpen} inputRef={linkInputRef} style={{ ...popoverPlacementStyle(linkMenu), width: LINK_MENU_WIDTH, visibility: "visible" }}>
        <SelectionToolbarLinkPicker inputRef={linkInputRef} value={linkValue} onValue={setLinkValue} source={linkPages}
          popoverRef={toolbarLinkMenuRef} onClose={focusText ? undefined : () => closeLinkEditor(true)}
          onPage={(target) => applyLinkValue(pageHref(target.id))} onAddress={() => applyLinkValue()} />
        {hadLinkOnOpen && <div className={styles.tbLinkActions}>
        {hadLinkOnOpen && <button type="button" className={styles.tbLinkIconButton} onClick={openCurrentLink}
          disabled={!currentLinkUrl} aria-label={t("selectionToolbar:linkEditor.open")}
          title={t("selectionToolbar:linkEditor.open")}><OpenInNew size="small" aria-hidden="true" /></button>}
        {hadLinkOnOpen && <button type="button" className={styles.tbLinkIconButton}
          onClick={() => void copyCurrentLink()} disabled={!currentLinkUrl}
          aria-label={linkCopied ? t("selectionToolbar:linkEditor.copiedLink") : t("selectionToolbar:linkEditor.copy")}
          title={linkCopied ? t("selectionToolbar:linkEditor.copied") : t("selectionToolbar:linkEditor.copy")}>
          <Copy size="small" aria-hidden="true" />
        </button>}
        {hadLinkOnOpen && <button type="button" className={styles.tbLinkRemove} onClick={removeLink}>
          {t("selectionToolbar:linkEditor.remove")}
        </button>}
        </div>}
      </SelectionToolbarLinkDialog>}
    </div>
  );
}
