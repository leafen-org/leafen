import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { resetDocumentChrome } from "@/lib/documentChrome";
import styles from "./PublicNotFound.module.css";

/**
 * A public address that leads nowhere — a site taken down, a share link
 * turned off or mistyped — as Notion's (seen 2026-10-08): the product's name
 * in the corner and a short "페이지 찾지 못함" in the middle, with nothing to
 * retry or report, since the visitor usually has no account.
 */
export function PublicNotFound({ branded = true }: {
  /** False on a published site with Leafen's branding off, which names no Leafen (2026-10-16). */
  branded?: boolean;
}) {
  const { t } = useTranslation("sharedPageView");
  // A tab whose page was taken away while open (its link removed, its site
  // unpublished) names the page no more: as the address opened afresh.
  useEffect(() => { resetDocumentChrome(); }, []);
  return (
    <main className={styles.page} data-public-not-found>
      {branded ? <a className={styles.brand} href="/">Leafen</a> : null}
      <div className={styles.message} role="alert">
        <h1>{t("pageNotFound")}</h1>
        <p>{t("pageNotFoundBody")}</p>
      </div>
    </main>
  );
}
