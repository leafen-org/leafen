import * as Y from "yjs";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import {
  backspaceSuggestion,
  formatSuggestionMarks,
  forwardDeleteSuggestion,
  suggestingTextRuns,
  typedMarks,
  type SuggestingTextRun,
} from "./suggestingInputModel";
import { pressMark } from "@/lib/ownSuggestionEvents";
import type { SuggestedEdit, SuggestedReceipt } from "./suggestingMode";

type Position = Y.RelativePosition | Promise<Y.RelativePosition | null>;
export type SuggestedCharacter = { start: Y.RelativePosition; end: Y.RelativePosition };
type OwnedSelection = { owned?: Promise<(SuggestedCharacter | null)[]> };

// What the person asked for, held at positions in the collaborative text so
// that suggestions still on their way move them along.
type Intent =
  | ({ kind: "insert"; text: string; start: Position; end: Position; composed: boolean; marks?: Record<string, string>;
    inserted(chars: SuggestedCharacter[]): void } & OwnedSelection)
  | ({ kind: "delete"; start: Position; end: Position } & OwnedSelection)
  | { kind: "backspace" | "forwardDelete"; at: Position }
  | ({ kind: "format"; start: Position; end: Position; mark: string } & OwnedSelection)
  | ({ kind: "formatWith"; start: Position; end: Position; marks: Record<string, boolean | string> } & OwnedSelection)
  | {
    kind: "block"; at: Position; blockType: string; newBlockId: string; firstChild: boolean;
    splitType: string | null; picked: boolean; landed(came: boolean): void;
  }
  | { kind: "turn"; blockType: string; stay: boolean; landed(came: boolean): void }
  | { kind: "remove"; blockIds: readonly string[] };
// Each is asked for at a press count (pressMark): its suggestion's card then
// opens only if no press came since (ownSuggestionEvents.ts).
type MarkedIntent = Intent & { askedAtPress?: number };

/** Positions in a block's text for what the person selected. */
export type SuggestingSelection = { start: Position; end: Position; collapsed: boolean } & OwnedSelection;

// A suggestion reaches the page's document through its room; one that does
// not arrive in time lets the next go ahead.
const CHANGE_WAIT_MS = 4000;

/**
 * A block's suggesting-mode input, in order: each edit goes to the page's
 * suggestion route with the text as the document holds it, and the next
 * waits until the document holds the change. Edits are placed by positions
 * taken when the person made them, so fast typing lands in order.
 */
export class SuggestingInputQueue {
  private readonly intents: MarkedIntent[] = [];
  // When the intent sent now was asked for (pressMark).
  private asked: number | undefined;
  private flight: Promise<void> = Promise.resolve();
  private running = false;
  /** Composed text the block shows that the document does not hold yet. */
  composedAhead = 0;

  constructor(private readonly options: {
    document: Y.Doc;
    blockId: string;
    suggest(edit: SuggestedEdit): Promise<SuggestedReceipt | void>;
    /** The queue is empty; the caret goes here when an edit placed it. */
    settled(caret: number | null): void;
    /** A new block was suggested; the caret goes to `caretBlockId` once it is there. */
    blockSuggested?(caretBlockId: string, newBlockId: string): void;
    /** What the first edit waits for: a block drawn before it was there (newBlock) coming. */
    after?: Promise<unknown>;
    changeWaitMs?: number;
  }) {
    this.gate = options.after;
  }
  private gate: Promise<unknown> | undefined;

  get busy() {
    return this.running;
  }

  /** Resolves once every edit so far has been suggested. */
  get drained() {
    return this.flight;
  }

  private text() {
    return this.options.document.getText(BLOCK_TEXT_ROOT_PREFIX + this.options.blockId);
  }

  private relative(index: number) {
    return Y.createRelativePositionFromTypeIndex(this.text(), index);
  }

  private async absolute(position: Position) {
    const anchor = await position;
    const value = anchor && Y.createAbsolutePositionFromRelativePosition(anchor, this.options.document);
    if (!value || value.type !== this.text()) throw new RangeError("Suggested input anchor is unavailable.");
    return value.index;
  }

  private runs() {
    return suggestingTextRuns(this.text().toDelta());
  }

  /** The selection in the document's text, from offsets in the block as it shows now. */
  select(start: number, end: number): SuggestingSelection {
    const ahead = this.composedAhead;
    return {
      start: this.relative(Math.max(0, start - ahead)),
      end: this.relative(Math.max(0, end - ahead)),
      collapsed: start === end,
    };
  }

  /** `marks` are the text's own (a mention's), in place of those it would take where it goes. */
  insert(text: string, selection: SuggestingSelection, composed = false, marks?: Record<string, string>) {
    if (composed) this.composedAhead += text.length;
    return new Promise<SuggestedCharacter[]>(inserted => this.push({ kind: "insert", text,
      start: selection.start, end: selection.end, owned: selection.owned, composed, marks, inserted }));
  }

