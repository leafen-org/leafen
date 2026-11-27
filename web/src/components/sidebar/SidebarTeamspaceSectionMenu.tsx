import { ArrowDown, ArrowUp, DotsHorizontal, SlidersIcon, TeamspaceIcon, X } from "@/icons/leafen";
import { createPortal } from "react-dom";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { teamspaceMenuRows } from "./SidebarTeamspaceMenu";
import { useSidebarCustomizing } from "./sidebarCustomizing";
import type { SidebarTeamspaceAction } from "./useSidebarTeamspaceActions";
import { styles } from "./sidebarStyles";

/**
 * A teamspace's own section's "…", as Notion's (seen 2026-11-11; as wide as
 * its rows — 241px for a member who may not copy it, 282px with "팀스페이스
 * 복제"'s note):
 * "팀스페이스 ›" with the teamspace it shows, choosing another the reader is
 * in from a search, then the teamspace's own rows — "멤버 추가", "팀스페이스
 * 설정", copying, leaving and archiving it — then "위로 옮기기", "아래로
 * 옮기기", "섹션 제거" and "사이드바 사용자 지정". It sorts and counts nothing.
 * "팀스페이스 ›" joins the teamspace's first group of rows, with no line
 * between, as Notion's TeamOverflowMenu merges its extra first section into
 * its own first one (its code, 2026-11-11): over "멤버 추가" where that shows,
 * the line coming after it; over "팀스페이스 설정" and the rest where it does not.
 * In a customize list it opens from ⚙ and the row's × removes the section.
 */
export function SidebarTeamspaceSectionMenu({
  teamspace,
  choices,
  canCreate,
  canMoveUp,
  canMoveDown,
  onChoose,
  onMove,
  onRemove,
  onCustomize,
  onManage,
  onAct,
}: {
  teamspace: DialogTeamspace;
  /** The teamspaces it may show instead: its own and those without a section of their own. */
  choices: readonly DialogTeamspace[];
  canCreate: boolean;
  canMoveUp: boolean;
  canMoveDown: boolean;
  onChoose: (teamspaceId: string) => void;
  onMove: (step: -1 | 1) => void;
  onRemove: () => void;
  /** Opens the sidebar's customize mode; absent while it is open. */
  onCustomize?: () => void;
  onManage(teamspaceId: string, invite: boolean): void;
  onAct(teamspace: DialogTeamspace, action: SidebarTeamspaceAction): void;
}) {
  const { t } = useTranslation(["sidebar", "teamspaceDialog", "common"]);
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const customizing = useSidebarCustomizing();

  function close(restoreFocus = true) {
    setOpen(false);
    setQuery("");
    if (restoreFocus) focusOpenerAfterPaint(button.current, { preventScroll: true });
  }

  const action = (key: string) => ({ "data-sidebar-section-action": key });
  const needle = query.trim().toLocaleLowerCase();
  const rows: SidebarMenuRow[] = [
    { key: "teamspace", label: t("sidebar:teamspaces"), icon: <TeamspaceIcon size="regular" aria-hidden="true" />, value: teamspace.name,
      data: action("teamspace"), submenu: {
        label: t("sidebar:teamspaces"), width: 280, data: { "data-sidebar-section-choices": "teamspace" },
        search: { label: t("sidebar:searchTeamspaces"), query, onQuery: setQuery, bare: true },
        choices: choices.filter((choice) => !needle || choice.name.toLocaleLowerCase().includes(needle)).map((choice) => ({
          key: choice.id, label: choice.name, checked: choice.id === teamspace.id,
          icon: <span className={styles.sectionMenuTeamspaceIcon} aria-hidden="true">{teamspaceGlyph(choice)}</span>,
          data: { "data-sidebar-section-choice": choice.id }, run: () => {
            close();
            if (choice.id !== teamspace.id) onChoose(choice.id);
          },
        })),
      } },
    ...teamspaceMenuRows({ t, teamspace, canCreate, onManage, onAct, close }),
    ...(canMoveUp ? [{ key: "move-up", label: t("sidebar:sectionMoveUp"), icon: <ArrowUp size="regular" aria-hidden="true" />,
      data: action("move-up"), run: () => { onMove(-1); close(); } }] : []),
    ...(canMoveDown ? [{ key: "move-down", label: t("sidebar:sectionMoveDown"), icon: <ArrowDown size="regular" aria-hidden="true" />,
      data: action("move-down"), run: () => { onMove(1); close(); } }] : []),
    ...(customizing ? [] : [{ key: "remove", label: t("sidebar:sectionRemove"), icon: <X size="regular" aria-hidden="true" />,
      data: action("remove"), run: () => { close(); onRemove(); } }]),
    ...(onCustomize ? [{ key: "customize", label: t("sidebar:customizeSidebar"), icon: <SlidersIcon size="regular" aria-hidden="true" />,
      separatorBefore: true, data: action("customize"), run: () => { close(); onCustomize(); } }] : []),
  ];
  // A line parts the section's own rows from the teamspace's.
  const moves = rows.findIndex((row) => row.key === "move-up" || row.key === "move-down" || row.key === "remove");
  if (moves > 0) rows[moves] = { ...rows[moves], separatorBefore: true };
  const label = customizing ? t("sidebar:sectionMenu", { name: teamspace.name }) : t("sidebar:manageTeamspace", { name: teamspace.name });

  return (
    <>
      <button ref={button} type="button" className={styles.sectionAction} data-sidebar-section-menu="teamspace"
        aria-label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => (open ? close() : setOpen(true))}>
        {customizing ? <SlidersIcon size="small" aria-hidden="true" /> : <DotsHorizontal size="small" aria-hidden="true" />}
      </button>
      {open ? createPortal(<SidebarActionMenu button={button} label={label} rows={rows} contentWidth
        backdropLabel={t("common:actions.close")} close={close} />, document.body) : null}
    </>
  );
}
