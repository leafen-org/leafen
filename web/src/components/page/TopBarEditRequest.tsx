import type { ProductStatusSink } from "@/lib/productStatus";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { requestPageAccess } from "@/lib/accessRequests";
import type { Page, Session } from "@/types";
import { styles } from "./topBarStyles";

/**
 * On a page the person may only view or comment on, they ask the people with
 * full access to it to let them edit, who decide in their inbox — from the
 * share window's band, where Notion has it (shareAsksEdit).
 */
export function TopBarEditRequest({ page, session, onStatus, className }: {
  page: Pick<Page, "id">;
  session: Session;
  onStatus?: ProductStatusSink;
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
