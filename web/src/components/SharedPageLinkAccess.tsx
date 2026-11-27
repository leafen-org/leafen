import { useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { joinShareLink, rememberShareLinkJoin } from "@/lib/shareLinkJoin";
import type { Session } from "@/types";
import styles from "./SharedPageLinkAccess.module.css";

/**
 * Over a page read through a web link that lets the organization's people
 * comment or edit: the way into the app to do so, for someone signed in, or
 * to sign in first.
 */
export function SharedPageLinkAccess({ token, pageId, role, session }: {
  token: string; pageId: string; role: "comment" | "edit"; session: Session | null;
}) {
  const { t } = useTranslation("sharedPageView");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function open() {
    if (!session) {
      rememberShareLinkJoin({ token, pageId });
      pushRoute("/");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const joined = await joinShareLink(session, { token, pageId });
      pushRoute(pageHref(joined.pageId));
    } catch (failure) {
      setError(failure instanceof LeafenApiError && failure.status === 403 ? t("linkAccess.notInOrganization") : t("linkAccess.failed"));
      setBusy(false);
    }
  }

  return (
    <div className={styles.strip} data-shared-link-access={role}>
      <span>{t(role === "edit" ? "linkAccess.canEdit" : "linkAccess.canComment")}</span>
      <button type="button" className={styles.open} disabled={busy} onClick={() => void open()}>
        {/* Signed out, Notion's words without its sign-up: "댓글을 작성하려면 로그인하세요." (seen 2026-11-15). */}
        {t(session ? (role === "edit" ? "linkAccess.edit" : "linkAccess.comment") : role === "edit" ? "linkAccess.signInToEdit" : "linkAccess.signInToComment")}
      </button>
      {error ? <span role="alert" className={styles.error}>{error}</span> : null}
    </div>
  );
}
