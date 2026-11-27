import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { usePagePresenceSnapshot } from "@/lib/pagePresenceStore";
import type { PagePresenceAwarenessView } from "@/lib/pagePresenceView";
import styles from "./PagePresenceFaces.module.css";
import { simpleTableGridBoxes } from "./simpleTableMerges";
import { inlineSign, readsRightToLeft } from "@/lib/readingDirection";
import { touchScreen } from "@/lib/screenKind";

/** Notion's face (seen 2026-11-05): 22px, 26px left of the block's frame, in the middle of its first line. */
const FACE_SIZE = 22;
const FACE_GAP = 26;
/** Left of a table's edge, beside its first row. */
const TABLE_FACE_GAP = 26;
/**
 * A touch screen's face, as Notion's phone and iPad (seen 2026-11-12 in
 * iPhone Safari at 402pt and iPad Safari at 834, dark, measured 2026-11-15
 * from those pictures): 16px, 18 with its ring (17.3–18 there). Its ring
 * starts 28px before a line's text, as the desktop face starts 26px before
 * the 2px the text sits inside Notion's block frame (Leafen's frame on a
 * phone starts further out, so the text is the measure), and ends 1px before
 * a table's edge.
 */
const TOUCH_FACE_SIZE = 16;
const TOUCH_TEXT_GAP = 27;
const TOUCH_TABLE_GAP = 2;
/** Further left while the pointer is on the line and its + and handle show. */
const HANDLE_SHIFT = 52;
/** How far left of a block the pointer still counts as on its line, as far as the face past the handle. */
const LINE_REACH = 90;
/** Each further person on the same line, overlapping. */
const SAME_LINE_SHIFT = 18;
const LEAVE_MS = 180;

type Face = Pick<PagePresenceAwarenessView, "color" | "label" | "userId"> & { blockId: string };
type Place = { x: number; y: number; moved: boolean };
type Pointer = { x: number; y: number };

function blockElement(root: HTMLElement, blockId: string) {
  return root.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(blockId)}"]`);
}

/** Beside a block's first line: a callout's at the top of its box, a table's at its first row. */
function placeBeside(block: HTMLElement, origin: DOMRect, touch: boolean): Omit<Place, "moved"> | null {
  if (block.getClientRects().length === 0) return null;
  const size = touch ? TOUCH_FACE_SIZE : FACE_SIZE;
  // Its left `gap` before the box's start: past its right where the page reads from the right.
  const rtl = readsRightToLeft(block);
  const before = (box: { left: number; width: number }, gap: number) => rtl ? box.left + box.width - origin.left + gap - size : box.left - origin.left - gap;
  const type = block.getAttribute("data-block-type");
  const row = type === "table" ? block.querySelector("[data-table-row-id]") : null;
  if (row) {
    // A table with merged cells has no row boxes: its grid's first row stands in.
    const grid = block.querySelector<HTMLElement>("[role=table][data-table-merged]");
    const rect = (grid ? simpleTableGridBoxes(grid, { left: 0, top: 0 }).rows[0] : null) ?? row.getBoundingClientRect();
    return { x: before(rect, touch ? TOUCH_TABLE_GAP + size : TABLE_FACE_GAP), y: rect.top - origin.top + (rect.height - size) / 2 };
  }
  const frame = block.getBoundingClientRect();
  const text = type === "callout" ? null : block.querySelector<HTMLElement>("[data-rt-editable]");
  if (!text) return { x: before(frame, FACE_GAP), y: frame.top - origin.top + 3 };
  const rect = text.getBoundingClientRect(), style = getComputedStyle(text);
  const line = Number.parseFloat(style.lineHeight) + Number.parseFloat(style.paddingTop) + Number.parseFloat(style.paddingBottom);
  const first = Number.isFinite(line) ? Math.min(rect.height, line) : rect.height;
  return { x: touch ? before(textStart(text, rect), TOUCH_TEXT_GAP) : before(frame, FACE_GAP), y: rect.top - origin.top + (first - size) / 2 };
}

/** Where a line's text starts across: its box less the padding before its words. */
function textStart(text: HTMLElement, rect: DOMRect) {
  const style = getComputedStyle(text);
  const start = Number.parseFloat(style.paddingInlineStart) || 0, end = Number.parseFloat(style.paddingInlineEnd) || 0;
  return { left: rect.left + (readsRightToLeft(text) ? 0 : start), width: rect.width - start - end };
}

function pointerOnLine(pointer: Pointer | null, block: HTMLElement) {
  if (!pointer) return false;
  const rect = block.getBoundingClientRect();
  return pointer.y >= rect.top && pointer.y <= rect.bottom && pointer.x >= rect.left - LINE_REACH && pointer.x <= rect.right;
}

/**
 * The faces beside the lines others' carets are on, as Notion's: the only
 * thing that shows in the page itself, with no caret or selection in the text.
 * A face grows in, slides from line to line and shrinks away, and steps aside
 * for the line's + and handle; there it can be pointed at for a name. Where the
 * page has no room left of its lines, as on a phone, it stands where it would,
 * cut by the page's edge, as Notion's phone (seen 2026-11-12 in iPhone Safari
 * at 402pt: half a face at the screen's left end beside a caret's, a chosen
 * text's or a chosen block's line); only one wholly past the edge shows none.
 */
