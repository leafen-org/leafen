/**
 * A searched menu's own Tab stops, as Notion's page "⋯" and block "⋮⋮"
 * menus go round them (seen 2026-11-26 in Chrome 1440, two tabs, no script
 * before the keys, a letter typed after): its search field; the field's
 * "입력 값 지우기" while it holds words (the page "⋯"'s; the block "⋮⋮"'s
 * field has none); and the foot's last editor while the foot names a person
 * by a link to their profile (EditedByLine). Its rows take no Tab: the
 * arrows light them. Tab goes to the next stop and Shift+Tab to the one
 * before, round from the last to the first; with the field the only stop,
 * the field keeps the Tab (isInertMenuKey).
 */
const STOP = "[data-native-menu-search], [data-block-menu-search], [data-search-clear], [data-menu-foot-person]";

/**
 * The box a menu was drawn as: the element its portal put on the page's body
 * (BlockActionMenu) or in a popover layer (PlacedPageMenu) — beside it, not
 * in it, what opens over it (a submenu, a panel).
 */
function menuBox(element: Element): Element | null {
  let at: Element | null = element;
  while (at?.parentElement && at.parentElement !== document.body && !at.parentElement.hasAttribute("data-popover-layer")) at = at.parentElement;
  return at?.parentElement ? at : null;
}

/** The stops of the menu holding `from`, in their order on the screen. */
export function menuTabStops(from: Element): HTMLElement[] {
  const box = menuBox(from);
  return box ? [...box.querySelectorAll<HTMLElement>(STOP)].filter((stop) => stop.getClientRects().length > 0) : [];
}

/**
 * Moves a plain Tab or Shift+Tab pressed on one of a menu's stops to the next
 * or the one before; false where it is not such a key, or the stop is the
 * menu's only one (it keeps the Tab then: isInertMenuKey).
 */
export function goRoundMenuStops(event: {
  key: string; shiftKey: boolean; altKey: boolean; ctrlKey: boolean; metaKey: boolean; target: EventTarget | null;
  preventDefault(): void; stopPropagation(): void;
}): boolean {
  if (event.key !== "Tab" || event.altKey || event.ctrlKey || event.metaKey || !(event.target instanceof Element)) return false;
  const stops = menuTabStops(event.target), at = stops.indexOf(event.target as HTMLElement);
  if (at < 0 || stops.length < 2) return false;
  event.preventDefault(); event.stopPropagation();
  stops[(at + (event.shiftKey ? stops.length - 1 : 1)) % stops.length].focus({ preventScroll: true });
  return true;
}
