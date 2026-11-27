import { useCallback, useRef } from "react";
import { dropTargetForExternal } from "@atlaskit/pragmatic-drag-and-drop/external/adapter";
import { getFiles } from "@atlaskit/pragmatic-drag-and-drop/external/file";

type TargetArgs = Parameters<typeof dropTargetForExternal>[0];
type Payload = Parameters<NonNullable<TargetArgs["onDrop"]>>[0];
type Location = Payload["location"]["current"];
type TargetRecord = Location["dropTargets"][number];
type Source = Payload["source"];

/**
 * A place that takes files dragged in from outside. Every such place is made
 * here, so a drag reaches them all the same way.
 *
 * Pragmatic drag and drop takes the drag. WebKit (Safari, the Mac app) sends a
 * stray "dragleave" partway through a file drag, after which Pragmatic takes the
 * drag to have left the window and lets the file go by (seen 2026-11-05 in the
 * app: Files over the page, dropEffect "none", no drop). A file drag nothing
 * has taken goes to the same places here, with the same calls Pragmatic makes.
 * While Pragmatic holds a drag it has cancelled the event in its capture phase,
 * so the two never both take one; drags of anything but files are left alone.
 */
export function dropTargetForFiles(args: TargetArgs) {
  const unbind = dropTargetForExternal(args);
  places.set(args.element, args);
  listen();
  return () => {
    unbind();
    if (places.get(args.element) === args) places.delete(args.element);
    if (over.some((record) => record.element === args.element)) over = over.filter((record) => record.element !== args.element);
  };
}

/**
 * A callback ref for an element that takes the files dropped on it: an upload
 * area in a window or menu. Over a page, the innermost place takes the drop,
 * so the page doesn't take the same file again (seen 2026-11-05: a file let go
 * on the image window's upload button made two images).
 */
export function useFileDrop(take: (files: File[]) => void, { canDrop, onOver }: {
  /** Whether it takes this drag, by the kinds of data it carries; files at all by default. */
  canDrop?: (types: readonly string[]) => boolean;
  onOver?: (over: boolean) => void;
} = {}) {
  const latest = useRef({ take, canDrop, onOver });
  latest.current = { take, canDrop, onOver };
  return useCallback((element: HTMLElement | null) => element ? dropTargetForFiles({
    element,
    canDrop: ({ source }) => latest.current.canDrop ? latest.current.canDrop(source.types) : source.types.includes("Files"),
    getDropEffect: () => "copy",
    onDragEnter: () => latest.current.onOver?.(true),
    onDragLeave: () => latest.current.onOver?.(false),
    onDrop: ({ source }) => {
      latest.current.onOver?.(false);
      const files = getFiles({ source });
      if (files.length > 0) latest.current.take(files);
    },
  }) : undefined, []);
}

const places = new Map<Element, TargetArgs>();
// The places the fallback holds a drag over now, innermost first, as Pragmatic's.
let over: TargetRecord[] = [];
// How a place took each move of a file drag, for the last word on it (see `listen`).
const taken = new WeakMap<Event, DataTransfer["dropEffect"]>();
let leaving = 0;
let listening = false;

const carriesFiles = (event: DragEvent) => Array.from(event.dataTransfer?.types ?? []).includes("Files");

function inputOf(event: DragEvent): Location["input"] {
  const { altKey, button, buttons, ctrlKey, metaKey, shiftKey, clientX, clientY, pageX, pageY } = event;
  return { altKey, button, buttons, ctrlKey, metaKey, shiftKey, clientX, clientY, pageX, pageY };
}

// What is dragged, as Pragmatic shows it: the files can be read only at the drop.
function sourceOf(event: DragEvent, dropped: boolean): Source {
  const transfer = event.dataTransfer;
  return {
    types: Array.from(transfer?.types ?? []),
    items: dropped ? Array.from(transfer?.items ?? []) : [],
    getStringData: (mediaType) => dropped && mediaType !== "Files" ? transfer?.getData(mediaType) ?? null : null,
  };
}

function placesOver(target: EventTarget | null, source: Source, input: Location["input"]) {
  const records: TargetRecord[] = [];
  for (let element = target instanceof Element ? target : null; element; element = element.parentElement) {
    const place = places.get(element);
    if (!place || (place.canDrop && !place.canDrop({ source, input, element }))) continue;
    records.push({
      element,
      data: place.getData?.({ source, input, element }) ?? {},
      dropEffect: place.getDropEffect?.({ source, input, element }) ?? "copy",
      isActiveDueToStickiness: false,
    });
  }
  return records;
}