  delete(selection: SuggestingSelection, direction: "backward" | "forward") {
    if (!selection.collapsed) this.push({ kind: "delete", start: selection.start, end: selection.end, owned: selection.owned });
    else this.push({ kind: direction === "backward" ? "backspace" : "forwardDelete", at: selection.start });
  }

  format(selection: SuggestingSelection, mark: string) {
    if (!selection.collapsed) this.push({ kind: "format", start: selection.start, end: selection.end, owned: selection.owned, mark });
  }

  /** Suggests exact formatting, as the toolbar's color and link do. */
  formatWith(selection: SuggestingSelection, marks: Record<string, boolean | string>) {
    if (!selection.collapsed) this.push({ kind: "formatWith", start: selection.start, end: selection.end, owned: selection.owned, marks });
  }

  /**
   * Enter at the block's end suggests a new block after it, or as its first
   * child; elsewhere in its text it splits the block, the new block of
   * splitType, and suggests the break. A block a menu picked (`picked`) is a
   * suggestion of its own after the block, no break (page_suggestion_picked.go).
   * Resolves whether the new block came: the page holds it, and its text
   * what the break moved there.
   */
  newBlock(
    selection: SuggestingSelection, blockType: string, newBlockId: string, firstChild = false,
    splitType: string | null = null, picked = false,
  ) {
    if (!selection.collapsed) return Promise.resolve(false);
    return new Promise<boolean>(landed => this.push({ kind: "block", at: selection.start, blockType, newBlockId, firstChild, splitType, picked, landed }));
  }

  /**
   * Turns the block, the person's own suggested one, into another kind of
   * text block, a suggestion of its own, or within the suggestion it is in
   * (`stay`, page_suggestion_turns.go). Resolves whether it turned: a block
   * that stands, or someone else's suggestion, is refused.
   */
  turn(blockType: string, stay = false) {
    return new Promise<boolean>(landed => this.push({ kind: "turn", blockType, stay, landed }));
  }

  /** Suggests the block away, with the blocks after it named (page_suggestion_removals.go), after what was asked before. */
  remove(blockIds: readonly string[] = []) {
    this.push({ kind: "remove", blockIds });
  }

  private push(intent: Intent) {
    this.intents.push(Object.assign(intent, { askedAtPress: pressMark() }));
    if (!this.running) this.flight = this.drain();
  }

  /** The edit to the page's route, with when its intent was asked for. */
  private suggestEdit(edit: SuggestedEdit) {
    return this.options.suggest({ ...edit, askedAtPress: this.asked });
  }

  private async drain() {
    this.running = true;
    let caret: number | null = null;
    try {
      if (this.gate) { await this.gate.catch(() => undefined); this.gate = undefined; }
      while (this.intents.length > 0) {
        const intent = this.intents.shift()!;
        this.asked = intent.askedAtPress;
        // A refused suggestion leaves the text as the document holds it; the
        // page reports why.
        caret = await this.run(intent).catch(() => {
          if (intent.kind === "insert") intent.inserted([]);
          if (intent.kind === "block" || intent.kind === "turn") intent.landed(false);
          return null;
        }) ?? caret;
      }
    } finally {
      this.running = false;
      this.options.settled(caret);
    }
  }

  private async send(edit: Omit<SuggestedEdit, "blockId" | "expectedText">, arrived?: (comment: SuggestedReceipt | void) => boolean) {
    const shared = this.text();
    let finish = () => {}, replied = false, observed = false;
    let comment: SuggestedReceipt | void;
    const present = () => arrived ? arrived(comment) : !comment
      || this.runs().some(run => run.attributes.suggestionId === comment!.id) || edit.kind !== "insert" && observed;
    const observe = () => { observed = true; if (replied && present()) finish(); };
    const changed = new Promise<void>((resolve) => {
      const timer = setTimeout(() => finish(), this.options.changeWaitMs ?? CHANGE_WAIT_MS);
      finish = () => {
        clearTimeout(timer);
        shared.unobserve(observe);
        resolve();
      };
      shared.observe(observe);
    });
    try {
      comment = await this.suggestEdit({ blockId: this.options.blockId, expectedText: shared.toString(), ...edit });
      replied = true;
      if (comment ? present() : observed) finish();
    } catch (error) {
      finish();
      throw error;
    }
    await changed;
    if (comment && !present()) throw new RangeError("Suggested input has not reached the native document.");
    return comment;
  }

