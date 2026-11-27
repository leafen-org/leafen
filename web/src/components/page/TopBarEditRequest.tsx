import type { ProductStatusSink } from "@/lib/productStatus";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { requestPageAccess } from "@/lib/accessRequests";
import type { Page, Session } from "@/types";
import { styles } from "./topBarStyles";
import { NativeActionMenuItem } from "@/lib/nativeActionMenu";

/**
 * On a page the person may only view or comment on, they ask the people with
 * full access to it to let them edit, who decide in their inbox.
 */
export function TopBarEditRequest({ page, session, onStatus, menu = false, className }: {
  page: Pick<Page, "id">;
  session: Session;
  onStatus?: ProductStatusSink;
  menu?: boolean;
  className?: string;
}) {
  const { t } = useTranslation("topBar");
  const [state, setState] = useState<"idle" | "busy" | "requested">("idle");
  function request() {
    setState("busy");
    void requestPageAccess(session, page.id, "edit").then((outcome) => {
      setState(outcome === "requested" ? "requested" : "idle");
      onStatus?.(t(`editRequest_${outcome}`), outcome === "requested" ? "default" : "error");
    });
  }
  const label = state === "requested" ? t("editRequested") : t("requestEdit");
  if (menu) return <NativeActionMenuItem id="phone-request-edit" textValue={label} disabled={state !== "idle"}
    className={styles.phoneRequestItem} data-topbar-edit-request onClick={request}>{label}</NativeActionMenuItem>;
  return (
    <button
      type="button"
      className={className ?? styles.action}
      data-topbar-edit-request
      disabled={state !== "idle"}
      onClick={request}
    >
      {label}
    </button>
  );
}
