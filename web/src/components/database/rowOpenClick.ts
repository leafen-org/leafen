/**
 * How a click on a row or card opens it, as Notion's row menu says (seen
 * 2026-10-11: "새 탭 ⌘⇧↵ · 사이드 보기 ⌥Click"): ⌘ or Ctrl in a new tab, ⌥ in
 * the side peek, otherwise as the view opens its pages.
 */
export function rowOpenModeOfClick(event: { metaKey: boolean; ctrlKey: boolean; altKey: boolean }): "default" | "new_tab" | "side" {
  return event.metaKey || event.ctrlKey ? "new_tab" : event.altKey ? "side" : "default";
}
