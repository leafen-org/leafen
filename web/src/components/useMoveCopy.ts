import { createElement, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { WarningIcon } from "@/icons/leafen/automationIcons";
import { pageHref } from "@/lib/navigation";
import { duplicateNativePage } from "@/lib/pageDuplicationService";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { organizationErrorCodes } from "@/organizationWire.generated";
import type { Page, Session, Workspace } from "@/types";
import type { MoveDestination } from "./pageMoveDialogModel";
import { useProductPages } from "./product/productPagesContext";
import { confirmAction } from "./ConfirmActionDialog";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";

/**
 * A page "옮기기" copies to another workspace where it can't move there
 * (useMoveToWorkspace: a database's page, or one the reader lacks full
 * access to), as Notion's help says of every page (read 2026-10-01): once
 * the reader confirms, the copy goes where they chose and the page stays
 * where it is. A database's page goes as a
 * page, its body without its values, as Notion's does (seen 2026-10-31).
 * As Notion's, it warns in a card with a mark, a title and two paragraphs;
 * once confirmed the popover closes (`onStarted`), "페이지 복제 중" shows
 * with "무시하기" while the copy is made, and the copy opens. A guest's
 * "페이지 복제 ›" names the workspace itself and asks nothing: no
 * destination puts the copy at the top of its private pages.
 */
export function useMoveCopy(session: Session, movingPage: Page | undefined, workspace: Workspace | undefined, onStarted: () => void) {
  const { t } = useTranslation("moveToDialog");
  const productPages = useProductPages();
  const toast = useOptionalProductToast();
  return useCallback(async (destination: MoveDestination | null, to: Workspace | undefined = workspace, ask = true) => {
    if (!movingPage || !to) return;
    if (ask && !await confirmAction({ message: t("moveToDialog:copy.title"), icon: createElement(WarningIcon),
      notes: [t("moveToDialog:copy.stays"), t("moveToDialog:copy.mayDiffer")],
      confirmLabel: t("moveToDialog:copy.confirmAction"), danger: "filled-white" })) return;
    onStarted();
    const progress = t("moveToDialog:copy.progress");
    toast?.notify(progress, "default", { label: t("moveToDialog:copy.dismiss"), onClick: () => {}, autoDismissMs: null, dismissible: false },
      "progress");
    try {
      const copied = await duplicateNativePage(session, movingPage, new AbortController().signal, {
        parentId: destination?.type === "page" ? destination.id : "",
        workspaceId: to.id,
        teamspaceId: destination?.type === "place" ? destination.place.teamspaceId : "",
      });
      await productPages?.refreshPages?.();
      toast?.dismissMessage(progress);
      toast?.notify(t("moveToDialog:toast.copied", {
        workspace: to.name, title: plainTitleOf(movingPage) || untitledPageDisplayTitle(movingPage) }), "default");
      pushRoute(pageHref(copied.targetRootPageId));
    } catch (caught) {
      toast?.dismissMessage(progress);
      // The organization's sharing security may keep its pages in their workspace.
      const blocked = caught instanceof LeafenApiError && caught.code === organizationErrorCodes.sharingSecurityBlocked;
      toast?.notify(t(blocked ? "moveToDialog:toast.copyBlockedByOrganization" : "moveToDialog:toast.copyFailed"), "error");
    }
  }, [movingPage, onStarted, productPages, session, t, toast, workspace]);
}
