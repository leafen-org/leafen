import type { OverlayLayerOptions } from "./overlayLayer";
import { holdPageUnder } from "./pageHoverShield";
import { phoneScreen } from "./screenKind";

/**
 * The kinds of floating layer, named as WAI-ARIA and React Aria name the pattern, each with the options every window
 * of it takes (useOverlayLayer's `kind`). A site names its kind and passes only its own handlers (`onEscape`,
 * `onOutsidePress`…), its parts (`inside`, `modalOwned`, `returnFocusRef`) and the exceptions allowed it
 * (scripts/check-overlay-kinds.mjs); an option it passes itself wins over its kind's.
 *
 * - dialog — a modal window (aria-modal; React Aria Modal + Dialog): the page inert, Tab kept inside, Escape closes,
 *   the focus enters at its first control and goes back to its opener.
 * - alertdialog — a question asked before an action (React Aria AlertDialog): as a dialog, the focus on its answer.
 * - menu — a button's menu (menu button; React Aria MenuTrigger + Popover + Menu): the page under it shielded
 *   (pointer, press and wheel), a press beside it only closes it, Escape closes, the focus back to its button.
 * - submenu — a list beside a menu's row (React Aria SubmenuTrigger): as a menu, a press beside both closing both.
 * - picker — a listbox popover or a small window with a field (React Aria ComboBox / Select popover, Popover +
 *   Dialog): as a menu, its field taking the keys.
 * - popover — a database bar's window (React Aria Popover isNonModal): no shield, a press beside closes it and does
 *   its own work, the focus back to its pill or button.
 * - caret-menu — a list at the text's caret ("/", "@", "+", "[[", ":"; React Aria useComboBox, the editor its
 *   input): the focus stays at the caret, a press beside closes it and goes on, the wheel over the page is held and
 *   the blocks' handles stay away (":" lets them come — `handlesUnder`), as Notion's (2026-11-26, two tabs).
 * - card — a non-modal card by the text (a comment's, a discussion's; React Aria Popover isNonModal): a press beside
 *   closes it and goes on.
 * - side-peek — a panel beside the page (a side peek, a docked panel): a press beside does its work, then closes it.
 * - panel — a non-modal panel only Escape or its own controls close (the trash, the inbox over the page); the focus
 *   goes back to what opened it, ringed after Escape (Notion's trash, 2026-11-26, two tabs).
 * - hovercard — a card shown while the pointer rests on a mention: Escape goes on to what is under it.
 * - tooltip — a hover tip (React Aria Tooltip): takes no key or press.
 * - toolbar — the text selection's toolbar (React Aria Toolbar): follows the chosen text, Escape is the text's, the
 *   blocks' handles stay away while it shows, and Tab in the text (an indent) takes it away, as Notion's (2026-11-26,
 *   two tabs).
 * - inner — part of a layer that holds it (a list beside a menu's row its holder closes), with no close of its own.
 */
export type OverlayKind = "dialog" | "alertdialog" | "menu" | "submenu" | "picker" | "popover" | "caret-menu" | "card"
  | "side-peek" | "panel" | "hovercard" | "tooltip" | "toolbar" | "inner";

type KindOptions = Partial<Omit<OverlayLayerOptions, "ref" | "kind">>;

const KINDS: Record<OverlayKind, KindOptions> = {
  dialog: { modal: true },
  alertdialog: { modal: true },
  menu: { floats: true, restoreFocus: true },
  submenu: { floats: true, restoreFocus: true, closesHost: true },
  picker: { floats: true, restoreFocus: true },
  popover: { pressGoesOn: true, restoreFocus: true },
  "caret-menu": { pressGoesOn: true },
  card: { pressGoesOn: true },
  "side-peek": { pressGoesOn: true, closesAfterClick: true, restoreFocus: false },
  panel: { restoreFocus: true },
  hovercard: { floats: true, escapeGoesOn: true },
  tooltip: { passive: true },
  toolbar: { followsText: true, floats: true, restoreFocus: true },
  inner: { floats: true },
};

/** Exceptions a site may pass to its kind's ways, each allowed it in scripts/check-overlay-kinds.mjs. */
export type KindExceptions = {
  /** A menu at the caret under which the blocks' handles still come to the pointer (":"'s emoji menu). */
  handlesUnder?: boolean;
  /** Closed by a press beside it, it gives the focus back ringed, as after a key (Notion's export window). */
  ringsAfterPress?: boolean;
};

/** What a layer's kind holds of the page under it while it is drawn (pageHoverShield.ts); returns the release. */
export function holdUnderKind(layer: object, options: Pick<OverlayLayerOptions, "kind" | "handlesUnder">): (() => void) | undefined {
  if (options.kind === "caret-menu" && !phoneScreen()) return holdPageUnder(layer, { wheel: true, handles: options.handlesUnder !== true });
  if (options.kind === "toolbar" && !phoneScreen()) return holdPageUnder(layer, { wheel: false, handles: true });
  return undefined;
}

// A layer closed with its ring asked for (ringsAfterPress) hands it to the one closing with it that gives the focus
// back: the export window opened from the page's "⋯" menu, which stays open under it and gives "⋯" back as it goes.
let ringHandedAt = 0;
/** A layer asking for its ring after a press closes (useOverlayLayer). */
export function handRingOn() { ringHandedAt = Date.now(); }
/** Whether a layer that closed a moment ago asked for its ring after a press; taken once. */
export function takeHandedRing() {
  const handed = Date.now() - ringHandedAt < 1000;
  ringHandedAt = 0;
  return handed;
}

/** A layer's options with its kind's under them: what the site passes itself wins. */
export function withKind<T extends { kind?: OverlayKind }>(options: T): T {
  return options.kind ? { ...KINDS[options.kind], ...options } : options;
}
