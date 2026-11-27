import { startTransition, useEffect, useState } from "react";
import { afterNextPaint } from "@/lib/afterPress";

// A picker's grid — the emoji and the built-in icons — draws its first rows
// as it opens and the rest after that frame, a step at a time, so a press
// answers on the next frame. The emoji catalog's 1,843 drawn at once held a
// phone's reaction sheet 176–360 ms, and even a screenful of 200 kept a
// Galaxy A90 at 208–232 ms, most of it drawing the emoji's pictures
// (2026-11-14); the 885 built-in icons drawn at once, solid, held the icon
// tab's press 272–320 ms on a NAS-class server with Chrome's CPU ×2, and
// drawn this way 72–96 ms (2026-11-16, Notion's own 88 ms). The emoji's
// first step is four rows of a phone's nine (three of the desktop picker's
// twelve); the next fills the rest of a tall phone's screen, sixteen rows
// of nine; each takes a phone about 30 ms.
const FIRST_CELLS = 36;
const MORE_CELLS = 150;

/**
 * How many of a picker grid's `total` cells are drawn yet: the first rows
 * (`first`, where a grid shows more at once) as it opens and again when its
 * filter (`restart`) changes, then a step more after each frame is painted,
 * until all are. `drawAll` draws the rest at once (a group asked for from
 * the emoji bar before it was drawn).
 */
export function usePickerCellsDrawn(total: number, restart: string, first = FIRST_CELLS) {
  const [drawn, setDrawn] = useState(first);
  const [drawnFor, setDrawnFor] = useState(restart);
  if (drawnFor !== restart) {
    setDrawnFor(restart);
    setDrawn(first);
  }
  const allDrawn = drawn >= total;
  useEffect(() => allDrawn ? undefined
    : afterNextPaint(() => startTransition(() => setDrawn((count) => count + MORE_CELLS))), [drawn, allDrawn]);
  return { drawn, allDrawn, drawAll: () => setDrawn(Number.POSITIVE_INFINITY) };
}