  private async run(intent: Intent): Promise<number | null> {
    switch (intent.kind) {
      case "insert": {
        // Typed over a selection, its words are replaced: one suggestion, the
        // new words before the old ones struck through, the caret after the
        // new words, as Notion's (page_suggestion_text.go). Words of the
        // selection apart from its first stretch (a peer's typing came between)
        // are suggested away alone.
        let replaced: { start: number; end: number } | null = null;
        if (intent.owned) {
          const chars = await intent.owned;
          const first = this.ownedRanges(chars)[0];
          if (first) {
            const firstChars = new Set(first.chars);
            await this.eachOwnedRange(Promise.resolve(chars.filter(char => !char || !firstChars.has(char))),
              range => this.deleteRange(range.start, range.end));
            replaced = this.ownedRanges(first.chars)[0] ?? null;
          }
        } else {
          const start = await this.absolute(intent.start), end = await this.absolute(intent.end);
          if (start < end) replaced = { start, end };
        }
        // The new words go in at `before`; `after` is the first character
        // after them that stays — past the selection, whose own suggested
        // words go and whose standing words stay struck after the new ones.
        const at = replaced ? replaced.start : await this.absolute(intent.end);
        const after = this.relative(replaced ? replaced.end : at);
        const before = Y.createRelativePositionFromTypeIndex(this.text(), at, -1);
        // Typed over words, the new ones take the first word's formatting.
        const marks = intent.marks ?? typedMarks(this.runs(), replaced ? at + 1 : at);
        const edit = replaced
          ? { kind: "replace" as const, offset: at, length: replaced.end - at, text: intent.text, marks }
          : { kind: "insert" as const, offset: at, text: intent.text, marks };
        const added = (run: SuggestingTextRun, result: SuggestedReceipt | void) =>
          run.attributes.suggestion === "insert" && (!result || run.attributes.suggestionId === result.id);
        const comment = await this.send(edit, result => {
          const left = Y.createAbsolutePositionFromRelativePosition(before, this.options.document)?.index;
          const right = Y.createAbsolutePositionFromRelativePosition(after, this.options.document)?.index;
          if (left === undefined || right === undefined) return false;
          const length = this.runs().filter(run => added(run, result))
            .reduce((sum, run) => sum + Math.max(0, Math.min(right, run.end) - Math.max(left, run.start)), 0);
          return length === intent.text.length;
        });
        const from = await this.absolute(before), to = await this.absolute(after);
        const ranges = (comment ? this.runs().filter(run => added(run, comment)) : [{ start: from, end: to }])
          .map(run => ({ start: Math.max(from, run.start), end: Math.min(to, run.end) })).filter(run => run.end > run.start);
        const chars = ranges.flatMap(range => Array.from({ length: range.end - range.start }, (_, index) => ({
          start: Y.createRelativePositionFromTypeIndex(this.text(), range.start + index, 0),
          end: Y.createRelativePositionFromTypeIndex(this.text(), range.start + index + 1, -1),
        })));
        intent.inserted(chars.length === intent.text.length ? chars : []);
        if (intent.composed) this.composedAhead = Math.max(0, this.composedAhead - intent.text.length);
        return replaced ? from + intent.text.length : to;
      }
      case "delete": {
        const start = await this.absolute(intent.start);
        const end = await this.absolute(intent.end);
        if (start >= end) return start;
        const before = this.relative(start);
        if (intent.owned) await this.eachOwnedRange(intent.owned, range => this.deleteRange(range.start, range.end));
        else await this.deleteRange(start, end);
        return await this.absolute(before);
      }
      case "backspace":
      case "forwardDelete": {
        const plan = (intent.kind === "backspace" ? backspaceSuggestion : forwardDeleteSuggestion)(
          this.text().toString(), this.runs(), await this.absolute(intent.at),
        );
        if (!plan.edit) return plan.caret;
        const after = this.relative(plan.caret);
        await this.deleteRange(plan.edit.offset, plan.edit.offset + plan.edit.length);
        return await this.absolute(after);
      }
      case "block": {
        const shared = this.text();
        const at = await this.absolute(intent.at);
        if (at < shared.length && intent.splitType) {
          const moved = at > 0;
          await this.suggestEdit({
            blockId: this.options.blockId, expectedText: shared.toString(), kind: "split", offset: at,
            newBlockId: intent.newBlockId, blockType: intent.splitType,
          });
          // The text after the break reaches the new block's text through the room.
          if (moved) await this.arrival(this.options.document.getText(BLOCK_TEXT_ROOT_PREFIX + intent.newBlockId), text => text.length > 0);
          intent.landed(true);
          // A break at the start makes the block before this one, and the
          // caret stays at the start of this one.
          this.options.blockSuggested?.(moved ? intent.newBlockId : this.options.blockId, intent.newBlockId);
          return null;
        }
        if (at !== shared.length) { intent.landed(false); return null; }
        await this.suggestEdit({
          blockId: this.options.blockId, expectedText: shared.toString(), kind: "block", offset: at,
          newBlockId: intent.newBlockId, blockType: intent.blockType,
          ...(intent.firstChild ? { firstChild: true } : {}), ...(intent.picked ? { picked: true } : {}),
        });
        intent.landed(true);
        this.options.blockSuggested?.(intent.newBlockId, intent.newBlockId);
        return null;
      }
      case "remove": {
        await this.suggestEdit({ blockId: this.options.blockId, expectedText: this.text().toString(), kind: "remove", offset: 0,
          ...(intent.blockIds.length ? { blockIds: [...intent.blockIds] } : {}) });
        return null;
      }
      case "turn": {
        await this.suggestEdit({ blockId: this.options.blockId, expectedText: this.text().toString(), kind: "turn", offset: 0, blockType: intent.blockType,
          ...(intent.stay ? { stay: true } : {}) });
        intent.landed(true);
        // The page reads the block's new kind, the caret staying at its start.
        this.options.blockSuggested?.(this.options.blockId, this.options.blockId);
        return null;
      }
      case "format":
      case "formatWith": {
        const start = await this.absolute(intent.start);
        const end = await this.absolute(intent.end);
        if (intent.owned) {
          const chars = await intent.owned;
          const ranges = this.ownedRanges(chars);
          const runs = this.runs().filter(run => ranges.some(range => run.start < range.end && range.start < run.end));
          const marks = intent.kind === "format" ? formatSuggestionMarks(runs, start, end, intent.mark) : intent.marks;
          await this.eachOwnedRange(Promise.resolve(chars), range => this.send({
            kind: "format", offset: range.start, length: range.end - range.start, marks,
          }));
        } else if (start < end) {
          await this.send({
            kind: "format", offset: start, length: end - start,
            marks: intent.kind === "format" ? formatSuggestionMarks(this.runs(), start, end, intent.mark) : intent.marks,
          });
        }
        return null;
      }
    }
  }

