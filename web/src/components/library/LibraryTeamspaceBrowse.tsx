import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CaretRightFill, DescriptionIcon, DotsHorizontal, GridIcon, NetworkIcon, PersonCircleIcon } from "@/icons/leafen";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { teamspaceMenuRows } from "@/components/sidebar/SidebarTeamspaceMenu";
import type { SidebarTeamspaceAction } from "@/components/sidebar/useSidebarTeamspaceActions";
import { TeamspaceKindIcon } from "@/components/teamspace/TeamspaceSecurityIcon";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { teamspaceSecurity, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { focusOpener } from "@/lib/overlayLayer";
import { LibraryNoResults } from "./LibraryEmpty";
import panelStyles from "./LibraryPanel.module.css";
import styles from "./LibraryView.module.css";
import browseStyles from "./LibraryTeamspaces.module.css";

const GROUP_GUTTER = 24;

/**
 * The teamspaces table's head: name, description, kind and members, each
 * with its filled mark. Grouped, each group draws its own under its name,
 * the joined ones' with the box that chooses them all and the others' without
 * one, as Notion's (its code, read 2026-11-16: the group's body is the head
 * and then its rows).
 */
export function LibraryTeamspaceHead({ selectBox }: { selectBox?: ReactNode }) {
  const { t } = useTranslation("library");
  return (
    <div className={styles.headRow} role="row" data-library-teamspace-head>
      {selectBox}
      <span className={`${styles.nameColumn} ${browseStyles.nameHead}`} role="columnheader">
        <span className={styles.columnStatic}><GridIcon size="page" aria-hidden="true" /><span className={styles.headLabel}>{t("teamspaceColumns.name")}</span></span>
      </span>
      <span className={`${styles.column} ${browseStyles.description}`} role="columnheader">
        <span className={styles.columnStatic}><DescriptionIcon size="page" aria-hidden="true" /><span className={styles.headLabel}>{t("teamspaceColumns.description")}</span></span>
      </span>
      <span className={`${styles.column} ${browseStyles.security}`} role="columnheader">
        <span className={styles.columnStatic}><NetworkIcon size="page" aria-hidden="true" /><span className={styles.headLabel}>{t("teamspaceColumns.security")}</span></span>
      </span>
      <span className={`${styles.column} ${browseStyles.members}`} role="columnheader">
        <span className={styles.columnStatic}>
          <PersonCircleIcon size="page" weight="fill" aria-hidden="true" /><span className={styles.headLabel}>{t("teamspaceColumns.members")}</span>
        </span>
      </span>
    </div>
  );
}

/**
 * A fold of the library's teamspaces once there are teamspaces to join, as
 * Notion's (seen 2026-11-12): "▼ 내 팀스페이스" over those the reader is in,
 * "▶ 팀스페이스 더 보기" — folded at first — over those they may join. Open,
 * its body (LibraryTeamspaceGroupBody) holds the table's head and its rows.
 */
export function LibraryTeamspaceGroup({ group, open, onToggle }: { group: "mine" | "more"; open: boolean; onToggle(): void }) {
  const { t } = useTranslation("library");
  const label = t(group === "mine" ? "myTeamspaces" : "moreTeamspaces");
  return (
    <div className={`${styles.row} ${panelStyles.groupRow}`} role="row" data-library-teamspace-group={group}>
      {/* Laid out as the other views' groups (LibraryGroupRow), without their count. */}
      <span className={styles.nameColumn} role="cell" style={{ paddingInlineStart: GROUP_GUTTER }}>
        <button type="button" className={panelStyles.rowToggle} style={{ insetInlineStart: 0 }} aria-expanded={open}
          aria-label={t("openRow", { title: label })} data-library-teamspace-group-toggle={group} onClick={onToggle}>
          <CaretRightFill aria-hidden="true" />
        </button>
        <span className={panelStyles.groupLabel}>{label}</span>
      </span>
    </div>
  );
}

/**
 * An open group's head and rows, 8px in from the group's name, as Notion's
 * (its code, read 2026-11-16: the body's paddingInlineStart 8 while grouped).
 */
export function LibraryTeamspaceGroupBody({ group, children }: { group: "mine" | "more"; children: ReactNode }) {
  return <div className={browseStyles.groupBody} role="rowgroup" data-library-teamspace-group-body={group}>{children}</div>;
}

/**
 * A group of the teamspaces table with no rows to show — the reader's own,
 * or those to join while a search leaves none — as Notion's (its code, read
 * 2026-11-17): under its head, while searching or where the reader may make
 * a teamspace, the band of "검색 초기화" and "+ 새 팀스페이스"; otherwise one
 * row of 12px tertiary words 8px into the name column, "참여한 팀스페이스
 * 없음" for the reader's own and "참여할 팀스페이스 없음" for the others.
 */
export function LibraryTeamspaceGroupEmpty({ group, onClearSearch, onNew }: {
  group: "mine" | "more"; onClearSearch?: () => void; onNew?: () => void;
}) {
  const { t } = useTranslation("library");
  if (onClearSearch || onNew) return <LibraryNoResults onClearSearch={onClearSearch} newLabel={t("newTeamspace")} onNew={onNew} />;
  return (
    <div className={`${styles.row} ${browseStyles.noneRow}`} role="row" data-library-teamspace-none={group}>
      <span className={`${styles.nameColumn} ${browseStyles.noneCell}`} role="cell">
        {t(group === "mine" ? "noJoinedTeamspaces" : "noTeamspacesToJoin")}
      </span>
    </div>
  );
}

/**
 * A teamspace the reader may join, a row of the table's kind without the
 * handle or the box: its "▶" lists its pages as a joined one's does, and
 * its "⋯" holds Notion's "+ 팀스페이스 참여 · 멤버 보기 · 팀스페이스 복제"
 * (teamspaceMenuRows, seen 2026-11-12). A touch screen's row has no "⋯",
 * nor a menu from a press, as Notion's phone and iPad (seen 2026-11-16 at
 * 402 and 834); Notion's code (read 2026-11-16) gives every row its "▶".
 */
export function LibraryTeamspaceBrowseRow({ teamspace, canCreate, expanded, touch, onToggle, onManage, onAct }: {
  teamspace: DialogTeamspace; canCreate: boolean;
  /** Its pages are listed under it. */
  expanded: boolean;
  touch: boolean;
  onToggle(): void;
  onManage(teamspaceId: string, invite: boolean): void; onAct(teamspace: DialogTeamspace, action: SidebarTeamspaceAction): void;
}) {
  const { t } = useTranslation(["sidebar", "teamspaceDialog", "common"]);
  const { t: tl } = useTranslation("library");
  const button = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState(false);
  function close(restoreFocus = true) {
    setMenu(false);
    if (restoreFocus) focusOpener(button.current, { preventScroll: true });
  }
  const security = teamspaceSecurity(teamspace);
  return (
    <div className={browseStyles.row} role="row" data-library-teamspace-browse={teamspace.id}>
      <span className={`${styles.nameColumn} ${browseStyles.nameCell}`} role="cell">
        <button type="button" className={`${panelStyles.rowToggle} ${browseStyles.toggle}`} aria-expanded={expanded}
          aria-label={tl("openTeamspace", { name: teamspace.name })} data-library-teamspace-toggle={teamspace.id} onClick={onToggle}>
          <CaretRightFill aria-hidden="true" />
        </button>
        <span className={browseStyles.glyph} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
        <span className={browseStyles.name}>{teamspace.name}</span>
        {touch ? null : (
          <button ref={button} type="button" className={`${browseStyles.more} ${menu ? browseStyles.moreOpen : ""}`}
            aria-haspopup="menu" aria-expanded={menu}
            aria-label={tl("moreTeamspaceSettings", { name: teamspace.name })} data-library-teamspace-browse-menu
            onClick={() => setMenu((current) => !current)}>
            <DotsHorizontal size="small" aria-hidden="true" />
          </button>
        )}
      </span>
      <LibraryTeamspaceDescription teamspace={teamspace} />
      <span className={`${styles.column} ${browseStyles.security}`} role="cell">
        <TeamspaceKindIcon security={security} size="regular" />
        <span className={styles.cellText}>{t(`teamspaceDialog:security.${security}`)}</span>
      </span>
      <span className={`${styles.column} ${browseStyles.members}`} role="cell">{teamspace.summary.memberCount}</span>
      {menu && !touch ? createPortal(<SidebarActionMenu button={button} label={tl("moreTeamspaceSettings", { name: teamspace.name })}
        backdropLabel={t("common:actions.close")} close={close} contentWidth
        rows={teamspaceMenuRows({ t, teamspace, canCreate, onManage, onAct, close })} />, document.body) : null}
    </div>
  );
}

/** A teamspace's description in its row of the table, as Notion's library (seen 2026-11-17). */
export function LibraryTeamspaceDescription({ teamspace }: { teamspace: Pick<DialogTeamspace, "description"> }) {
  return (
    <span className={`${styles.column} ${browseStyles.description}`} role="cell">
      <span className={styles.cellText}>{teamspace.description}</span>
    </span>
  );
}
