import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useButton, useMenuTrigger } from "react-aria";
import { useMenuTriggerState } from "react-stately";
import { CheckIcon, DotsHorizontal, EyeIcon, FileText, LibraryIcon } from "@/icons/leafen";
import { LastVisitedIcon } from "@/icons/leafen/homeSampleIcons";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { readStartPage, saveStartPage, START_PAGES, type StartPage } from "@/lib/startPage";
import { RowMenuBesideSubmenu, RowMenuChoiceSubmenu, RowMenuSubmenuRow } from "@/components/RowMenuSubmenus";
import rowMenuStyles from "@/components/RowMenu.module.css";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import type { Session } from "@/types";

const WIDGETS = ["greeting", "recentEdits", "upcoming", "myTasks", "database"] as const;
export type HomeWidget = typeof WIDGETS[number];
const HIDDEN_KEY = "leafen:home-hidden-widgets";

/** The widgets this person hid on this device, as a page's start page is kept. */
export function useHiddenHomeWidgets(session: Session | undefined) {
  const place = session ? { userId: session.user.id } : null;
  // The database view stays off until asked for, as Notion's (seen 2026-11-05).
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => {
    try { return new Set(place ? JSON.parse(personValue(HIDDEN_KEY, place) ?? '["database"]') as string[] : ["database"]); } catch { return new Set(["database"]); }
  });
  const toggle = (widget: HomeWidget) => {
    const next = new Set(hidden);
    if (!next.delete(widget)) next.add(widget);
    setHidden(next);
    if (place) storePersonValue(HIDDEN_KEY, JSON.stringify([...next]), place);
  };
  return { hidden, toggle };
}

const START_PAGE_KEYS: Record<StartPage, string> = {
  last_visited_page: "startPageLastVisited", sidebar_top_page: "startPageSidebarTop", library: "startPageLibrary",
};
// Each choice's mark, as Notion's (seen 2026-11-13): a clock turning back, a
// page and the library's books. The widgets' choices have none, as Notion's.
const START_PAGE_ICONS: Record<StartPage, typeof FileText> = {
  last_visited_page: LastVisitedIcon, sidebar_top_page: FileText, library: LibraryIcon,
};

/**
 * Home's "⋯", as Notion's (seen 2026-11-05): the page to start on and which
 * widgets show, each opening beside it. Notion's AI start page and its widgets
 * Leafen has none of are left out, and so is its third row, "홈에 대해
 * 알아보기", which opens Notion's help center (seen 2026-11-05 and
 * 2026-11-13): Leafen has no help center. Its two rows are the side-list rows
 * every other menu has (RowMenuSubmenuRow): → or Enter opens the choices, ←
 * closes them. Notion's answer no key there, the list opening only to a press
 * (seen 2026-11-18 at 1440 in Chrome, judged by the screen: ↓ ringed "기본 시작
 * 페이지 변경", then →, Enter and ← each left it as it was), which would leave
 * a keyboard with no way to the start page; its page "⋯" rows open by both.
 */
export function HomeMenu({ session, workspaceId, hidden, onToggle }: {
  session: Session; workspaceId: string; hidden: ReadonlySet<string>; onToggle(widget: HomeWidget): void;
}) {
  const { t } = useTranslation(["homeView", "workspaceSettingsDialog"]);
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const state = useMenuTriggerState({});
  const [open, setOpen] = useState<{ kind: "start" | "widgets"; row: HTMLElement } | null>(null);
  const [startPage, setStartPage] = useState(() => readStartPage(session.user.id, workspaceId));
  const close = () => { setOpen(null); state.close(); };
  const { menuTriggerProps, menuProps } = useMenuTrigger({ type: "menu" }, state, trigger);
  const { buttonProps } = useButton({ ...menuTriggerProps, preventFocusOnPress: false, onPressStart: undefined,
    onPress: (event) => { if (event.pointerType !== "keyboard") state.toggle(); } }, trigger);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 4 },
  ) : null, [state.isOpen], undefined, { kind: "menu", onEscape: close, onOutsidePress: close,
    inside: () => [trigger.current] });
  const row = (kind: "start" | "widgets", icon: ReactNode, label: string) => (
    <RowMenuSubmenuRow icon={icon} label={label} value="" open={open?.kind === kind} disabled={false}
      onOpen={(element) => setOpen({ kind, row: element })} data={{ [kind === "start" ? "data-home-menu-start" : "data-home-menu-widgets"]: "" }} />
  );
  return <>
    <button {...buttonProps} ref={trigger} className={topBarStyles.iconAction} data-home-menu aria-label={t("menu")}>
      <DotsHorizontal size="wide" aria-hidden="true" />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={`${rowMenuStyles.menu} ${rowMenuStyles.contextMenu}`} data-row-menu-variant="home"
      style={{ ...popoverPlacementStyle(placement), width: 250 }}>
      <NativeActionMenu {...menuProps} aria-label={t("menu")} autoFocus={state.focusStrategy ?? true}>
        {row("start", <FileText size="regular" aria-hidden="true" />, t("menuStartPage"))}
        {row("widgets", <EyeIcon size="regular" weight="regular" aria-hidden="true" />, t("menuWidgets"))}
      </NativeActionMenu>
      {open?.kind === "start" ? <RowMenuChoiceSubmenu row={open.row} width={250} label={t("menuStartPage")} value={startPage} onClose={() => setOpen(null)}
        choices={START_PAGES.map((value) => {
          const Icon = START_PAGE_ICONS[value];
          return { value, label: t(`workspaceSettingsDialog:${START_PAGE_KEYS[value]}`), icon: <Icon size="regular" aria-hidden="true" /> };
        })}
        onChoose={(value) => { saveStartPage(session.user.id, workspaceId, value); setStartPage(value); close(); }} /> : null}
      {open?.kind === "widgets" ? <RowMenuBesideSubmenu row={open.row} width={200} label={t("menuWidgets")} ownMenu onClose={() => setOpen(null)}>
        <NativeActionMenu aria-label={t("menuWidgets")}>
          {WIDGETS.map((widget) => <NativeActionMenuItem key={widget} id={widget} role="menuitemcheckbox" aria-checked={!hidden.has(widget)}
            className={rowMenuStyles.item} data-home-widget={widget} onClick={() => onToggle(widget)}>
            <span>{t(`widget.${widget}`)}</span>{hidden.has(widget) ? null : <CheckIcon size="small" aria-hidden="true" />}
          </NativeActionMenuItem>)}
        </NativeActionMenu>
      </RowMenuBesideSubmenu> : null}
    </div>, document.body) : null}
  </>;
}
