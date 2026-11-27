import { ArrowDown, ArrowUp, DotsHorizontal, HashIcon, Plus, SlidersIcon, SortIcon, X } from "@/icons/leafen";
import { createPortal } from "react-dom";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { SidebarLayoutSection, SidebarSectionSort } from "@/workspaceMembershipWire.generated";
import { SIDEBAR_SECTION_LIMITS } from "./sidebarLayoutModel";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { useSidebarCustomizing } from "./sidebarCustomizing";
import { styles } from "./sidebarStyles";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";

const SORTS: readonly SidebarSectionSort[] = ["manual", "edited"];

/**
 * A sidebar section's "…" menu, as Notion's (seen 2026-10-01, and with its
 * icons and dividers 2026-10-07; 220px 2026-10-30): "정렬" and "표시" with
 * their values, each opening its choices beside the menu, then "위로 옮기기",
 * "아래로 옮기기", "섹션 제거" and "사이드바 사용자 지정" — Notion's "탭으로 이동" goes with
 * its tabs (the one-sidebar decision of 2026-10-29). Only the private and
 * shared pages have a sort; the favorites keep the reader's order, set by
 * dragging (seen 2026-11-11 and 2026-11-13), and the recent pages and the
 * teamspaces have none. In a customize list it opens from ⚙ and the row's × removes the
 * section (sidebarCustomizing.ts). A database view's menu is its own
 * (SidebarViewSectionMenu).
 */
export function SidebarSectionMenu({
  name,
  section,
  canMoveUp,
  canMoveDown,
  onChange,
  onMove,
  onRemove,
  onCustomize,
  onCreateTeamspace,
}: {
  /** The section's name, for the button's label. */
  name: string;
  section: SidebarLayoutSection;
  canMoveUp: boolean;
  canMoveDown: boolean;
  onChange: (change: Partial<Pick<SidebarLayoutSection, "sort" | "limit">>) => void;
  onMove: (step: -1 | 1) => void;
  onRemove: () => void;
  /** Opens the sidebar's customize mode; absent while it is open. */
  onCustomize?: () => void;
  /** The teamspaces section's "새 팀스페이스", for one who may make one. */
  onCreateTeamspace?: () => void;
}) {
  const { t } = useTranslation("sidebar");
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const customizing = useSidebarCustomizing();
  const sortName = (sort: SidebarSectionSort) => t(sort === "edited" ? "sectionSortEdited" : "sectionSortManual");

  // Focus goes back to "…" unless what was chosen takes it.
  function close(restoreFocus = true) {
    setOpen(false);
    if (restoreFocus) focusOpenerAfterPaint(button.current, { preventScroll: true });
  }

  const action = (key: string) => ({ "data-sidebar-section-action": key });
  const icon = (Icon: typeof Plus) => <Icon size="regular" aria-hidden="true" />;
  const rows: SidebarMenuRow[] = [
    // Only the private and shared pages sort; the developer links have no count either.
    ...(section.id !== "private" && section.id !== "shared" ? [] : [{ key: "sort", label: t("sectionSort"), icon: icon(SortIcon), value: sortName(section.sort), data: action("sort"),
      submenu: { label: t("sectionSort"), radio: true, data: { "data-sidebar-section-choices": "sort" }, choices: SORTS.map((sort) => ({
        key: sort, label: sortName(sort), checked: section.sort === sort, data: { "data-sidebar-section-choice": sort },
        run: () => { onChange({ sort }); close(); } })) } }]),
    ...(section.id === "developer" ? [] : [{ key: "limit", label: t("sectionShow"), icon: icon(HashIcon), value: String(section.limit), data: action("limit"),
      submenu: { label: t("sectionShow"), radio: true, data: { "data-sidebar-section-choices": "limit" }, choices: SIDEBAR_SECTION_LIMITS.map((limit) => ({
        key: String(limit), label: String(limit), checked: section.limit === limit, data: { "data-sidebar-section-choice": String(limit) },
        run: () => { onChange({ limit }); close(); } })) } }]),
    ...(onCreateTeamspace ? [{ key: "create-teamspace", label: t("newTeamspace"), icon: icon(Plus), data: action("create-teamspace"),
      run: () => { close(false); onCreateTeamspace(); } }] : []),
    // A move with nowhere to go isn't offered, as Notion's first and last sections (seen 2026-10-30).
    ...(canMoveUp ? [{ key: "move-up", label: t("sectionMoveUp"), icon: icon(ArrowUp), data: action("move-up"), run: () => { onMove(-1); close(); } }] : []),
    ...(canMoveDown ? [{ key: "move-down", label: t("sectionMoveDown"), icon: icon(ArrowDown), data: action("move-down"), run: () => { onMove(1); close(); } }] : []),
    ...(customizing ? [] : [{ key: "remove", label: t("sectionRemove"), icon: icon(X), data: action("remove"), run: () => { close(); onRemove(); } }]),
    ...(onCustomize ? [{ key: "customize", label: t("customizeSidebar"), icon: icon(SlidersIcon), separatorBefore: true, data: action("customize"), run: () => { close(); onCustomize(); } }] : []),
  ];
  // A line parts the moves from the rows above them, when there are any.
  const moves = rows.findIndex((row) => row.key === "move-up" || row.key === "move-down" || row.key === "remove");
  if (moves > 0) rows[moves] = { ...rows[moves], separatorBefore: true };
  // With nothing to set or move, a customize row has its × alone.
  if (rows.length === 0) return null;

  return (
    <>
      <button ref={button} type="button" className={styles.sectionAction} data-sidebar-section-menu={section.id}
        aria-label={t("sectionMenu", { name })} aria-haspopup="menu" aria-expanded={open}
        onClick={() => (open ? close() : setOpen(true))}>
        {customizing ? <SlidersIcon size="small" aria-hidden="true" /> : <DotsHorizontal size="small" aria-hidden="true" />}
      </button>
      {open ? createPortal(<SidebarActionMenu button={button} label={t("sectionMenu", { name })} rows={rows}
        backdropLabel={t("closeSectionMenu")} close={close} />, document.body) : null}
    </>
  );
}
