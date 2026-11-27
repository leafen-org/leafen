import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { searchCoverImages, selectCoverImage } from "@/coverImageApi";
import type { CoverImagePhoto } from "@/coverImageWire.generated";
import { coverImageErrorCodes } from "@/coverImageWire.generated";
import { loadSession } from "@/lib/authSessionStore";
import { UNSPLASH_REFERRAL_URL } from "@/lib/coverCredit";
import { ServiceIcon } from "@/icons/leafen";
import styles from "./PageCover.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * The mark before an "Unsplash" tab, the cover picker's and an image's window's: Notion draws Unsplash's logo there,
 * 12px in the tab's color and 4px before its name (measured 2026-11-15 at 1440 dark, both windows, and on a Galaxy A90
 * at 411); another company's mark is drawn as a one-color frame with its initial.
 */
export function UnsplashTabMark() {
  return <ServiceIcon service="unsplash" name="Unsplash" size="micro" />;
}

/**
 * Unsplash's search and photos, each with its "저자:" line linking its photographer, and one faint "Unsplash" link
 * under them all, as Unsplash's API terms ask of a screen that shows its photos (the photographer and Unsplash
 * named, each linked with Leafen's referral). Notion's pane ends with its photos (seen 2026-11-15 on desktop and
 * on a Galaxy A90); the Unsplash line is Leafen's own. On a phone's sheet (an image's, `sheet`) the search is the
 * sheet's own row and field (`sheet.row`, `sheet.field`), and the photos stand three to a row
 * (PageCover.module.css).
 */
export function PageCoverUnsplash({ currentCover, busy, onApplyCover, sheet }: {
  currentCover: string; busy: boolean; onApplyCover(cover: string): Promise<boolean>; sheet?: { row: string; field: string };
}) {
  const { t } = useTranslation("pageCover");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [retry, setRetry] = useState(0);
  const [photos, setPhotos] = useState<CoverImagePhoto[]>([]);
  const [nextPage, setNextPage] = useState(0);
  const [loading, setLoading] = useState(true);
  const [selecting, setSelecting] = useState(false);
  const [error, setError] = useState("");
  const selection = useRef<AbortController | null>(null);

  function errorLabel(cause: unknown) {
    return t(cause && typeof cause === "object" && "code" in cause && cause.code === coverImageErrorCodes.rateLimited
      ? "unsplash.rateLimited" : "unsplash.error");
  }

  useEffect(() => {
    const controller = new AbortController();
    if (page === 1) setPhotos([]);
    setLoading(true);
    setError("");
    const timer = window.setTimeout(async () => {
      const session = loadSession();
      if (!session) { setError(t("unsplash.error")); setLoading(false); return; }
      try {
        const result = await searchCoverImages(session, query.trim(), page, controller.signal);
        if (controller.signal.aborted) return;
        // Keep one provider page in memory; both directions reach every
        // result without accumulating a growing gallery in the document.
        setPhotos(result.photos);
        setNextPage(result.nextPage);
      } catch (cause) {
        if (!controller.signal.aborted) setError(errorLabel(cause));
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }, query ? 350 : 0);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [query, page, retry, t]);
  useEffect(() => () => selection.current?.abort(), []);

  async function select(photo: CoverImagePhoto) {
    if (busy || selecting) return;
    const session = loadSession();
    if (!session) return;
    const controller = new AbortController();
    selection.current?.abort();
    selection.current = controller;
    setSelecting(true);
    setError("");
    try {
      const result = await selectCoverImage(session, photo.id, controller.signal);
      if (!controller.signal.aborted) await onApplyCover(result.cover);
    } catch (cause) {
      if (!controller.signal.aborted) setError(errorLabel(cause));
    } finally {
      if (!controller.signal.aborted) setSelecting(false);
    }
  }

  const search = <input className={sheet?.field} aria-label={t("unsplash.search")} placeholder={t("unsplash.search")} maxLength={200}
    value={query} disabled={busy || selecting}
    onChange={event => { setQuery(event.target.value); setPage(1); }}
    onKeyDown={event => { if (fieldEnter(event)) setRetry(value => value + 1); }} />;
  return <div className={styles.unsplashPane} data-sheet={sheet ? "true" : undefined} aria-busy={loading || selecting}>
    {sheet ? <label className={sheet.row}>{search}</label> : search}
    <div className={styles.unsplashGrid}>
      {photos.map(photo => <div key={photo.id} className={styles.unsplashPhoto}>
        <button type="button" data-cover-menu-item disabled={busy || selecting || loading}
          aria-pressed={currentCover.split("#", 1)[0] === photo.imageUrl}
          aria-label={t("unsplash.select", { author: photo.author, description: photo.description })}
          onClick={() => void select(photo)}>
          <img src={photo.thumbnailUrl} alt="" loading="lazy" />
        </button>
        <span>{t("credit.photoBy")} <a href={photo.profileUrl} target="_blank" rel="noopener noreferrer">{photo.author}</a></span>
      </div>)}
    </div>
    {loading && <p role="status">{t("unsplash.loading")}</p>}
    {selecting && <p role="status">{t("unsplash.applying")}</p>}
    {!loading && !error && photos.length === 0 && <p role="status">{t("unsplash.empty")}</p>}
    {error && <div className={styles.linkError} role="alert">{error} <button type="button" onClick={() => setRetry(value => value + 1)}>{t("unsplash.retry")}</button></div>}
    {page > 1 && <button type="button" disabled={busy || selecting || loading} onClick={() => setPage(value => value - 1)}>{t("unsplash.previous")}</button>}
    {!loading && !error && nextPage > 0 && <button type="button" disabled={busy || selecting} onClick={() => setPage(nextPage)}>{t("unsplash.more")}</button>}
    <p className={styles.providerCredit}><a href={UNSPLASH_REFERRAL_URL} target="_blank" rel="noopener noreferrer">Unsplash</a></p>
  </div>;
}
