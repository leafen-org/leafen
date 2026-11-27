import { ICON_SIZES, IconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";

// The ⠿ a thing is dragged by: six dots, two across and three down, where
// Notion's two marks put their ink (each drawn large and its dots measured,
// 2026-11-18 in Chrome at 1440, two tabs), not from Notion's paths. Its small
// mark (dragHandleFillSmall, a 16 box: the menu lists', a property row's) has
// dots 2.4 across, 4 apart across and 4.8 down, ink 6.4×12; its 20px one
// (dragHandle: a block's, a table row's, a button's) dots 2.5 across, 5 apart
// across and 6 down, ink 7.5×14.5. The small mark's dots are the heavier for
// its size, so the two are drawn apart rather than one scaled to the other.
const SMALL = { box: 16, dot: 1.2, across: [6, 10], down: [3.2, 8, 12.8] } as const;
const REGULAR = { box: 20, dot: 1.25, across: [7.5, 12.5], down: [4, 10, 16] } as const;

/** The ⠿: Notion's small mark below 18px, its 20px one from there, each growing and shrinking with the size it is drawn at. */
export const DragHandleIcon = (p: LeafenIconProps) => {
  const dots = smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? SMALL : REGULAR;
  return (
    <IconSvg {...p} viewBox={`0 0 ${dots.box} ${dots.box}`} glyph="drag-handle" fill="currentColor" stroke="none">
      {dots.down.flatMap((y) => dots.across.map((x) => <circle key={`${x} ${y}`} cx={x} cy={y} r={dots.dot} />))}
    </IconSvg>
  );
};
