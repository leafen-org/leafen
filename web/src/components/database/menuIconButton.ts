/**
 * The name and state of a button drawn as an icon alone in a menu or a
 * settings panel that opens a picker or a menu of its own, one rule for
 * every such button: its name is read out alone, with no title and so no
 * dark tip, as Notion's (measured 2026-11-17 and 2026-11-18 in Chrome 1416,
 * dark and light in two tabs, a 2.5s hover each: "아이콘 바꾸기" on a column
 * menu's head, the view settings' "+ 새 속성", a new property's tile, the
 * view settings' head and a content tab's settings, "아이콘 선택" on the
 * table's "+" new column — no title, no tip; a select option's "⋯" in a
 * cell's menu has no tip either). Only the ⓘ beside a name, which opens its
 * description's line, names itself in a tip (DescribeButton).
 */
export function menuIconButtonProps(name: string, open: boolean) {
  return { "aria-label": name, "aria-haspopup": "dialog", "aria-expanded": open } as const;
}
