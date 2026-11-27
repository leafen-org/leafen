import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { HoverTip } from "@/components/IconTipButton";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { securityKinds, teamspaceSecurity, type DialogTeamspace, type TeamspaceSecurity } from "@/components/teamspace/teamspaceDialogModel";
import { useSidebarLayout } from "@/components/shell/AppShell";
import styles from "./WorkspaceTeamspacesSection.module.css";
import { SettingsChevron } from "./SettingsChevron";
import { SettingsPhoneMenuScreen } from "./SettingsPhoneMenuScreen";

/**
 * A row's "공개 수준" in the settings teamspace table, as Notion's (seen
 * 2026-11-11, its rule from its code): its kind and a ⌄ that opens 기본 ·
 * 공개 · 참가 제한 · 비공개 ("기본" only for the workspace's owners, or on a
 * default one), 180px wide under the button's start, the kind it is now lit.
 * One the reader may not change — a teamspace they don't own, even as the
 * workspace's owner, or a default one while the reader doesn't own the
 * workspace or it is the only default — is dim, does not open, and a tip
 * before it says who may change it. On a phone its choices take the whole
 * screen settings menus take there (SettingsPhoneMenuScreen).
 */
export function WorkspaceTeamspaceAccessMenu({ teamspace, canChange, workspaceAdmin, locked, busy, onChange }: {
  teamspace: DialogTeamspace;
  /** Whether the reader owns the teamspace: only its owners change its access level, as Notion's. */
  canChange: boolean;
  /** Only the workspace's owners make a teamspace the default. */
  workspaceAdmin: boolean;
  /** A default teamspace that stays so (defaultSecurityLock). */
  locked: boolean;
  busy: boolean;
  onChange(kind: TeamspaceSecurity): void;
}) {
  const { t } = useTranslation(["teamspaceDialog", "workspaceSettingsDialog", "common"]);
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const kind = teamspaceSecurity(teamspace);
  const label = (value: TeamspaceSecurity) => value === "default" ? t("teamspaceDialog:security.default") : t(`teamspaceDialog:access.${value}`);
  const fixed = !canChange || locked;
  const { phone } = useSidebarLayout(true);
  const rows = securityKinds(workspaceAdmin, teamspace.isDefault).map((value) => ({
    key: value, label: label(value), data: { "data-teamspace-access-choice": value },
    run: () => { setOpen(false); if (value !== kind) onChange(value); },
  }));
  const trigger = (
    <button ref={button} type="button" className={styles.accessButton} disabled={fixed || busy} aria-haspopup="menu" aria-expanded={open}
      aria-label={t("workspaceSettingsDialog:teamspacesAccess")} data-teamspace-access={kind} data-fixed={fixed ? "true" : undefined}
      onClick={() => setOpen((current) => !current)}>
      <span>{label(kind)}</span><SettingsChevron filled />
    </button>
  );
  return <>
    {fixed ? <HoverTip label={t("workspaceSettingsDialog:teamspacesAccessOwnersOnly")} placement="left" width={200}>{trigger}</HoverTip> : trigger}
    {open && phone ? (
      <SettingsPhoneMenuScreen title={t("workspaceSettingsDialog:teamspacesAccess")} current={kind} rows={rows} close={() => setOpen(false)} />
    ) : open ? createPortal(<SidebarActionMenu button={button} label={t("workspaceSettingsDialog:teamspacesAccess")}
      backdropLabel={t("common:actions.close")} close={() => setOpen(false)} width={180} gap={0} current={kind}
      rows={rows} />, document.body) : null}
  </>;
}
