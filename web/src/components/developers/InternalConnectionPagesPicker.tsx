import { useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import { Search } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { outOfTrashIn } from "@/lib/pageIndex";
import { usePageNames } from "@/lib/pageNames";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { publicMutationService } from "@/lib/publicMutationService";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import styles from "@/components/settings/DeveloperSettings.module.css";
import own from "./InternalConnection.module.css";

const SHOWN_MOST = 50;

/**
 * "편집 권한": the workspace's pages, found by name, each one the connection
 * reaches or not — choosing one adds it, and the pages under it, as its
 * page menu's "연결" does (page_notion_connections.go). An MCP token's page
 * chooses its own way (toggle).
 */
export function InternalConnectionPagesPicker({ session, grantId, toggle: change, chosen, onChanged, onClose, title }: {
  session: Session; grantId?: string; toggle?(pageId: string, connect: boolean): Promise<void>;
  /** Its heading where it chooses pages for something else (a guest invite link, TopBarShareGuestLinks). */
  title?: string;
  chosen: ReadonlySet<string>; onChanged(): void; onClose(): void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const pages = useProductPages();
  const [query, setQuery] = useState("");
  const [on, setOn] = useState(() => new Set(chosen));
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, onEscape: onClose, initialFocus: searchRef });
  const candidates = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase();
    const listed = pages?.pages ?? [], outOfTrash = outOfTrashIn(listed);
    return listed
      .filter((page) => outOfTrash(page) && (!needle || pageNames.title(page).toLocaleLowerCase().includes(needle)))
      .slice(0, SHOWN_MOST);
  }, [pages?.pages, query, pageNames]);
  async function toggle(pageId: string) {
    if (busy) return;
    setBusy(pageId);
    setError("");
    try {
      const connect = !on.has(pageId);
      if (change) await change(pageId, connect);
      else {
        const state = await publicMutationService.appConnections(session, pageId, new AbortController().signal);
        await publicMutationService.changeAppConnection(session, pageId, { expectedRevision: state.revision, action: connect ? "connect" : "disconnect", grantId: grantId ?? "" });
      }
      setOn((current) => { const next = new Set(current); if (connect) next.add(pageId); else next.delete(pageId); return next; });
      onChanged();
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    } finally {
      setBusy("");
    }
  }
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }} data-internal-connection-pages>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <h2 id={titleId} className={styles.dialogTitle}>{title ?? t("developerTools.internal.editAccess")}</h2>
        <div className={styles.dialogBody}>
          <label className={own.search}>
            <Search size="small" aria-hidden="true" />
            <input ref={searchRef} value={query} placeholder={t("developerTools.internal.accessSearch")} aria-label={t("developerTools.internal.accessSearch")}
              onChange={(event) => setQuery(event.currentTarget.value)} />
          </label>
          <div className={own.pickList}>
            {candidates.map((page) => (
              <label key={page.id} className={own.pageRow} data-connection-pick={page.id}>
                <input type="checkbox" checked={on.has(page.id)} disabled={busy === page.id} onChange={() => void toggle(page.id)} />
                <PageIconGlyph page={page} size={18} />
                <span><PageTitleText page={page} /></span>
              </label>
            ))}
          </div>
          {error ? <p className={styles.error} role="alert">{error}</p> : null}
          <div className={styles.dialogActions}>
            <button type="button" className={styles.primary} onClick={onClose}>{t("common:actions.done")}</button>
          </div>
        </div>
      </section>
    </div>,
    document.body,
  );
}
