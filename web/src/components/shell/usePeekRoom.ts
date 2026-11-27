import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { afterNextPaint } from "@/lib/afterPress";
import { SIDE_PEEK_CLOSE_EASING, SIDE_PEEK_CLOSE_MS, SIDE_PEEK_OPEN_EASING, SIDE_PEEK_OPEN_MS, SIDE_PEEK_SWITCH_MS, lessMotion, takePeekSwitch } from "@/lib/sidePeekMotion";

/** A side peek's place as the page is told it: whether one is open and how wide. */
export type PeekPlace = { open: boolean; width?: number };

// Elements in the page above which laying it out again outlasts a frame
// (2026-11-13: 85–130 ms a frame for a page of 38,000 elements; counting
// them takes well under a millisecond).
const LARGE_PAGE_ELEMENTS = 6_000;
const SIDE_PEEK_ROOM = "side-peek-room";

function roomOf(peek: PeekPlace) {
  return peek.open ? `${Math.min(peek.width ?? 564, window.innerWidth)}px` : "0px";
}

/**
 * The room the page gives the side peek beside it (AppShell): the peek's
 * place, or the place the page still keeps while a large page waits.
 *
 * A page gives that room as the peek slides in and takes it back as it
 * slides out or opens whole, in the same frames, as Notion's page (seen
 * 2026-11-12: 1170 wide to 450 and back), quicker as the peek turns to the
 * center or back (sidePeekMotion.ts); a width dragged follows at once.
 *
 * A large page is laid out anew once instead. Each frame of that motion laid
 * all of it out again and its tables measured and wrote again after it,
 * 85–130 ms a frame for a page of 38,000 elements (seen 2026-11-13), so the
 * peek's slide showed two frames and the page jumped. The peek slides in over
 * it and the page makes its room once the peek is in; it takes the room back
 * as soon as the press that closed or turned the peek is drawn, the peek
 * sliding out over it.
 */
export function usePeekRoom(mainRef: RefObject<HTMLElement | null>, peek: PeekPlace, phone: boolean): PeekPlace {
  // The room a large page keeps meanwhile; none when it follows the peek.
  const [kept, setKept] = useState<PeekPlace | null>(null);
  const room = kept ?? peek;
  const roomNow = useRef(room);
  roomNow.current = room;
  const before = useRef(peek);
  const waiting = useRef<(() => void) | null>(null);
  // The motion giving that room, which one turning it back starts from: kept
  // here, as asking the page for its motions worked out its styles first,
  // some 35 ms in the press that opened a peek (seen 2026-11-12).
  const motion = useRef<Animation | null>(null);
  useLayoutEffect(() => {
    const was = before.current;
    before.current = peek;
    const main = mainRef.current;
    if (!main || was.open === peek.open || phone) return;
    const switched = takePeekSwitch();
    if (main.getElementsByTagName("*").length > LARGE_PAGE_ELEMENTS) {
      waiting.current?.();
      setKept(roomNow.current === peek ? was : roomNow.current);
      // The peek starts sliding in the frame after its own (DatabaseRowPage's `entered`).
      const slidesIn = peek.open && !switched && !lessMotion();
      let timer: ReturnType<typeof setTimeout> | undefined;
      const letGo = () => {
        waiting.current = null;
        setKept(null);
      };
      const callOff = afterNextPaint(() => {
        if (slidesIn) timer = setTimeout(letGo, SIDE_PEEK_OPEN_MS);
        else letGo();
      });
      waiting.current = () => {
        callOff();
        clearTimeout(timer);
      };
      return;
    }
    if (lessMotion()) return;
    const moving = motion.current?.playState === "running" ? motion.current : null;
    const from = moving ? getComputedStyle(main).marginInlineEnd : roomOf(was);
    moving?.cancel();
    motion.current = main.animate([{ marginInlineEnd: from }, { marginInlineEnd: roomOf(peek) }], switched
      ? { id: SIDE_PEEK_ROOM, duration: SIDE_PEEK_SWITCH_MS, easing: SIDE_PEEK_CLOSE_EASING }
      : peek.open ? { id: SIDE_PEEK_ROOM, duration: SIDE_PEEK_OPEN_MS, easing: SIDE_PEEK_OPEN_EASING }
        : { id: SIDE_PEEK_ROOM, duration: SIDE_PEEK_CLOSE_MS, easing: SIDE_PEEK_CLOSE_EASING });
  }, [mainRef, peek, phone]);
  // The page keeps its own column (PageView.module.css) while it gives the
  // room and until a peek closing has slid out, as Notion's: let go as the
  // page started widening, its text was laid out again into the room still
  // taken for some frames and the blocks above the one read grew by hundreds
  // of pixels (seen 2026-11-14: the page then stood 300px and more away).
  // The document's root says so, as a page laid over the main column outside
  // the shell (a row page open whole) gives a docked panel's room to the peek
  // too (DatabaseRowProperties.module.css); a phone gives none.
  useLayoutEffect(() => {
    const root = document.documentElement;
    if (!mainRef.current) return undefined;
    if (room.open && !phone) {
      root.dataset.sideRowPeekRoom = "true";
      return undefined;
    }
    const going = motion.current?.playState === "running" ? motion.current : null;
    if (!going || phone) {
      delete root.dataset.sideRowPeekRoom;
      return undefined;
    }
    let live = true;
    const letGo = () => { if (live) delete root.dataset.sideRowPeekRoom; };
    going.finished.then(letGo, letGo);
    return () => { live = false; };
  }, [mainRef, phone, room.open]);
  useEffect(() => () => {
    waiting.current?.();
    delete document.documentElement.dataset.sideRowPeekRoom;
  }, []);
  return room;
}
