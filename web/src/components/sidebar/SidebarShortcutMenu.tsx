import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import {
  ArrowDown, ArrowUp, CenterPeekIcon, ComposeIcon, DotsHorizontal, FullPagePeekIcon, OpenAsPage, SidePeekIcon, SlidersIcon, X,
} from "@/icons/leafen";
import type { SidebarLayoutShortcut, SidebarOpenMode } from "@/workspaceMembershipWire.generated";
import { sidebarShortcutOpenMode } from "./sidebarLayoutModel";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { useSidebarCustomizing } from "./sidebarCustomizing";
import { styles } from "./sidebarStyles";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";

const MENU_WIDTH = 240;
const MODES: readonly SidebarOpenMode[] = ["side", "center", "full"];

const MODE_ICONS: Record<SidebarOpenMode, ReactNode> = {
  side: <SidePeekIcon size="regular" aria-hidden="true" />,
  center: <CenterPeekIcon size="regular" aria-hidden="true" />,
  full: <FullPagePeekIcon size="regular" aria-hidden="true" />,
};

/** The icon of where a page opens, as a menu row shows it. */
export function sidebarOpenModeIcon(mode: SidebarOpenMode): ReactNode {
  return MODE_ICONS[mode];
}

/** The modes "다음에서 열기" offers, in its order. */
export const SIDEBAR_OPEN_MODES = MODES;

export function sidebarOpenModeLabel(mode: SidebarOpenMode, t: (key: string) => string) {
  return t(mode === "side" ? "openInSide" : mode === "center" ? "openInCenter" : "openInFull");
}

/**
 * A shortcut's moving and removing rows, as Notion ends every shortcut's
 * menu (seen 2026-10-30): "위로 옮기기" unless it is first, "아래로 옮기기"
 * unless it is last, "단축키 제거", then a line and "사이드바 사용자 지정".
 * Notion's "탭으로 이동" among them goes with its tabs (the one-sidebar
 * decision of 2026-10-29). In a customize list the row's × removes it.
 */
export function sidebarShortcutArrangeRows(t: (key: string) => string, { close, onMove, onRemove, onCustomize, customizing = false }: {
  close(restoreFocus?: boolean): void;
  onMove: { up?(): void; down?(): void };
  onRemove(): void;
  onCustomize?(): void;
  customizing?: boolean;
}): SidebarMenuRow[] {
  const data = (key: string) => ({ "data-sidebar-shortcut-action": key });
  return [
    ...(onMove.up ? [{ key: "move-up", label: t("sectionMoveUp"), icon: <ArrowUp size="regular" aria-hidden="true" />, data: data("move-up"),
      run: () => { close(); onMove.up?.(); } }] : []),
    ...(onMove.down ? [{ key: "move-down", label: t("sectionMoveDown"), icon: <ArrowDown size="regular" aria-hidden="true" />, data: data("move-down"),
      run: () => { close(); onMove.down?.(); } }] : []),
    ...(customizing ? [] : [{ key: "remove", label: t("shortcutRemove"), icon: <X size="regular" aria-hidden="true" />, data: data("remove"),
      run: () => { close(false); onRemove(); } }]),
    ...(onCustomize ? [{ key: "customize", label: t("customizeSidebar"), icon: <SlidersIcon size="regular" aria-hidden="true" />,
      separatorBefore: true, data: data("customize"), run: () => { close(false); onCustomize(); } }] : []),
  ];
}

/**
 * A shortcut's menu, as Notion's (seen 2026-10-02 and 2026-10-30, 240px): a
 * new page's has "단축키 이름 변경", "다음에서 열기" with where the page opens
 * beside it (사이드 보기, 중앙에서 보기 or 전체 페이지 보기, the last at
 * first), "<page> 열기" for the page it goes in; a page's has "다음에서 열기";
 * then, after a line, its moving and removing rows, which a new database's
 * and the others' have alone.
 */
export function SidebarShortcutMenu({
  label,
  shortcut,
  placeTitle,
  onRename,
  onOpenIn,
  onOpenPlace,
  onMove,
  onRemove,
  onCustomize,
}: {
  /** The shortcut's name as shown, for the button's label. */
  label: string;
  shortcut: SidebarLayoutShortcut;
  /** A new page's page, for "<page> 열기". */
  placeTitle?: string;
  onRename: () => void;
  onOpenIn: (mode: SidebarOpenMode) => void;
  onOpenPlace?: () => void;
  onMove: { up?(): void; down?(): void };
  onRemove(): void;
  onCustomize?(): void;
}) {
  const { t } = useTranslation("sidebar");
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const customizing = useSidebarCustomizing();
  const opens = shortcut.kind === "page" || shortcut.kind === "new_page";
  const mode = sidebarShortcutOpenMode(shortcut);

  function close(restoreFocus = true) {
    setOpen(false);
    if (restoreFocus) focusOpenerAfterPaint(button.current, { preventScroll: true });
  }

  const action = (key: string) => ({ "data-sidebar-shortcut-action": key });
  const choices: SidebarMenuRow[] = [
    ...(shortcut.kind === "new_page" ? [{ key: "rename", label: t("shortcutRename"), icon: <ComposeIcon size="regular" aria-hidden="true" />,
      data: action("rename"), run: () => { close(false); onRename(); } }] : []),
    ...(opens ? [{ key: "open-in", label: t("openIn"), icon: MODE_ICONS[mode], value: sidebarOpenModeLabel(mode, t), data: action("open-in"),
      submenu: { label: t("openIn"), radio: true, data: { "data-sidebar-shortcut-choices": "open" }, choices: MODES.map((choice) => ({
        key: choice, label: sidebarOpenModeLabel(choice, t), icon: MODE_ICONS[choice], checked: mode === choice,
        data: { "data-sidebar-shortcut-open": choice }, run: () => { onOpenIn(choice); close(); } })) } }] : []),
    ...(shortcut.kind === "new_page" && onOpenPlace && placeTitle ? [{ key: "open-place", label: t("openPlace", { title: placeTitle }),
      icon: <OpenAsPage size="regular" aria-hidden="true" />, data: action("open-place"), run: () => { close(false); onOpenPlace(); } }] : []),
  ];
  const arrange = sidebarShortcutArrangeRows(t, { close, onMove, onRemove, onCustomize, customizing });
  // A line sets moving and removing apart from the choices, as Notion's.
  if (choices.length && arrange.length) arrange[0] = { ...arrange[0], separatorBefore: true };
  const rows = [...choices, ...arrange];
  // With nothing to set or move, a customize row has its × alone.
  if (rows.length === 0) return null;

  return (
    <>
      <button ref={button} type="button" className={styles.sectionAction} data-sidebar-shortcut-menu={shortcut.key}
        aria-label={t("shortcutMenu", { name: label })} title={t("shortcutMenuTitle")} aria-haspopup="menu"
        aria-expanded={open} onClick={(event) => {
          event.stopPropagation();
          if (open) close();
          else setOpen(true);
        }}>
        {customizing ? <SlidersIcon size="regular" aria-hidden="true" /> : <DotsHorizontal size="regular" aria-hidden="true" />}
      </button>
      {open ? createPortal(<SidebarActionMenu button={button} label={t("shortcutMenu", { name: label })} width={MENU_WIDTH}
        rows={rows} backdropLabel={t("closeSectionMenu")} close={close} />, document.body) : null}
    </>
  );
}
