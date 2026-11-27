import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { changeSidebarLayout } from "@/components/sidebar/useSidebarLayout";
import { orderTeamspaces, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceLeaveQuestion } from "@/components/teamspace/teamspaceLeaveQuestion";
import { newId } from "@/lib/ids";
import { useProductToast } from "@/lib/productToast";
import { requestTeamspacesRead, type WorkspaceTeamspaces } from "@/lib/workspaceTeamspaces";
import { createTeamspaceApi } from "@/teamspaceApi";
import { teamspaceErrorCodes } from "@/teamspaceWire.generated";
import type { Session } from "@/types";

/**
 * "참여하기" in a page's teamspace window, for an open teamspace the reader
 * isn't in, as Notion's (its breadcrumb window's code, read 2026-11-17): the
 * reader joins where they are, told "〈팀〉 팀에 참여했습니다." at the
 * window's foot, the teamspace last in their sidebar order as the library's
 * "팀스페이스 참여" puts it, and the sidebar reads the teamspaces again.
 * Joining is the server's to decide first; the press shows at once as busy.
 */
export function useTeamspaceCrumbJoin(session: Session, workspaceId: string, kept: WorkspaceTeamspaces | null) {
  const { t } = useTranslation("teamspaceDialog");
  const { notify } = useProductToast();
  const [busy, setBusy] = useState(false);
  const operationRef = useRef<AbortController | null>(null);

  useEffect(() => () => operationRef.current?.abort(), []);

  async function join(teamspace: DialogTeamspace) {
    if (busy || teamspace.joined || !teamspace.canJoin) return;
    const controller = new AbortController();
    operationRef.current = controller;
    setBusy(true);
    try {
      await createTeamspaceApi().mutateTeamspace(session, workspaceId, {
        action: "join", teamspaceId: teamspace.id, expectedRevision: teamspace.revision, requestId: newId(),
      }, controller.signal);
      if (controller.signal.aborted) return;
      notify(t("notice.joinedName", { name: teamspace.name }), "success");
      const joined = (kept?.teamspaces ?? []).filter((item) => item.joined && !item.archivedAt && item.id !== teamspace.id);
      void changeSidebarLayout(session, workspaceId, (layout) => ({
        ...layout, teamspaceOrder: [...orderTeamspaces(joined, layout.teamspaceOrder).map((item) => item.id), teamspace.id],
      })).catch(() => undefined);
      requestTeamspacesRead(workspaceId);
    } catch (cause) {
      if (controller.signal.aborted) return;
      const code = cause instanceof LeafenApiError ? cause.code : "";
      notify(t(code === teamspaceErrorCodes.conflict ? "error.conflict" : "error.action"), "error");
    } finally {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setBusy(false);
      }
    }
  }

  /**
   * A closed teamspace's "요청" window sends this, as Notion's: its owners
   * get the request (their inbox and the teamspace's requests), the reader
   * "멤버십 요청 완료" at the window's foot. A request already waiting stands
   * as sent. What the window's message says goes with it.
   */
  async function request(teamspace: DialogTeamspace, message = "") {
    if (teamspace.joined) return false;
    if (teamspace.requestPending) {
      notify(t("notice.membershipRequested"), "success");
      return true;
    }
    if (!teamspace.canRequest) return false;
    const controller = new AbortController();
    operationRef.current?.abort();
    operationRef.current = controller;
    try {
      await createTeamspaceApi().mutateTeamspace(session, workspaceId, {
        action: "request", teamspaceId: teamspace.id, expectedRevision: teamspace.revision, requestId: newId(),
        ...(message.trim() ? { joinMessage: message.trim() } : {}),
      }, controller.signal);
      if (controller.signal.aborted) return false;
      notify(t("notice.membershipRequested"), "success");
      requestTeamspacesRead(workspaceId);
      return true;
    } catch {
      if (!controller.signal.aborted) notify(t("error.membershipRequest"), "error");
      return false;
    } finally {
      if (operationRef.current === controller) operationRef.current = null;
    }
  }

  /**
   * Its "✓ 참여 중" leaves the teamspace as the sidebar's "팀스페이스 나가기"
   * does (TeamspaceJoinButton): asked first (teamspaceLeaveQuestion), then
   * "〈팀〉 팀에서 나옴" at the window's foot, and the sidebar reads the
   * teamspaces again. Leaving is the server's to decide first.
   */
  async function leave(teamspace: DialogTeamspace) {
    if (busy || !teamspace.joined || !await confirmAction(teamspaceLeaveQuestion(teamspace.name))) return;
    const controller = new AbortController();
    operationRef.current = controller;
    setBusy(true);
    try {
      await createTeamspaceApi().mutateTeamspace(session, workspaceId, {
        action: "leave", teamspaceId: teamspace.id, expectedRevision: teamspace.revision, requestId: newId(),
      }, controller.signal);
      if (controller.signal.aborted) return;
      notify(t("notice.leftName", { name: teamspace.name }), "success");
      requestTeamspacesRead(workspaceId);
    } catch (cause) {
      if (controller.signal.aborted) return;
      const code = cause instanceof LeafenApiError ? cause.code : "";
      notify(t(code === teamspaceErrorCodes.lastOwner ? "leave.lastOwner" : code === teamspaceErrorCodes.conflict ? "error.conflict" : "error.action"), "error");
    } finally {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setBusy(false);
      }
    }
  }

  return { busy, join, leave, request };
}