  /** Resolves once `text` holds what `holds` waits for, or the wait for a change runs out. */
  private arrival(text: Y.Text, holds: (text: Y.Text) => boolean) {
    if (holds(text)) return Promise.resolve();
    return new Promise<void>(resolve => {
      const done = () => { clearTimeout(timer); text.unobserve(observe); resolve(); };
      const observe = () => { if (holds(text)) done(); };
      const timer = setTimeout(done, this.options.changeWaitMs ?? CHANGE_WAIT_MS);
      text.observe(observe);
    });
  }

  private ownedRanges(chars: (SuggestedCharacter | null)[]) {
    const ranges: { start: number; end: number; chars: SuggestedCharacter[] }[] = [];
    for (const char of chars) {
      if (!char) throw new RangeError("Suggested draft character is unavailable.");
      const start = Y.createAbsolutePositionFromRelativePosition(char.start, this.options.document);
      const end = Y.createAbsolutePositionFromRelativePosition(char.end, this.options.document);
      if (!start || !end || start.type !== this.text() || end.type !== this.text()) throw new RangeError("Suggested draft range is unavailable.");
      if (start.index >= end.index) continue;
      const previous = ranges.at(-1);
      if (previous?.end === start.index) { previous.end = end.index; previous.chars.push(char); }
      else ranges.push({ start: start.index, end: end.index, chars: [char] });
    }
    return ranges;
  }

  private async eachOwnedRange(owned: Promise<(SuggestedCharacter | null)[]>, send: (range: { start: number; end: number }) => Promise<unknown>) {
    let remaining = await owned;
    while (remaining.length) {
      // Resolve again after every native acknowledgement. Peer characters
      // inserted inside the pending PM selection never join its owned range.
      const range = this.ownedRanges(remaining).at(-1);
      if (!range) return;
      await send(range);
      const sent = new Set(range.chars);
      remaining = remaining.filter(char => !char || !sent.has(char));
    }
  }

  private deleteRange(start: number, end: number) {
    const intervals = Array.from({ length: end - start }, (_, offset) => ({
      start: Y.createRelativePositionFromTypeIndex(this.text(), start + offset, 0),
      end: Y.createRelativePositionFromTypeIndex(this.text(), start + offset + 1, -1),
    }));
    return this.send({ kind: "delete", offset: start, length: end - start }, receipt => intervals.every(interval => {
      const from = Y.createAbsolutePositionFromRelativePosition(interval.start, this.options.document)?.index;
      const to = Y.createAbsolutePositionFromRelativePosition(interval.end, this.options.document)?.index;
      if (from === undefined || to === undefined) return false;
      return from === to || this.runs().some(run => run.start <= from && to <= run.end
        && run.attributes.suggestion === "delete" && (!receipt || run.attributes.suggestionId === receipt.id));
    }));
  }
}
