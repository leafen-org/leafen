import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "@/components/Switch";
import { DotsHorizontal, SiteSearchableIcon } from "@/icons/leafen";
import { publicMutationStrings } from "@/publicMutationWire.generated";
import type { Session } from "@/types";
import { SITE_UNTITLED_HEAD_TITLE } from "./topBarShareModel";
import { SiteImageThumbnail } from "./TopBarShareSitePreview";
import { TopBarSiteScreen } from "./TopBarSitePublished";
import published from "./TopBarSitePublished.module.css";
import { ShareErrorLine } from "./ShareErrorLine";
import styles from "./TopBarSiteIndexing.module.css";

/** The words a page without any heads its site with, in every language, from the contract the server's head reads too. */
const HEAD_DEFAULT_DESCRIPTION: string = publicMutationStrings.defaultHeadDescription;

/**
 * "검색 엔진 인덱싱 ›", as Notion's (seen 2026-11-11, measured 2026-11-12):
 * "웹에서 검색 가능" with its icon and switch, no words under it; then the
 * site as a search engine lists it ("SEO 미리보기": its icon, host and "⋯"
 * on one line, the link title, the description on one line), the "링크 제목"
 * and "설명" fields it is drawn from, and "변경 사항 저장", which keeps them
 * for the head of the site's pages (public_site_document.go) — the title a
 * link to it shows and the words under it. The link title starts out as the
 * head says it until one is kept, "〈title〉 | Leafen", as Notion's starts
 * out "〈title〉 | Notion"; empty for an untitled page, over the line its
 * head says, Leafen's product line, which the preview shows while it is
 * empty — as Notion's shows its own product line (seen 2026-11-12).
 * The description starts out as the head gives it too, the page's first
 * words, else Leafen's own line, which is the field's placeholder — as
 * Notion's starts out the page's words over a line of its own (seen
 * 2026-11-12); kept as it is, it stays the page's.
 * While the site isn't searchable the preview is faint and its fields and
 * "변경 사항 저장" can't be used, as Notion's.
 */
export function TopBarSiteIndexing({ session, siteLink, faviconFileId, defaultTitle, title, description, defaultDescription, allowIndexing,
  disabled, onIndexingChange, onSave, onTyped, onBack, errors, failure }: {
  session: Session;
  siteLink: string;
  /** The site's own favicon, else Leafen's. */
  faviconFileId: string;
  /** The head's title while the site has no link title of its own (siteHeadTitle), empty for an untitled page; kept as it is, it stays the page's. */
  defaultTitle: string;
  /** The link title and description as the site keeps them. */
  title: string;
  description: string;
  /** The head's description while the site keeps none of its own (publicPageDescription); kept as it is, it stays the page's. */
  defaultDescription: string;
  allowIndexing: boolean;
  disabled: boolean;
  onIndexingChange(value: boolean): void;
  /** Keeps a new link title and description, saying whether they took. */
  onSave(title: string, description: string): Promise<boolean>;
  /** A field typed in again, which puts away why it was refused. */
  onTyped(field: "title" | "description"): void;
  onBack(): void;
  /** Why the link title or description can't be the site's (over its length), said under each (ShareErrorLine). */
  errors: { title?: string; description?: string };
  /** Why its last change didn't take, said at its end (ShareErrorLine). */
  failure: string;
}) {
  const { t } = useTranslation("siteBuilder");
  const keptTitle = title || defaultTitle, keptDescription = description || defaultDescription;
  const [draftTitle, setDraftTitle] = useState(keptTitle);
  const [draftDescription, setDraftDescription] = useState(keptDescription);
  // What the site keeps, once saved here or read again, is what the fields
  // start from — but a field still holding words not kept goes on holding
  // them: a save refused and read back (the site changed elsewhere) leaves
  // them to fix and save again, as the address row does (shareChangeTook).
  const started = useRef({ title: keptTitle, description: keptDescription });
  useEffect(() => {
    const before = started.current;
    started.current = { title: keptTitle, description: keptDescription };
    setDraftTitle((draft) => draft === before.title ? keptTitle : draft);
    setDraftDescription((draft) => draft === before.description ? keptDescription : draft);
  }, [keptTitle, keptDescription]);
  const changed = draftTitle.trim() !== (title.trim() || defaultTitle)
    || draftDescription.trim() !== (description.trim() || defaultDescription);
  const host = (() => { try { return new URL(siteLink).host; } catch { return siteLink; } })();
  const fieldsOff = disabled || !allowIndexing;
  return (
    <TopBarSiteScreen title={t("searchIndexing")} panel="publish-indexing" onBack={onBack}>
      <div className={published.setting}>
        <SiteSearchableIcon size="regular" className={published.settingIcon} aria-hidden="true" />
        <span className={published.settingText}>{t("allowIndexing")}</span>
        <Switch on={allowIndexing} aria-label={t("allowIndexing")} className={published.switch}
          disabled={disabled} onClick={() => onIndexingChange(!allowIndexing)} data-site-indexing={allowIndexing ? "on" : "off"} />
      </div>
      <div className={styles.seo}>
        <div className={styles.group}>
          <span className={styles.label}>{t("seoPreview")}</span>
          <div className={styles.preview} data-site-seo-preview data-off={allowIndexing ? undefined : "true"}>
            <span className={styles.site}>
              <span className={styles.favicon}>
                {faviconFileId ? <SiteImageThumbnail session={session} fileId={faviconFileId} />
                  : <img src="/favicon-32.png" alt="" />}
              </span>
              <span className={styles.host}>{host}</span>
              <DotsHorizontal size="micro" className={styles.more} aria-hidden="true" />
            </span>
            <strong className={styles.title}>{draftTitle.trim() || defaultTitle || SITE_UNTITLED_HEAD_TITLE}</strong>
            {draftDescription.trim() ? <span className={styles.description}>{draftDescription.trim()}</span> : null}
          </div>
        </div>
        <label className={styles.field}>{t("linkTitle")}
          <input value={draftTitle} placeholder={SITE_UNTITLED_HEAD_TITLE} disabled={fieldsOff} data-site-link-title
            aria-invalid={errors.title ? true : undefined} onChange={(event) => { setDraftTitle(event.target.value); onTyped("title"); }} />
        </label>
        <ShareErrorLine text={errors.title ?? ""} className={styles.invalid} data-site-link-title-invalid />
        <label className={styles.field}>{t("siteDescription")}
          <textarea value={draftDescription} placeholder={HEAD_DEFAULT_DESCRIPTION} disabled={fieldsOff} rows={3} data-site-link-description
            aria-invalid={errors.description ? true : undefined}
            onChange={(event) => { setDraftDescription(event.target.value); onTyped("description"); }} />
        </label>
        <ShareErrorLine text={errors.description ?? ""} className={styles.invalid} data-site-link-description-invalid />
      </div>
      <div className={published.footer}>
        <button type="button" className={styles.save} disabled={fieldsOff || !changed} data-site-seo-save
          onClick={() => void onSave(draftTitle.trim() === defaultTitle ? "" : draftTitle.trim(),
            draftDescription.trim() === defaultDescription ? "" : draftDescription.trim())}>{t("saveChanges")}</button>
      </div>
      <ShareErrorLine text={failure} data-site-failure />
    </TopBarSiteScreen>
  );
}
