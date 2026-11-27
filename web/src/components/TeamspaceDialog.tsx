"use client";

import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Session, Workspace } from "@/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useClosedForLink } from "@/lib/productWindows";
import { X } from "@/icons/leafen";
import { TeamspaceCreatePanel, type TeamspaceFromGroup } from "./teamspace/TeamspaceCreatePanel";
import { TeamspaceInviteDialog } from "./teamspace/TeamspaceInviteDialog";
import { TeamspaceSettingsPanel, type TeamspaceSettingsTab } from "./teamspace/TeamspaceSettingsPanel";
import { useTeamspaceDialog } from "./teamspace/useTeamspaceDialog";
import styles from "./TeamspaceDialog.module.css";
import settingsStyles from "./teamspace/TeamspaceSettings.module.css";

/** What the window opens on: making a teamspace, or one teamspace's settings. */
export type TeamspaceDialogView = "create" | "manage";

type TeamspaceDialogProps = {
  session: Session;
  workspace: Workspace;
  initialTeamspaceId?: string;
  initialView?: TeamspaceDialogView;
  /** Opens on the teamspace's invite dialog, as the sidebar's "멤버 추가". */
  initialInvite?: boolean;
  /** The settings tab it opens at, as a notice about the teamspace asks. */
  initialTab?: TeamspaceSettingsTab;
  /** Opens on making a teamspace from a group, named and marked as it is, the group its member. */
  fromGroup?: TeamspaceFromGroup;
  /** Opened from 설정 › 팀스페이스, whose rows are every active teamspace for the workspace's owner. */
  everyTeamspace?: boolean;
  /** A teamspace made here, before the window asks whom to invite (an import's "+ 새 팀스페이스" takes it as its place). */
  onCreated?: (teamspace: { id: string; name: string; icon: string }) => void;
  onClose?: () => void;
};

/**
 * The window that makes a teamspace or holds one's settings. Browsing the
 * teamspaces to join is the library's "팀스페이스 더 보기" and the archived
 * ones are 설정 › 팀스페이스's "보관됨", as Notion's (its code and seen
 * 2026-11-17: its section's "↗" opens the library, and nothing else lists
 * archived teamspaces), so the window has no list of its own; once its
 * teamspace is archived or left it closes.
 */
export function TeamspaceDialog({
  session,
  workspace,
  initialTeamspaceId,
  initialView = "manage",
  initialInvite = false,
  initialTab,
  fromGroup,
  everyTeamspace = false,
  onCreated,
  onClose,
}: TeamspaceDialogProps) {
  const { t } = useTranslation(["teamspaceDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const createNameRef = useRef<HTMLInputElement>(null);
  const createIconRef = useRef<HTMLButtonElement>(null);
  const controller = useTeamspaceDialog({ session, workspace, initialTeamspaceId, initialView: fromGroup ? "create" : initialView, everyTeamspace });
  const { actionBusy } = controller;
  // A teamspace made from a group starts with the group's name and mark,
  // once for each group: a caller drawing again, with a new value for the
  // same group, leaves what the person changed since. It starts 참가 제한,
  // as Notion's (seen 2026-11-11): the group is who is in it.
  const { setName, setIcon, setAccess, setOpenPageRole } = controller;
  const fromGroupId = fromGroup?.id;
  useEffect(() => {
    if (!fromGroup) return;
    setName(fromGroup.name.slice(0, 80));
    setIcon(fromGroup.icon ?? "");
    setAccess("closed");
    setOpenPageRole("none");
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only another group starts again
  }, [fromGroupId, setName, setIcon, setAccess, setOpenPageRole]);
  // As in Notion, a teamspace just made asks whom to invite before it closes.
  const [invitingCreated, setInvitingCreated] = useState(false);
  // One made from a group has its members, so it asks no one: its own
  // window opens in place at "설정", as Notion's (seen 2026-11-16 in its free
  // workspace, 1440 dark: "✓ 참여 중", closing it leaves the groups list).
  // A create answered goes to its teamspace by itself (the view as last
  // drawn), one found without an answer still needs taking there.
  const viewNow = useRef(controller.view);
  useEffect(() => { viewNow.current = controller.view; }, [controller.view]);
  // The sidebar's "멤버 추가" opens the invite window alone, as Notion's (seen 2026-10-10).
  // A teamspace archived or left takes the settings away as the window closes.
  const layout = invitingCreated || initialInvite ? "invite"
    : controller.view === "manage" ? "settings" : controller.view === "create" ? "create" : null;

  // A modal layer: the page stays still and Escape closes it while nothing
  // opened over it takes the key.
  // A link going to a place here closes it as its × does (closeWindowsForLink).
  useClosedForLink(() => { if (!actionBusy) onClose?.(); });
  const { zIndex } = useOverlayLayer(layout === "settings" || layout === "create", {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!actionBusy) onClose?.(); },
    // It opens on its icon with nothing lit, as Notion's (seen 2026-11-05): the name is the next Tab.
    initialFocus: layout === "create" ? createIconRef : closeRef,
  });

  const closeButton = (
    <button
      ref={closeRef}
      type="button"
      className={styles.close}
      onClick={onClose}
      disabled={actionBusy}
      aria-label={t("common:actions.close")}
    >
      <X size="small" aria-hidden="true" />
    </button>
  );

  if (layout === "invite") {
    return <TeamspaceInviteDialog controller={controller} skippable={invitingCreated} onClose={() => onClose?.()} />;
  }
  if (!layout || typeof document === "undefined") return null;

  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }}>
      <button
        type="button"
        className={styles.backdrop}
        onClick={() => !actionBusy && onClose?.()}
        tabIndex={-1}
        aria-label={t("common:actions.close")}
      />
      <section
        ref={dialogRef}
        className={`${styles.dialog} ${layout === "settings" ? settingsStyles.settingsDialog : `${settingsStyles.createDialog}${fromGroup ? ` ${settingsStyles.createFromGroupDialog}` : ""}`}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={actionBusy || undefined}
        data-teamspace-management="dialog"
        data-teamspace-layout={layout}
      >
        {layout === "settings" ? (
          <TeamspaceSettingsPanel controller={controller} titleId={titleId} initialInvite={initialInvite}
            initialTab={fromGroup ? "settings" : initialTab} closeButton={closeButton}
            workspaceName={workspace.name} onDone={() => onClose?.()} />
        ) : (
          <TeamspaceCreatePanel controller={controller} titleId={titleId} closeButton={closeButton} workspaceName={workspace.name}
            nameRef={createNameRef} iconRef={createIconRef} fromGroup={fromGroup} onCancel={() => onClose?.()}
            onCreated={(made) => {
              onCreated?.(made);
              if (!fromGroup) setInvitingCreated(true);
              else if (viewNow.current !== "manage") controller.openView("manage");
            }} />
        )}
      </section>
    </div>,
    document.body,
  );
}
