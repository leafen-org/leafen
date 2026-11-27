import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { DotsHorizontal } from "@/icons/leafen";

import groupStyles from "./WorkspaceGroups.module.css";

/**
 * A settings table row's actions behind one 28px "…" button, as the members
 * table keeps them: no row of word buttons. A row closes the menu before it
 * runs, so a window it opens takes the keys. Nothing to do draws no button.
 */
export function SettingsRowMenu({ label, rows, width = 220, data, disabled = false }: {
  label: string; rows: SidebarMenuRow[]; width?: number; data?: Record<string, string>; disabled?: boolean;
}) {
  const { t } = useTranslation("common");
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  if (!rows.length) return null;
  // Focus goes back to the "…" button before a row runs, so a window the row
  // opens hands it back there when it closes, not to the page.
  const closing = rows.map((row) => row.run ? { ...row, run: () => { setOpen(false); button.current?.focus(); row.run?.(); } } : row);
  return (
    <>
      <button ref={button} type="button" className={groupStyles.iconButton} aria-haspopup="menu" aria-expanded={open}
        aria-label={label} disabled={disabled} onClick={() => setOpen((value) => !value)} {...data}>
        <DotsHorizontal size="regular" aria-hidden="true" />
      </button>
      {open && !disabled ? (
        <SidebarActionMenu button={button} rows={closing} width={width} label={label}
          backdropLabel={t("actions.close")} close={() => setOpen(false)} />
      ) : null}
    </>
  );
}
