import { type MouseEvent as ReactMouseEvent, useEffect, useRef } from "react";
import { flushSync } from "react-dom";
import { childrenScope, occurrenceBlockId } from "./blockOccurrences";
import { editorCommandOwner, editorRootOf } from "./editorScope";
import { inlineStartX } from "@/lib/readingDirection";
import { phoneScreen } from "@/lib/screenKind";
import { pressedPoint } from "@/lib/pressedPoint";

/**
 * Keyboard shortcuts and the slash menu reach the parts of the editor that
 * own what they do: a block's handle opens its menu, color menu, move dialog
 * or comments, or duplicates, deletes or copies a link to its block, and the
 * selection toolbar formats, links, colors or comments on selected text.
 */
export type EditorBlockCommand = "menu" | "turn" | "color" | "colorMenu" | "move" | "comment" | "duplicate" | "delete"
  | "deleteToLineAbove" | "copyLink"
  | "imageFullScreen" | "imageDownload" | "imageCrop" | "mediaOriginal" | "mediaReplace" | "caption" | "rename" | "unsyncAll"
  | "lineBelow" | "present" | "imageLink";
export type EditorSelectionCommand = "link" | "comment" | "strikethrough" | "code" | "color" | "equation" | "clear";

const BLOCK_COMMAND = "leafen:editor-block-command";
const SELECTION_COMMAND = "leafen:editor-selection-command";

/** Where a block menu opens: at a point, beside the handle that opened it, or
 * from the keyboard at the block's box (its text 6px out, a simple table's
 * table) — the box's `left` is where the menu starts, its right edge where the
 * screen reads from the right (lib/readingDirection `inlineStartX`). */
export type EditorBlockMenuAnchor = { x: number; y: number; pressed?: boolean; handle?: { top: number; bottom: number; left: number; right: number };
  box?: { top: number; bottom: number; left: number };
  /** From a touch screen's editing tools: the menu is the phone's sheet, in the middle of a wider screen (TouchSheetLayout). */
  sheet?: boolean };

type BlockCommandDetail = { blockId: string; command: EditorBlockCommand; anchor?: EditorBlockMenuAnchor;
  owner: Element | null; occurrenceId?: string; handled: boolean };
type SelectionCommandDetail = { command: EditorSelectionCommand; handled: boolean };

/**
 * Where the "⋯" on a block's hover tools (picture, video, embed) opens the
 * block's menu: its top left at the pointer that pressed it, as Notion's (seen
 * 2026-11-06 on a picture's and a Miro board's tools: pressed 4px and 21px down
 * the 24px button, the menu began there). From the keyboard, under the button.
 */
export function blockToolMenuAnchor(event: ReactMouseEvent<HTMLElement>, gap = 4): EditorBlockMenuAnchor {
  if (event.detail > 0) return pressedPoint(event);
  const box = event.currentTarget.getBoundingClientRect();
  return { x: inlineStartX(box, 0, event.currentTarget), y: box.bottom + gap };
}

/**
 * A block's own "⋯" among its tools, a synced block's head and the selection
 * toolbar: the block is chosen as its menu opens at the press, as Notion's
 * (seen 2026-11-06: an image's and a code block's, chosen still once the menu
 * closes, so a delete then ⌘Z gives it back chosen; 2026-11-18: a synced
 * block's head and the selection toolbar's "⋯", still chosen after Escape).
 * `source` is where in the editor the block stands — the button, or for a
 * toolbar drawn outside the editor the chosen words — so the editor and the
 * occurrence (a synced copy drawing it again) are the ones pressed.
 */
export function openBlockToolMenu(blockId: string, event: ReactMouseEvent<HTMLElement>, source: Element = event.currentTarget) {
  // A phone's opens the block's sheet and leaves it unchosen, so nothing stays
  // chosen once the sheet closes, nor comes back chosen after a reload, as
  // Notion's (seen 2026-11-15 on an A90 in Chrome: a video's sheet swiped
  // away, and its "바꾸기" window closed, the video not chosen).
  if (!phoneScreen()) requestEditorBlockSelection(blockId, false, editorRootOf(source) ? source : undefined);
  requestEditorBlockCommand(blockId, "menu", blockToolMenuAnchor(event), source);
}

export function requestEditorBlockCommand(blockId: string, command: EditorBlockCommand, anchor?: EditorBlockMenuAnchor, source?: Element) {
  const owner = source ? editorRootOf(source) : editorCommandOwner();
  const node = window.getSelection()?.anchorNode;
  const element = source ?? (document.activeElement instanceof Element && document.activeElement.closest("[data-native-occurrence]")
    ? document.activeElement : node instanceof Element ? node : node?.parentElement);
  const occurrence = element?.closest<HTMLElement>("[data-native-occurrence]");
  const id = occurrence?.dataset.nativeOccurrence;
  const occurrenceId = id && editorRootOf(occurrence!) === owner && (id === blockId || id.endsWith(`/${blockId}`)) ? id : undefined;
  const detail: BlockCommandDetail = { blockId, command, anchor, owner, occurrenceId, handled: false };
  window.dispatchEvent(new CustomEvent<BlockCommandDetail>(BLOCK_COMMAND, { detail }));
  return detail.handled;
}

// Each block's handlers, in the order they came, behind one listener (a long
// page's every block added a window listener of its own, and each command
// asked them all).
type BlockCommandEntry = {
  handler: { current: (command: EditorBlockCommand, anchor?: EditorBlockMenuAnchor) => void };
  origin: { current?: () => Element | null };
};
const blockCommandHandlers = new Map<string, Set<BlockCommandEntry>>();
let blockCommandsHeard = false;

