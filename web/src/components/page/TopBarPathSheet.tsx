import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PathChildIcon } from "@/icons/leafen";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { useKeptTeamspaces } from "@/lib/workspaceTeamspaces";
import type { Page } from "@/types";
import { teamspaceIdOf } from "./TopBarTeamspaceCrumb";
import styles from "./TopBarPathSheet.module.css";

/**
 * A page's mark in the sheet, as Notion's iPhone draws it (measured
 * 2026-11-17 at 402 in iPhone Safari, twice, a teamspace page's path and a
 * shared page's): its glyph nine tenths of a 24px place, 21.6px.
 */
export const PATH_SHEET_MARK = 24 * 0.9;

export type TopBarPathSheetRow = {
  key: string;
  /** The page's own mark, at PATH_SHEET_MARK. */
  icon: ReactNode;
  label: ReactNode;
  onPress(): void;
};

/**
 * A phone's "이동 경로", as Notion's (seen 2026-10-27; its rows again
 * 2026-11-17 at 402 in iPhone Safari, twice): its rows the sheets' own, 17px
 * words in 45px rows (a 390 emulation once read 16px), and a teamspace page's path starts
 * with its teamspace — its letter or icon in a 21px grey square, its name —
 * and pressing that row only closes the sheet, as Notion's does (it opens no
 * teamspace window there, on the teamspace's home page or under it). Every
 * row under the first begins with a ↳ in the first row's mark place, the
 * page's own mark after it and then its name, Notion's 16px ↳ 16px in, the
 * mark's 24px place from 44 and the name 10px after it.
 */
export function TopBarPathSheet({ page, pages, userId, rows, onClose }: {
  page: Page;
  pages: readonly Page[];
  userId: string;
  rows: readonly TopBarPathSheetRow[];
  onClose(): void;
}) {
  const { t } = useTranslation("topBar");
  const teamspace = usePathTeamspace(page, pages, userId);
  return (
    <PhoneSheet size="half" title={t("pathSheet")} onClose={onClose} data={{ "data-topbar-path-sheet": "true" }}>
      <PhoneSheetGroup>
        {teamspace ? (
          <PhoneSheetRow more={false} data={{ "data-topbar-path-teamspace": teamspace.id }}
            icon={<span className={styles.teamspaceMark}>{teamspaceGlyph(teamspace)}</span>}
            label={teamspace.name} onPress={onClose} />
        ) : null}
        {rows.map((row, index) => index > 0 || teamspace ? (
          <PhoneSheetRow key={row.key} more={false} icon={<PathChildIcon className={styles.childArrow} />}
            label={<span className={styles.childLabel}><span className={styles.childMark} aria-hidden="true">{row.icon}</span>{row.label}</span>}
            onPress={row.onPress} />
        ) : (
          <PhoneSheetRow key={row.key} more={false} icon={row.icon} label={row.label} onPress={row.onPress} />
        ))}
      </PhoneSheetGroup>
    </PhoneSheet>
  );
}

/** The teamspace a page's path starts in, as the sidebar last read it: an archived one too, as the bar's piece (TopBarTeamspaceCrumb). */
function usePathTeamspace(page: Page, pages: readonly Page[], userId: string) {
  const kept = useKeptTeamspaces(userId, page.workspaceId);
  const teamspaceId = teamspaceIdOf(page, pages);
  if (!teamspaceId || !kept) return undefined;
  return kept.teamspaces.find((item) => item.id === teamspaceId) ?? kept.archived?.find((item) => item.id === teamspaceId);
}