export function PagePresenceFaces({ pageId, rootRef }: { pageId: string; rootRef: RefObject<HTMLElement | null> }) {
  const { awareness } = usePagePresenceSnapshot(pageId);
  const anchorRef = useRef<HTMLDivElement>(null);
  const faces = useMemo<Face[]>(() => awareness.flatMap((person) => person.blockId
    ? [{ blockId: person.blockId, color: person.color, label: person.label, userId: person.userId }] : []), [awareness]);
  const facesRef = useRef(faces);
  facesRef.current = faces;
  const [places, setPlaces] = useState<Record<string, Place>>({});
  const pointer = useRef<Pointer | null>(null);
  const [leaving, setLeaving] = useState<Face[]>([]);

  const measure = useCallback(() => {
    const root = rootRef.current, anchor = anchorRef.current;
    if (!root || !anchor) return;
    const origin = anchor.getBoundingClientRect();
    const touch = touchScreen(), size = touch ? TOUCH_FACE_SIZE : FACE_SIZE;
    // Each further person on a line overlaps the one before by as much of a smaller face.
    const sameLine = (SAME_LINE_SHIFT * size) / FACE_SIZE;
    const scroller = root.closest<HTMLElement>(".nscroll")?.getBoundingClientRect();
    const onLine = new Map<string, number>();
    const next: Record<string, Place> = {};
    for (const face of facesRef.current) {
      const block = blockElement(root, face.blockId);
      const place = block && placeBeside(block, origin, touch);
      if (!block || !place) continue;
      const before = onLine.get(face.blockId) ?? 0;
      onLine.set(face.blockId, before + 1);
      const moved = pointerOnLine(pointer.current, block);
      // Further out from the page past the faces before it and the handle: rightward read from the right.
      const sign = inlineSign(block);
      const x = place.x - (before * sameLine + (moved ? HANDLE_SHIFT : 0)) * sign;
      const shownAt = origin.left + place.x - before * sameLine * sign;
      if (scroller && (sign < 0 ? shownAt >= scroller.right : shownAt + size <= scroller.left)) continue;
      next[face.userId] = { x, y: place.y, moved };
    }
    setPlaces((current) => {
      const keys = Object.keys(next);
      return keys.length === Object.keys(current).length
        && keys.every((key) => current[key]?.x === next[key].x && current[key]?.y === next[key].y && current[key]?.moved === next[key].moved) ? current : next;
    });
  }, [rootRef]);

  useLayoutEffect(measure, [faces, measure]);

  const someone = faces.length > 0;
  useEffect(() => {
    const root = rootRef.current;
    if (!root || !someone) return undefined;
    let frame = 0;
    const soon = () => {
      if (!frame) frame = window.requestAnimationFrame(() => { frame = 0; measure(); });
    };
    const size = new ResizeObserver(soon);
    size.observe(root);
    const lines = new MutationObserver(soon);
    lines.observe(root, { childList: true, subtree: true });
    window.addEventListener("resize", soon);
    const moved = (event: MouseEvent) => {
      pointer.current = { x: event.clientX, y: event.clientY };
      soon();
    };
    document.addEventListener("mousemove", moved, { passive: true });
    return () => {
      window.cancelAnimationFrame(frame);
      size.disconnect();
      lines.disconnect();
      window.removeEventListener("resize", soon);
      document.removeEventListener("mousemove", moved);
    };
  }, [measure, rootRef, someone]);

  // Someone who left stays long enough to shrink away.
  const shown = useRef<Face[]>([]);
  const lastPlaces = useRef(places);
  useEffect(() => {
    const gone = shown.current.filter((face) => !faces.some((other) => other.userId === face.userId));
    shown.current = faces.filter((face) => places[face.userId]);
    if (Object.keys(places).length) lastPlaces.current = { ...lastPlaces.current, ...places };
    if (!gone.length) return;
    setLeaving((current) => [...current.filter((face) => !gone.some((other) => other.userId === face.userId)), ...gone]);
    // Not cancelled by the next change: the places change again as the line's people do.
    window.setTimeout(() => setLeaving((current) => current.filter((face) => !gone.includes(face))), LEAVE_MS);
  }, [faces, places]);

  const touch = touchScreen();
  const face = (person: Face, place: Place | undefined, away: boolean) => place ? (
    <div key={person.userId} className={styles.place} data-presence-face={person.userId} data-presence-block={person.blockId}
      data-moved={place.moved ? "true" : undefined} style={{ transform: `translate(${place.x}px, ${place.y}px)` }}>
      <span className={styles.face} data-leaving={away ? "true" : undefined}
        style={{ "--presence-color": person.color } as React.CSSProperties}>
        <PersonAvatar userId={person.userId} name={person.label} size={touch ? TOUCH_FACE_SIZE : FACE_SIZE} />
        <span className={styles.name}>
          <PersonAvatar userId={person.userId} name={person.label} size={16} />
          <span>{person.label}</span>
        </span>
      </span>
    </div>
  ) : null;

  return (
    <div ref={anchorRef} className={styles.anchor} contentEditable={false} aria-hidden="true" data-touch={touch ? "true" : undefined}>
      {faces.map((person) => face(person, places[person.userId], false))}
      {leaving.filter((person) => !faces.some((other) => other.userId === person.userId))
        .map((person) => face(person, lastPlaces.current[person.userId], true))}
    </div>
  );
}
