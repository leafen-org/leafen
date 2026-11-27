import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArchiveIcon } from "@/icons/leafen";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { archiveBandText, pageArchiveRoot, pageLookup } from "@/lib/pageArchive";
import { actorLabel } from "@/lib/peopleDirectory";
import { useProductToast } from "@/lib/productToast";
import { menuTimestampLabel } from "@/lib/relativeTime";
import type { Page, Session } from "@/types";
import styles from "./PageArchiveBanner.module.css";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";

/**
 * The yellow banner on an archived page: who archived it and when, and the
 * way back. A subpage names the archived page it is archived with, and
 * unarchiving acts on that page.
 */
export function PageArchiveBanner({
  page,
  pages,
  session,
  onPageMetadataChange,
}: {
  page: Page;
  pages: Page[];
  session: Session;
  onPageMetadataChange?: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
}) {
  const { t } = useTranslation(["pageArchive", "common"]);
  const { notify } = useProductToast();
  const [busy, setBusy] = useState(false);
  const operationRef = useRef<AbortController | null>(null);
  const root = pageArchiveRoot(page, pageLookup(pages));

  useEffect(() => () => operationRef.current?.abort(), []);
  useEffect(() => {
    operationRef.current?.abort();
    operationRef.current = null;
    setBusy(false);
  }, [page.id]);

  if (!root) return null;
  const archiveRoot = root;
  const actor = actorLabel(archiveRoot.archivedBy, session.user.id);
  const date = menuTimestampLabel(archiveRoot.archivedAt);
  const inherited = archiveRoot.id !== page.id;
  const rootTitle = archiveRoot.title || untitledPageDisplayTitle(archiveRoot);

  async function unarchive() {
    if (!onPageMetadataChange || busy) return;
    const controller = new AbortController();
    operationRef.current = controller;
    setBusy(true);
    try {
      const result = await onPageMetadataChange(archiveRoot, { isArchived: false }, controller.signal);
      if (controller.signal.aborted) return;
      notify(t(result === false ? "pageArchive:couldntUnarchive" : "pageArchive:unarchived"),
        result === false ? "error" : "success");
    } catch (error) {
      if (!controller.signal.aborted) notify(t("pageArchive:couldntUnarchive"), "error");
      else throw error;
    } finally {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setBusy(false);
      }
    }
  }

  return (
    <div className={styles.banner} role="status" data-page-archive-banner={inherited ? "inherited" : "self"}>
      <ArchiveIcon size="regular" aria-hidden="true" />
      <span className={styles.message}>
        {inherited
          ? t("pageArchive:archivedWithParent", { title: rootTitle, actor, date })
          : archiveBandText(archiveRoot.archivedBy, archiveRoot.archivedAt, session.user.id)}
      </span>
      {onPageMetadataChange ? (
        <button
          type="button"
          className={styles.action}
          data-page-unarchive
          disabled={busy}
          aria-busy={busy || undefined}
          onClick={() => void unarchive().catch(() => undefined)}
        >
          {t("pageArchive:unarchive")}
        </button>
      ) : null}
    </div>
  );
}
