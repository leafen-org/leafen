import { createElement, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { WarningIcon } from "@/icons/leafen/automationIcons";
import { useOptionalProductToast } from "@/lib/productToast";
import { movePageToWorkspace, planPageWorkspaceMove, type PageWorkspaceMovePlan } from "@/lib/pageWorkspaceMoveService";
import { organizationErrorCodes } from "@/organizationWire.generated";
import type { Page, Session, Workspace } from "@/types";
import type { MoveDestination } from "./pageMoveDialogModel";
import { useProductPages } from "./product/productPagesContext";
import { confirmActions } from "./ConfirmActionDialog";
import { useMoveCopy } from "./useMoveCopy";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";

/**
 * A page "옮기기" sends to another workspace on this server moves there, as
 * the user decided (2026-11-23, beyond Notion, which copies it — an approved
 * difference): it keeps its address, and the server cuts what can't come
 * along. The press closes the popover and says the move is being looked at;
 * the card then offers both, as the user asked (2026-11-23: "복제의 또다른
 * 유형으로 … 기존꺼도 두고"): "복제하기" first, Notion's own answer there —
 * the page stays and the other workspace gets a copy, by the copy's road
 * (useMoveCopy) — and "옮기기", marked as the one that takes the page away,
 * with what it will cut, counted by the server's plan, after its paragraph.
 * Moved, "페이지 옮기는 중" shows until the page is there, and it stays open
 * where it is. A database's page (`row`) goes there as a copy, as
 * before (useMoveCopy), and so does a page the person may not move — one they
 * lack full access to.
 */
export function useMoveToWorkspace(
  session: Session, movingPage: Page | undefined, to: Workspace | undefined, row: boolean, onStarted: () => void,
) {
  const { t } = useTranslation("moveToDialog");
  const productPages = useProductPages();
  const toast = useOptionalProductToast();
  const copy = useMoveCopy(session, movingPage, to, onStarted);
  return useCallback(async (destination: MoveDestination) => {
    if (row) return copy(destination);
    if (!movingPage || !to) return;
    const target = {
      workspaceId: to.id,
      parentId: destination.type === "page" ? destination.id : "",
      teamspaceId: destination.type === "place" ? destination.place.teamspaceId : "",
    };
    onStarted();
    const preparing = t("moveToDialog:move.preparing");
    toast?.notify(preparing, "default", undefined, "progress", undefined, undefined, true);
    let plan: PageWorkspaceMovePlan;
    try {
      plan = (await planPageWorkspaceMove(session, movingPage.id, target)).plan;
    } catch (caught) {
      toast?.dismissMessage(preparing);
      if (caught instanceof LeafenApiError && caught.status === 404) {
        await copy(destination);
        return;
      }
      const blocked = caught instanceof LeafenApiError && caught.code === organizationErrorCodes.sharingSecurityBlocked;
      toast?.notify(t(blocked ? "moveToDialog:toast.copyBlockedByOrganization" : "moveToDialog:toast.moveFailed"), "error");
      return;
    }
    toast?.dismissMessage(preparing);
    const chosen = await confirmActions({
      message: t("moveToDialog:move.chooseTitle", { workspace: to.name }), icon: createElement(WarningIcon),
      notes: [t("moveToDialog:move.duplicateNote"), t("moveToDialog:move.moveNote"), ...moveNotes(t, plan)],
      actions: [
        { value: "duplicate", label: t("moveToDialog:copy.confirmAction") },
        { value: "move", label: t("moveToDialog:move.confirmAction"), danger: true },
      ],
    });
    if (chosen === "duplicate") return copy(destination, to, false);
    if (chosen !== "move") return;
    const progress = t("moveToDialog:move.progress");
    toast?.notify(progress, "default", undefined, "progress", undefined, undefined, true);
    try {
      await movePageToWorkspace(session, movingPage.id, target);
      await productPages?.refreshPages?.();
      toast?.dismissMessage(progress);
      toast?.notify(t("moveToDialog:toast.movedTo", {
        title: plainTitleOf(movingPage) || untitledPageDisplayTitle(movingPage), place: to.name }), "default");
    } catch {
      toast?.dismissMessage(progress);
      toast?.notify(t("moveToDialog:toast.moveFailed"), "error");
    }
  }, [copy, movingPage, onStarted, productPages, row, session, t, to, toast]);
}

/** What the plan says will be cut, a sentence each, or that nothing is. */
function moveNotes(t: (key: string, options?: Record<string, unknown>) => string, plan: PageWorkspaceMovePlan): string[] {
  const { cut, across } = plan;
  const notes = [
    cut.relationProperties ? t("moveToDialog:move.cutRelations", { count: cut.relationProperties, values: cut.relationValues }) : "",
    cut.peopleLosingAccess
      ? t(cut.peopleLosingAccessAtLeast ? "moveToDialog:move.cutPeopleAtLeast" : "moveToDialog:move.cutPeople", { count: cut.peopleLosingAccess }) : "",
    cut.shares ? t("moveToDialog:move.cutShares", { count: cut.shares }) : "",
    cut.publicLinks + cut.guestLinks ? t("moveToDialog:move.cutLinks", { count: cut.publicLinks + cut.guestLinks }) : "",
    cut.favorites ? t("moveToDialog:move.cutFavorites", { count: cut.favorites }) : "",
    across.linkedViewsOut + across.linkedViewsIn
      ? t("moveToDialog:move.cutLinkedViews", { count: across.linkedViewsOut + across.linkedViewsIn }) : "",
  ].filter(Boolean);
  const between = across.mentionsOut + across.mentionsIn;
  return [
    ...(notes.length ? notes : [t("moveToDialog:move.nothingCut")]),
    ...(between ? [t("moveToDialog:move.across", { count: between })] : []),
    ...(across.sharedFiles ? [t("moveToDialog:move.sharedFiles", { count: across.sharedFiles })] : []),
  ];
}
