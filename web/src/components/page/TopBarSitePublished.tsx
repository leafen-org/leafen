import { Switch } from "@/components/Switch";
import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronLeft, ChevronRight, CodeIcon, Copy, CopySmallIcon, LinkIcon, PaintBrushIcon, Search, ServiceIcon, Settings, ShareSiteIcon, TitlePropertyIcon } from "@/icons/leafen";
import { useCopyNotice } from "@/lib/copyNotice";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SOCIAL_SHARE_SERVICES, siteEmbedCode, socialShareHref } from "@/lib/socialShare";
import styles from "./TopBarSitePublished.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { ShareErrorLine } from "./ShareErrorLine";

/** Where a published site's address breaks: its server, the path before
 * the page's own, and the page's path ("site/" and the site's address). */
function siteAddressParts(link: string) {
  try {
    const url = new URL(link);
    const path = url.pathname.replace(/^\//, "");
    const at = path.lastIndexOf("/") + 1;
    return { host: url.host, prefix: path.slice(0, at), page: decodeURIComponent(path.slice(at)) };
  } catch {
    return { host: link, prefix: "", page: "" };
  }
}

/**
 * The page's path in the address row, as Notion's (seen 2026-11-07): the
 * path is a button; pressing it turns it into a field with the whole path
 * chosen and "저장" where the copy button was, Enter saving too. The site's
 * first address still opens it afterwards (public_visibility.go). A path
 * that doesn't take — refused (another site has it, the site can't have
 * it) or never answered — keeps the field open with what was typed and why
 * under the row (ShareErrorLine), to fix and save again (shareChangeTook).
 *
 * Where the keys go then, as Notion's (seen 2026-11-16 in Chrome at 1440,
 * the window opened from "공유" and from the published page's "사이트 설정",
 * no script in the page before the key, a letter typed after it): saved,
 * the window stays and the focus is on the page, no ring and the next
 * letter landing nowhere — the field is marked data-focus-leaves so the
 * window's focus keeper hands it nothing; Esc is the window's own, closing
 * it with the path unsaved and the ringed focus back on its opener
 * (useOverlayLayer).
 */
function SiteAddressPath({ prefix, page, disabled, addressError, onSave, onEditing }: {
  prefix: string;
  page: string;
  disabled: boolean;
  addressError: string;
  onSave(value: string): Promise<boolean>;
  onEditing(editing: boolean): void;
}) {
  const { t } = useTranslation("siteBuilder");
  const [draft, setDraft] = useState<string | null>(null);
  const saving = useRef(false);
  const editing = draft !== null;
  useEffect(() => { onEditing(editing); }, [editing, onEditing]);
  const save = async () => {
    if (draft === null || saving.current) return;
    const value = draft.trim();
    if (!value || value === page) { setDraft(null); return; }
    saving.current = true;
    const saved = await onSave(value);
    saving.current = false;
    if (saved) setDraft(null);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (fieldEnter(event)) { event.preventDefault(); void save(); }
  };
  if (draft === null) {
    return <button type="button" className={styles.path} disabled={disabled} aria-label={t("editAddress")}
      onClick={() => setDraft(page)} data-site-address-path>{prefix}{page}</button>;
  }
  return <>
    <span className={styles.pathPrefix}>{prefix}</span>
    <input className={styles.pathField} value={draft} autoFocus aria-label={t("editAddress")} spellCheck={false}
      aria-invalid={addressError ? true : undefined} onFocus={(event) => event.currentTarget.select()}
      onChange={(event) => setDraft(event.target.value)} onKeyDown={onKeyDown}
      onBlur={(event) => { if (!event.currentTarget.parentElement?.contains(event.relatedTarget)) setDraft(null); }}
      data-site-address-field data-focus-leaves />
    <button type="button" className={styles.saveAddress} disabled={disabled}
      onMouseDown={(event) => event.preventDefault()} onClick={() => void save()} data-site-address-save>{t("saveAddress")}</button>
  </>;
}

/**
 * The "게시" tab once its page is on the web, as Notion's (seen 2026-11-05):
 * the site's address (its server, "/", the page's path) with a copy button;
 * "사이트 스타일 사용자 지정" under a brush; "검색 엔진 인덱싱", off at first, ›;
 * "템플릿으로 복제"; "모든 사이트 및 링크 관리"; a line across; "이 페이지 임베드 ›"
 * and "소셜 미디어로 공유 ›"; then "게시 취소", which asks nothing, and
 * "사이트 보기", side by side in halves.
 */
export function TopBarSitePublished({ siteLink, copied, busy, disabled, allowIndexing, allowDuplicate, addressError, onAddressChange, onCopy, onStyle, onIndexing, onDuplicateChange, onEmbed, onSocial, onManage, onUnpublish }: {
  siteLink: string;
  /** Why the path typed in the address row didn't take: another site has it, or any other refusal. */
  addressError: string;
  /** Publishes the site at a new page path, saying whether it took (shareChangeTook): refused or unanswered, the field stays. */
  onAddressChange(slug: string): Promise<boolean>;
  copied: boolean;
  busy: boolean;
  disabled: boolean;
  allowIndexing: boolean;
  /** "템플릿으로 복제": visitors may duplicate the site's pages, on at first as Notion's. */
  allowDuplicate: boolean;
  onDuplicateChange(value: boolean): void;
  onCopy(): void;
  onStyle(): void;
  onIndexing(): void;
  onEmbed(): void;
  onSocial(): void;
  onManage(): void;
  onUnpublish(): void;
}) {
  const { t } = useTranslation("siteBuilder");
  const address = siteAddressParts(siteLink);
  // The row is framed while its path is being typed, as Notion's.
  const [editing, setEditing] = useState(false);
  // A touch screen's tab, as Notion's phone and iPad (seen 2026-11-13 at
  // 402pt and 834pt): search indexing, duplicating as a template and
  // embedding alone, as the sheet's own rows (TopBarSitePublished.module.css).
  const { phone: touch } = useSidebarLayout(true);
  return (
    <div className={styles.published} data-share-panel="publish" data-site-published="true">
      <div className={styles.address} data-site-address data-editing={editing ? "true" : undefined}>
        <span className={styles.addressText} title={siteLink}>
          <span>{address.host}</span><span className={styles.slash}>/</span>
          <SiteAddressPath prefix={address.prefix} page={address.page} disabled={disabled || busy} addressError={addressError}
            onSave={onAddressChange} onEditing={setEditing} />
        </span>
        {editing ? null : <button type="button" className={styles.copy} aria-label={t("copySiteLink")} title={t(copied ? "copied" : "copySiteLink")}
          disabled={!siteLink} onClick={onCopy} data-site-copy-link>
          {copied ? <CheckIcon size="small" aria-hidden="true" /> : <LinkIcon size="small" aria-hidden="true" />}
        </button>}
      </div>
      <ShareErrorLine text={addressError} className={styles.addressError} data-site-address-taken />
      <div className={styles.rows} data-touch={touch ? "true" : undefined}>
        {touch ? null : <button type="button" className={styles.row} onClick={onStyle} data-site-row="style">
          <PaintBrushIcon size="regular" aria-hidden="true" /><span>{t("customizeStyle")}</span>
        </button>}
        <button type="button" className={styles.row} onClick={onIndexing} data-site-row="indexing">
          <Search size="regular" aria-hidden="true" /><span>{t("searchIndexing")}</span>
          <span className={styles.value}>{t(allowIndexing ? "on" : "off")}</span>
          <ChevronRight size="small" aria-hidden="true" />
        </button>
        <div className={styles.row} data-site-row="duplicate">
          <Copy size="regular" aria-hidden="true" /><span>{t("duplicateAsTemplate")}</span>
          <Switch on={allowDuplicate} aria-label={t("duplicateAsTemplate")} className={styles.switch}
            disabled={disabled} onClick={() => onDuplicateChange(!allowDuplicate)} data-site-duplicate={allowDuplicate ? "on" : "off"} />
        </div>
        {touch ? null : <>
          <button type="button" className={styles.row} onClick={onManage} data-site-row="manage">
            <Settings size="regular" aria-hidden="true" /><span>{t("manageSites")}</span>
          </button>
          <div className={styles.divider} />
        </>}
        <button type="button" className={styles.row} onClick={onEmbed} data-site-row="embed">
          <CodeIcon size="regular" aria-hidden="true" /><span>{t("embedPage")}</span>
          <ChevronRight size="small" aria-hidden="true" />
        </button>
        {touch ? null : <button type="button" className={styles.row} onClick={onSocial} data-site-row="social">
          <ShareSiteIcon size="regular" aria-hidden="true" /><span>{t("shareSocial")}</span>
          <ChevronRight size="small" aria-hidden="true" />
        </button>}
      </div>
      <div className={styles.footer}>
        <button type="button" className={styles.unpublish} disabled={disabled} onClick={onUnpublish} data-menu-item data-site-unpublish>
          {t("unpublish")}
        </button>
        <a className={styles.view} href={siteLink || undefined} target="_blank" rel="noreferrer" aria-disabled={!siteLink || busy}
          data-site-view>{t("viewSite")}</a>
      </div>
    </div>
  );
}

/** The head of a screen the publish tab's rows open: back to the list, and its name. */
export function TopBarSiteScreenHeader({ title, onBack }: { title: string; onBack(): void }) {
  const { t } = useTranslation("siteBuilder");
  return (
    <div className={styles.header}>
      <button type="button" aria-label={t("back")} onClick={onBack}><ChevronLeft size="regular" aria-hidden="true" /></button>
      <span>{title}</span>
    </div>
  );
}

/**
 * A screen the publish tab's rows open, as Notion's "검색 엔진 인덱싱", "이
 * 페이지 임베드" and "소셜 미디어로 공유" (measured 2026-11-12): the share
 * window's whole width with no "공유 | 게시" strip over it, its head
 * (TopBarSiteScreenHeader) and what it holds under it.
 */
export function TopBarSiteScreen({ title, panel, onBack, children }: { title: string; panel: string; onBack(): void; children: ReactNode }) {
  return (
    <div className={styles.screen} data-site-screen data-share-panel={panel}>
      <TopBarSiteScreenHeader title={title} onBack={onBack} />
      <div className={styles.screenBody}>{children}</div>
    </div>
  );
}

/** "이 페이지 임베드 ›", as Notion's (seen 2026-11-11, measured 2026-11-18):
 * "Aa 페이지 제목 표시" with its switch, the code that frames the page in
 * another site in its box, and "코드 복사" under it at the end, 14 apart. */
export function TopBarSiteEmbed({ siteLink, onBack }: { siteLink: string; onBack(): void }) {
  const { t } = useTranslation("siteBuilder");
  const [showTitle, setShowTitle] = useState(true);
  // The button keeps its words while a toast says the code was copied.
  const notice = useCopyNotice();
  const code = siteEmbedCode(siteLink, showTitle);
  return (
    <TopBarSiteScreen title={t("embedPage")} panel="publish-embed" onBack={onBack}>
      <div className={styles.embed}>
        <div className={`${styles.setting} ${styles.screenRow}`}>
          <TitlePropertyIcon size="regular" className={styles.settingIcon} aria-hidden="true" />
          <span className={styles.settingText}>{t("embedShowTitle")}</span>
          <Switch on={showTitle} aria-label={t("embedShowTitle")} className={styles.switch}
            onClick={() => setShowTitle((current) => !current)} data-site-embed-title={showTitle ? "on" : "off"} />
        </div>
        <div className={styles.codeBox}>
          <textarea className={styles.code} readOnly value={code} aria-label={t("embedCode")} data-site-embed-code
            onFocus={(event) => event.currentTarget.select()} />
        </div>
        <div className={styles.footer}>
          <button type="button" className={styles.copyCode} disabled={!siteLink}
            onClick={() => void notice(code)} data-site-embed-copy>
            <CopySmallIcon aria-hidden="true" />{t("copyEmbedCode")}
          </button>
        </div>
      </div>
    </TopBarSiteScreen>
  );
}

/** "소셜 미디어로 공유 ›": the site's link handed to each service, a full-width
 * outlined button each with its mark, as Notion's (seen 2026-11-11). */
export function TopBarSiteSocial({ siteLink, title, onBack }: { siteLink: string; title: string; onBack(): void }) {
  const { t } = useTranslation(["siteBuilder", "publicSiteView"]);
  const labels = {
    x: "publicSiteView:shareOnX", whatsapp: "publicSiteView:shareOnWhatsApp", linkedin: "publicSiteView:shareOnLinkedIn",
    facebook: "publicSiteView:shareOnFacebook", email: "siteBuilder:shareByEmail",
  } as const;
  return (
    <TopBarSiteScreen title={t("shareSocial")} panel="publish-social" onBack={onBack}>
      <div className={styles.services}>
        {SOCIAL_SHARE_SERVICES.map((service) => (
          <a key={service.id} className={styles.service} href={siteLink ? socialShareHref(service.id, siteLink, title) : undefined}
            target={service.id === "email" ? undefined : "_blank"} rel="noopener noreferrer" data-site-social={service.id}>
            <ServiceIcon service={service.id} name={service.name} size="regular" aria-hidden="true" />
            <span>{t(labels[service.id])}</span>
          </a>
        ))}
      </div>
    </TopBarSiteScreen>
  );
}