function payloadFor(record: TargetRecord, source: Source, current: Location, previous: TargetRecord[]): Payload {
  return { source, location: { initial: current, previous: { dropTargets: previous }, current }, self: record };
}

// In Pragmatic's order: the places left, then those come to, then a move over
// each (an outer place's enter may clear what an inner one's set; its move keeps it).
function settle(next: TargetRecord[], source: Source, input: Location["input"]) {
  const previous = over;
  over = next;
  const current = { input, dropTargets: next };
  const has = (records: TargetRecord[], record: TargetRecord) => records.some((other) => other.element === record.element);
  if (previous.length !== next.length || previous.some((record, index) => record.element !== next[index].element)) {
    for (const record of previous) {
      const place = places.get(record.element), payload = payloadFor(record, source, current, previous);
      place?.onDropTargetChange?.(payload);
      if (!has(next, record)) place?.onDragLeave?.(payload);
    }
    for (const record of next) {
      if (has(previous, record)) continue;
      const place = places.get(record.element), payload = payloadFor(record, source, current, previous);
      place?.onDropTargetChange?.(payload);
      place?.onDragEnter?.(payload);
    }
  }
  for (const record of next) places.get(record.element)?.onDrag?.(payloadFor(record, source, current, previous));
}

function listen() {
  if (listening || typeof document === "undefined") return;
  listening = true;
  // On the document in the capture phase, as Pragmatic's on the window: right
  // after it and before anything in the page, which may cancel a drag of its own
  // (the text editor cancels every "dragover" over its lines).
  const move = (event: DragEvent) => {
    window.clearTimeout(leaving);
    if (!carriesFiles(event)) return;
    const source = sourceOf(event, false), input = inputOf(event);
    // Pragmatic has taken it, picking the drag up again: the places it now
    // holds stay as they are.
    if (event.defaultPrevented) {
      if (event.dataTransfer) taken.set(event, event.dataTransfer.dropEffect);
      if (over.length > 0) {
        const held = placesOver(event.target, source, input);
        const current = { input, dropTargets: held }, previous = over;
        over = [];
        for (const record of previous) {
          if (!held.some((other) => other.element === record.element)) places.get(record.element)?.onDragLeave?.(payloadFor(record, source, current, previous));
        }
      }
      return;
    }
    const next = placesOver(event.target, source, input);
    if (next.length > 0) {
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = next[0].dropEffect;
      taken.set(event, next[0].dropEffect);
    }
    if (next.length > 0 || over.length > 0) settle(next, source, input);
  };
  document.addEventListener("dragenter", move, true);
  document.addEventListener("dragover", move, true);
  // The desktop runtime (Wails) answers every file drag on <html> after the
  // page has: with its own file drop off, "none", which WebKit takes as no
  // drop, and the file never lands (seen 2026-11-06 in the app: the drop line
  // showed, then a dragleave instead of the drop). A place that took the drag
  // has the last word, at the window, after it.
  const keep = (event: DragEvent) => {
    const effect = taken.get(event);
    if (effect && event.dataTransfer) event.dataTransfer.dropEffect = effect;
  };
  window.addEventListener("dragenter", keep);
  window.addEventListener("dragover", keep);
  // A drag that leaves the window lets its places go; WebKit's stray leave is
  // followed at once by the next "dragover", which keeps them.
  document.addEventListener("dragleave", (event) => {
    if (event.relatedTarget !== null || over.length === 0) return;
    const source = sourceOf(event, false), input = inputOf(event);
    window.clearTimeout(leaving);
    leaving = window.setTimeout(() => settle([], source, input), 150);
  }, true);
  document.addEventListener("drop", (event) => {
    window.clearTimeout(leaving);
    if (event.defaultPrevented || !carriesFiles(event)) {
      over = [];
      return;
    }
    const source = sourceOf(event, true), input = inputOf(event);
    const dropTargets = placesOver(event.target, source, input);
    if (dropTargets.length === 0) {
      if (over.length > 0) settle([], source, input);
      return;
    }
    const previous = over;
    over = [];
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = dropTargets[0].dropEffect;
    const current = { input, dropTargets };
    for (const record of previous) {
      if (!dropTargets.some((other) => other.element === record.element)) places.get(record.element)?.onDragLeave?.(payloadFor(record, source, current, previous));
    }
    for (const record of dropTargets) places.get(record.element)?.onDrop?.(payloadFor(record, source, current, previous));
  }, true);
}
