import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import { HoverTip } from "@/components/IconTipButton";
import { TeamspaceRequestWindow } from "./TeamspaceRequestWindow";
import type { DialogTeamspace, TeamspaceLeaveLock } from "./teamspaceDialogModel";
import styles from "./TeamspaceJoinButton.module.css";

/**
 * The pill beside a teamspace's name — in its settings window's head and in
 * a page path's teamspace window — as Notion's (its code, read 2026-11-17:
 * one button for both, which picks by the reader's place; seen 2026-11-17 in
 * the settings window's head, its tip over it):
 * - for its members "✓ 참여 중", which leaves it as the sidebar's "팀스페이스
 *   나가기" does, its tip "팀스페이스에서 나가려면 클릭하세요."; where one may
 *   not leave (TeamspaceLeaveLock) it is dim, its tip saying why, wrapped at
 *   240;
 * - for someone outside an open one, or a workspace admin, "참여하기" — the
 *   server's canJoin, which also lets in at once the workspace's owner
 *   standing in for a teamspace no owner answers (teamspace_stand_in_join.go);
 * - for someone outside a closed one "요청", which opens the request window
 *   (TeamspaceRequestWindow), but not the one a request would reach alone;
 * - nothing for the rest (a private one, for instance).
 */
export function TeamspaceJoinButton({ teamspace, leaveLock, busy = false, onJoin, onRequest, onLeave, onAskingChange }: {
  teamspace: Pick<DialogTeamspace, "name" | "icon" | "joined" | "canJoin" | "access" | "canRequest" | "requestPending" | "membershipGroups">;
  leaveLock: TeamspaceLeaveLock;
  /** A join or a leave is on its way: the press shows it, and waits. */
  busy?: boolean;
  onJoin(): void;
  /** Sends the request with what the window's message says; true once it is in. */
  onRequest(message: string): Promise<boolean>;
  onLeave(): void;
  /** The request window opened or closed, for a window it stands in to keep open meanwhile. */
  onAskingChange?(asking: boolean): void;
}) {
  const { t } = useTranslation(["teamspaceDialog", "topBar"]);
  const [asking, setAskingState] = useState(false);
  function setAsking(next: boolean) {
    setAskingState(next);
    onAskingChange?.(next);
  }
  if (teamspace.joined) {
    const tip = leaveLock === "group"
      ? t("teamspaceDialog:manageSection.leaveGroup", { groupNames: (teamspace.membershipGroups ?? []).join(",") })
      : t(leaveLock === "default" ? "teamspaceDialog:manageSection.leaveDefault"
        : leaveLock === "onlyOwner" ? "teamspaceDialog:manageSection.leaveOnlyOwner" : "teamspaceDialog:manageSection.leaveTip");
    const dim = Boolean(leaveLock) || busy;
    return (
      <HoverTip label={tip} placement="top" width={leaveLock ? 240 : undefined} className={styles.slot}>
        <button type="button" className={styles.pill} aria-disabled={dim || undefined} aria-busy={busy || undefined}
          data-teamspace-joined data-teamspace-leave-lock={leaveLock || undefined}
          onClick={() => { if (!dim) onLeave(); }}>
          <CheckIcon size="small" aria-hidden="true" />
          {t("teamspaceDialog:head.joined")}
        </button>
      </HoverTip>
    );
  }
  if (teamspace.canJoin) {
    return (
      <button type="button" className={styles.pill} aria-disabled={busy || undefined} aria-busy={busy || undefined}
        data-teamspace-join onClick={() => { if (!busy) onJoin(); }}>
        {t("topBar:teamspaceJoin")}
      </button>
    );
  }
  if (teamspace.access !== "closed" || !(teamspace.canRequest || teamspace.requestPending)) return null;
  return (
    <>
      <button type="button" className={styles.pill} aria-haspopup="dialog" aria-expanded={asking}
        data-teamspace-request onClick={() => setAsking(true)}>
        {t("topBar:teamspaceRequest")}
      </button>
      {asking ? <TeamspaceRequestWindow teamspace={teamspace} onRequest={onRequest} onClose={() => setAsking(false)} /> : null}
    </>
  );
}
