import type { RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ConfirmCard } from "@/components/ConfirmActionDialog";
import type { PendingSignOutPrompt } from "@/lib/productSignOut";
import { giveFocusBack } from "@/lib/focusGiveBack";

// The preserved sidebar's discard confirmation: sign-out waits here while
// this device still holds unsynced changes, or cannot tell whether it does.
// The app's one confirmation card; cancelling goes back to the workspace
// button.
export function SidebarPendingSignOut({
  pending,
  noEmail,
  returnFocusRef,
}: {
  pending: PendingSignOutPrompt;
  /** No way back from a browser alone: the one question says how to come back. */
  noEmail: boolean;
  returnFocusRef: RefObject<HTMLElement | null>;
}) {
  const { t } = useTranslation(["sidebar", "common"]);
  const readFailed = pending.count === null;
  // Asked with nothing waiting: only the way back is the question.
  const nothingWaiting = pending.count === 0;
  const wayBack = noEmail ? ` ${t("sidebar:logOutNoEmailBody")}` : "";
  const dialog = (
    <ConfirmCard danger={!nothingWaiting}
      message={nothingWaiting ? t("sidebar:logOutNoEmailTitle")
        : readFailed ? t("sidebar:pendingSignOutReadFailedTitle") : t("sidebar:pendingSignOutTitle")}
      detail={nothingWaiting ? t("sidebar:logOutNoEmailBody")
        : pending.count === null
          ? `${t("sidebar:pendingSignOutReadFailedDescription")}${wayBack}`
          : `${t("sidebar:pendingSignOutCount", { count: pending.count })} ${t("sidebar:pendingSignOutDescription")}${wayBack}`}
      confirmLabel={nothingWaiting ? t("sidebar:logOut") : t("sidebar:pendingSignOutConfirm")} cancelLabel={t("common:actions.cancel")}
      markers={{ dialog: { "data-sidebar-pending-sign-out": "" } }}
      onAnswer={(answer) => {
        if (answer !== null) {
          pending.onDiscard();
          return;
        }
        pending.onCancel();
        giveFocusBack({ to: returnFocusRef.current });
      }} />
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
