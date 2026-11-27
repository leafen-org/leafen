import type { Placement } from "@floating-ui/dom";
import type { PopoverAppear, PopoverOrigin } from "./popoverAppear";

// What a popover's place is asked by and given as (popoverPlacement.ts).
export type PopoverRect = { top: number; bottom: number; left: number; right: number; contextElement?: Element };
export type PopoverSize = { width: number; height: number };
export type PopoverViewport = { width: number; height: number };
export type PopoverSide = "below" | "above" | "right" | "left" | "point";
export type PopoverPlacement = { left: number; top: number; maxHeight?: number; maxWidth?: number; side: PopoverSide; zIndex?: number;
  /** Placed but not yet still since it opened (usePopoverPlacement): it takes no pointer. */
  settling?: true;
  /** The point of itself it grows from as it opens, by the side and edge its opener is on (popoverAppear.ts). */
  origin?: PopoverOrigin };

export type PopoverOptions = { margin?: number; cap?: number; heightStep?: { offset: number; step: number };
  /** Its distance from the window's top and foot, where that differs from its sides' (`margin`). */
  marginY?: number;
  /** Too tall for the room past its opener, it moves up over the opener to show whole, up to the window,
   * rather than shrinking there — Notion's long panels (seen 2026-11-02, 11-03: view settings 714 at 173..887). */
  rise?: boolean;
  /** Shown whole: over its opener when only that side has room for it, and with room on
   * neither, on its own side moved into the window over its opener — a block's add and
   * replace windows, as Notion's (seen 2026-11-06: under a 900 map, at the window's foot;
   * 2026-11-08: a select option's editor, 292..711 under its row at 419..447 in a 723 window). */
  whole?: boolean;
  /** With `whole` and room on neither side, on the side with more room, moved into the window —
   * a linked view's source list, as Notion's (seen 2026-11-08: 343 high in a 723 window, under
   * a tab row at 335 at 367..710, over one at 345 at 12..355). */
  roomier?: boolean;
  /** Keeps the side it is on while open, turning over only when what it holds no longer fits
   * there and fits the other side better — a cell's picker, as Notion's (seen 2026-11-08: a
   * multi-select's turned over its cell as values grew it past the room under, then stayed
   * over it, its foot on the cell, as typing shortened its list). */
  holdSide?: boolean;
  /** Chooses its side as though this tall, though drawn at its own height — a relation
   * cell's picker by its full 336, as Notion's (seen 2026-11-08: 284 high, it opened over
   * its cell with 335 under it and under the cell with 338). */
  sideBy?: number;
  /** Chooses its side once by this height and its gap, as a line's menus do: under its
   * opener where the room under it holds that, else over it where the room over it does,
   * else under it, the window's edge margins not counted; it keeps that side, cut to the
   * room there — Notion's "/", "@" and ":" (seen 2026-11-12 in a 723 window: "/북마크",
   * 113 high, under its line with 303 under and over it with 300, by its 40% cap and 12 gap).
   * By a touch screen's line (over its editing tools) the room is the window's, down to its
   * foot past what shows — under a phone's keyboard where the browser draws the page there —,
   * as Notion's phone "@" and "[[" (resolvePlacement). */
  lineSide?: number;
  /** How it comes on as it opens, where not as its kind says (popoverAppear.ts). */
  appear?: PopoverAppear };
/** `align` "center": its middle on its opener's (a line's window, as Notion's builder "관계형"). */
export type PlaceAroundOptions = PopoverOptions & { gap?: number; prefer?: "below" | "above"; align?: "start" | "end" | "center"; alignFallback?: boolean; minHeight?: number;
  /** Stays on its side when too tall for it, moved over its opener rather than turned over (a settings row's list, as Notion's). */
  keepSide?: boolean };
/** `prefer` is the side left to right: where the screen reads from the right, a popover beside its opener opens on the other. */
export type PlaceBesideOptions = PopoverOptions & { gap?: number; prefer?: "right" | "left"; offsetTop?: number };
export type PopoverRequest = { anchor: PopoverRect; placement: Placement; options: PopoverOptions; gap: number; offsetTop?: number; minHeight?: number; point?: boolean; alignFallback?: boolean; keepSide?: boolean;
  /** Placed by its top left, whichever way the screen reads (placeInWindow). */
  corner?: boolean };