/**
 * What a block command opens is drawn, and takes the focus, before the
 * key's event ends, as a selection command's (useEditorSelectionCommand):
 * heard through this event, React put it off to a later task, and keys typed
 * on the heels of ⌘⇧P or ⌘/ went into the line (2026-11-18: "xyz" typed
 * right after ⌘⇧P left "x" in the line and "yz" in the move window's search,
 * as ⌘/'s menu; ⌘P's search, opened from the key itself, took all three).
 * A sleeping handle (BlockHandleGate) woken by the command does it as it
 * comes up, in the same pass, not in an effect after: an effect's updates
 * wait for a later task, and the letter after ⌘⇧P still went into the line
 * wherever the pointer was away from the block's gutter (2026-11-18).
 */
function hearBlockCommands() {
  if (blockCommandsHeard) return;
  blockCommandsHeard = true;
  window.addEventListener(BLOCK_COMMAND, (event) => {
    const detail = (event as CustomEvent<BlockCommandDetail>).detail;
    if (!detail) return;
    for (const entry of [...(blockCommandHandlers.get(detail.blockId) ?? [])]) {
      if (detail.handled) return;
      const control = entry.origin.current?.();
      if (detail.owner && editorRootOf(control ?? null) !== detail.owner) continue;
      if (detail.occurrenceId && control?.closest<HTMLElement>("[data-native-occurrence]")?.dataset.nativeOccurrence !== detail.occurrenceId) continue;
      detail.handled = true;
      flushSync(() => entry.handler.current(detail.command, detail.anchor));
    }
  });
}

export function useEditorBlockCommand(
  blockId: string,
  onCommand: (command: EditorBlockCommand, anchor?: EditorBlockMenuAnchor) => void,
  element?: () => Element | null,
) {
  const handler = useRef(onCommand);
  handler.current = onCommand;
  const origin = useRef(element); origin.current = element;
  useEffect(() => {
    hearBlockCommands();
    const entry: BlockCommandEntry = { handler, origin };
    const entries = blockCommandHandlers.get(blockId) ?? new Set<BlockCommandEntry>();
    entries.add(entry);
    blockCommandHandlers.set(blockId, entries);
    return () => {
      entries.delete(entry);
      if (!entries.size && blockCommandHandlers.get(blockId) === entries) blockCommandHandlers.delete(blockId);
    };
  }, [blockId]);
}

/** Returns whether a selection toolbar took the command. */
export function requestEditorSelectionCommand(command: EditorSelectionCommand) {
  const detail: SelectionCommandDetail = { command, handled: false };
  window.dispatchEvent(new CustomEvent<SelectionCommandDetail>(SELECTION_COMMAND, { detail }));
  return detail.handled;
}

/**
 * onCommand returns whether it acted on the current text selection. What it
 * opens is drawn, and takes the focus, before the key's event ends: heard
 * through this event, React would put it off to a later task, and keys typed
 * on the heels of ⌘K went to the text over the chosen words (2026-11-15:
 * a letter 6 ms after ⌘K took the words' place).
 */
export function useEditorSelectionCommand(onCommand: (command: EditorSelectionCommand) => boolean) {
  const handler = useRef(onCommand);
  handler.current = onCommand;
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<SelectionCommandDetail>).detail;
      if (detail && !detail.handled && flushSync(() => handler.current(detail.command))) detail.handled = true;
    };
    window.addEventListener(SELECTION_COMMAND, listener);
    return () => window.removeEventListener(SELECTION_COMMAND, listener);
  }, []);
}

const SELECT_BLOCK = "leafen:editor-select-block";
type SelectBlockDetail = { blockId: string; owner: Element | null; keepChosen: boolean; at?: string; handled: boolean };

/**
 * Where a block is chosen: in the synced copy that draws it again, where it
 * was pressed or the caret is beside it — a block the slash menu made in a
 * copy on its original's page (2026-11-18: "/탭" Enter there chose the tabs
 * with the original's frame lit) —, or undefined where it stands itself.
 */
function choiceOccurrence(blockId: string, source?: Element) {
  const node = source ?? window.getSelection()?.anchorNode;
  const element = node instanceof Element ? node : node?.parentElement;
  const at = element?.closest<HTMLElement>("[data-native-occurrence]")?.dataset.nativeOccurrence;
  if (!at) return undefined;
  if (occurrenceBlockId(at) === blockId) return at.includes("/") ? at : undefined;
  const scope = childrenScope(at);
  return scope ? `${scope}/${blockId}` : undefined;
}

/**
 * Asks the editor that has the keyboard to select a block, as Escape does:
 * the caret leaves the text and the keys act on the block. Kept chosen, the
 * blocks chosen with it stay so.
 */
export function requestEditorBlockSelection(blockId: string, keepChosen = false, source?: Element) {
  // A block named from outside the editors (a link arrived at) is its own editor's to choose.
  const detail: SelectBlockDetail = { blockId, owner: source ? editorRootOf(source) : editorCommandOwner(), keepChosen,
    at: choiceOccurrence(blockId, source), handled: false };
  window.dispatchEvent(new CustomEvent<SelectBlockDetail>(SELECT_BLOCK, { detail }));
  return detail.handled;
}

/** An editor's answer to `requestEditorBlockSelection`: true when it selected the block (`at`, where it was chosen, if in a copy). */
export function listenForBlockSelection(select: (blockId: string, owner: Element | null, keepChosen: boolean, at?: string) => boolean) {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<SelectBlockDetail>).detail;
    if (detail && !detail.handled) detail.handled = select(detail.blockId, detail.owner, detail.keepChosen, detail.at);
  };
  window.addEventListener(SELECT_BLOCK, listener);
  return () => window.removeEventListener(SELECT_BLOCK, listener);
}
