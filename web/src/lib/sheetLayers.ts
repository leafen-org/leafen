// The layers phone sheets stand in (PhoneSheet.tsx) while they are shown. A
// sheet opened from a window or menu drawn in a layer goes just over that
// one, and stays there once the menu has gone; a window, menu, question or
// sheet opened after it is the person's next step, so it goes over the
// sheet, not under it (2026-11-16, a touch screen 834 wide: the trash opened from the
// sidebar's "더 보기" menu stood at 2001, and its delete's confirmation,
// opened from a row of it, at 2000 under it, where nothing could answer it).
const shown = new Set<{ zIndex: number }>();

/** Holds a shown sheet's layer until the returned release; a layer or sheet opened meanwhile goes over it (overlayLayer.ts, PhoneSheet.tsx). */
export function holdSheetLayer(zIndex: number) {
  const held = { zIndex };
  shown.add(held);
  return () => { shown.delete(held); };
}

/** The highest layer a shown sheet stands in, or `floor` where none stands higher. */
export function sheetLayerTop(floor: number) {
  let top = floor;
  for (const sheet of shown) top = Math.max(top, sheet.zIndex);
  return top;
}
