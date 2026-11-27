import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, CheckIcon, ChevronDown, ChevronRight, Copy, DragHandleIcon, Plus, Search, ShareSiteIcon, X } from "@/icons/leafen";
import { LaptopIcon, MoonIcon, SunIcon } from "@/icons/leafen/deviceIcons";
import { useDragReorder } from "@/lib/useDragReorder";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { sitePreviewSrc, useSitePreviewFrame, type SitePreviewDraft } from "@/lib/sitePreviewFrame";
import { SwitchInput } from "@/components/Switch";
import { IconTipButton } from "@/components/IconTipButton";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { PageIconGlyph } from "./PageIcon";
import type { Session } from "@/types";
import {
  SITE_IMAGE_TYPES, SiteImageThumbnail, useSiteImageUpload, validGoogleAnalyticsId, type SitePreviewSettings,
} from "./TopBarShareSitePreview";
import { pageTitle, type SharePage } from "./topBarShareModel";
import { TitleWithCustomEmoji } from "./TitleWithCustomEmoji";
import { ShareErrorLine, type ShareFieldErrors } from "./ShareErrorLine";
import frame from "./SiteStyleEditor.module.css";
import controls from "./SiteStylePanelControls.module.css";
import screens from "./SiteStylePanelScreens.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";

// The editor's styles by what they draw: its frame (the bar, the site and
// the settings panel), the settings' own controls, and the "제목" screen.
const css = { ...frame, ...controls, ...screens };

type SiteTheme = "system" | "light" | "dark";
type Screen = "settings" | "header" | "analytics" | "share";

/** Notion's header takes five page shortcuts at most, and says so past them (seen 2026-11-06). */
const MAX_SHORTCUTS = 5;

/**
 * "사이트 스타일 사용자 지정", as Notion's site editor (seen 2026-11-05 and
 * 2026-11-06): a window over everything — "× 취소", the site's title and
 * "변경 사항 게시" across the top, the published site itself on the left
 * drawn with the settings as they are being changed (its header is a part to
 * pick), and the settings on the right: the share preview card (an image
 * uploads from it), the theme as three tiles, the favicon's choices, the
 * header ("제목": its switches and page shortcuts) and Google Analytics.
 * "취소" leaves what was published; "변경 사항 게시" publishes and closes.
 * Leafen's own site address, which Notion's editor has no place for, opens
 * from the share preview card; its link title and description are Notion's
 * "검색 엔진 인덱싱" screen's (TopBarSiteIndexing).
 */
export function SiteStyleEditor(props: {
  session: Session;
  page: SharePage;
  untitled: string;
  siteLink: string;
  busy: boolean;
  disabled: boolean;
  publishingAllowed: boolean;
  /** Why the editor's last publish didn't take (shareErrorText), at its panel's end (ShareErrorLine), grey while its outcome isn't known. */
  errorCode: string;
  /**
   * Why what its fields hold can't be the site's (shareFieldErrors), each
   * said under its own field, its screen shown: the address (another site
   * has it, or a form the site can't have), the Analytics ID, the share
   * preview image and the favicon.
   */
  fieldErrors: ShareFieldErrors;
  navigationCandidates: SharePage[];
  siteTitle: string;
  siteSlug: string;
  siteTheme: SiteTheme;
  siteBreadcrumbs: boolean;
  siteSearch: boolean;
  siteBranding: boolean;
  siteNavigationPageIds: string[];
  sitePreview: SitePreviewSettings;
  onSlugChange(value: string): void;
  onThemeChange(value: SiteTheme): void;
  onBreadcrumbsChange(value: boolean): void;
  onSearchChange(value: boolean): void;
  onBrandingChange(value: boolean): void;
  onToggleNavigation(pageId: string): void;
  /** The page shortcuts in a new order, as their rows are dragged. */
  onNavigationOrderChange: (pageIds: string[]) => void;
  onSitePreviewChange(value: SitePreviewSettings): void;
  onPublish(): void;
  onCancel(): void;
}) {
  const { t } = useTranslation(["siteBuilder", "common", "publicSiteView", "topBar"]);
  const ref = useRef<HTMLDivElement>(null);
  const faviconRef = useRef<HTMLDivElement>(null);
  const [screen, setScreen] = useState<Screen>("settings");
  const [faviconOpen, setFaviconOpen] = useState(false);
  const [picking, setPicking] = useState(false);
  const [frame, setFrame] = useState<HTMLIFrameElement | null>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref, onEscape: props.onCancel, initialFocus: "first" });
  const preview = props.sitePreview;
  const { uploading, error: uploadError, waitingNote: uploadWaiting, upload } = useSiteImageUpload(props.page.workspaceId, props.page.id,
    preview, props.onSitePreviewChange);
  const title = props.siteTitle.trim() || pageTitle(props.page, props.untitled);
  // Its titles draw their custom emoji, as the site does (TitleWithCustomEmoji).
  const drawn = (text: string, page: SharePage) => <TitleWithCustomEmoji text={text} named={page} workspaceId={page.workspaceId} />;
  const host = (() => { try { return new URL(props.siteLink).host; } catch { return props.siteLink; } })();
  const shortcuts = props.siteNavigationPageIds
    .map((id) => props.navigationCandidates.find((candidate) => candidate.id === id))
    .filter((candidate): candidate is SharePage => Boolean(candidate));
  // Notion's shortcut rows move by their handles (seen 2026-11-05).
  const reorder = useDragReorder("site-shortcut", (dragged, target, side) => {
    const rest = props.siteNavigationPageIds.filter((id) => id !== dragged);
    const at = rest.indexOf(target) + (side === "after" ? 1 : 0);
    props.onNavigationOrderChange([...rest.slice(0, at), dragged, ...rest.slice(at)]);
  }, { live: true });
  const unpicked = props.navigationCandidates.filter((candidate) => !props.siteNavigationPageIds.includes(candidate.id));
  const [pickQuery, setPickQuery] = useState("");
  const query = pickQuery.trim().toLocaleLowerCase();
  const found = query ? unpicked.filter((candidate) => pageTitle(candidate, props.untitled).toLocaleLowerCase().includes(query)) : unpicked;
  const analyticsValid = validGoogleAnalyticsId(preview.googleAnalyticsId);
  const canPublish = !props.disabled && props.publishingAllowed && Boolean(props.siteSlug.trim()) && analyticsValid;
  const headerDefault = props.siteBreadcrumbs && props.siteSearch && props.siteBranding && preview.allowDuplicate && preview.showShare && !shortcuts.length;

  // A site without a title of its own goes by its page's, as the site draws it (PublicSiteView).
  const draft: SitePreviewDraft = {
    title: props.siteTitle.trim(), theme: props.siteTheme, showBreadcrumbs: props.siteBreadcrumbs, showSearch: props.siteSearch,
    showBranding: props.siteBranding, allowDuplicate: preview.allowDuplicate, showShare: preview.showShare, navigationPageIds: props.siteNavigationPageIds,
    picked: screen === "header" ? "header" : "",
  };
  const onPick = useCallback(() => { setScreen("header"); setFaviconOpen(false); }, []);
  // A publish refused for what a field holds shows that field and why.
  const errors = props.fieldErrors;
  const refusedScreen: Screen | "" = errors.slug ? "share" : errors.googleAnalyticsId ? "analytics"
    : errors.previewImageFileId || errors.faviconFileId ? "settings" : "";
  useEffect(() => { if (refusedScreen) setScreen(refusedScreen); }, [refusedScreen]);
  useSitePreviewFrame(frame, draft, onPick);

  // The favicon's choices close from a press anywhere else, as a menu does.
  useEffect(() => {
    if (!faviconOpen) return undefined;
    const onPointer = (event: PointerEvent) => {
      if (!faviconRef.current?.contains(event.target as Node)) setFaviconOpen(false);
    };
    document.addEventListener("pointerdown", onPointer);
    return () => document.removeEventListener("pointerdown", onPointer);
  }, [faviconOpen]);

  const open = (next: Screen) => { setScreen(next); setPicking(false); setFaviconOpen(false); };
  const heading = (text: string, back: boolean) => (
    <h2 className={css.panelTitle} data-site-editor-title>
      {back ? <button type="button" className={css.back} onClick={() => open("settings")} aria-label={t("common:actions.back")}
        data-site-editor-back><ArrowLeft size="small" aria-hidden="true" /></button> : null}
      {text}
    </h2>
  );
  const fileInput = (kind: "preview" | "favicon") => (
    <input type="file" accept={SITE_IMAGE_TYPES} disabled={props.disabled || uploading !== ""}
      onChange={(event) => { void upload(kind, event.currentTarget.files?.[0]); event.currentTarget.value = ""; setFaviconOpen(false); }} />
  );
  const row = (name: string, label: string, value: ReactNode, onClick: () => void, expanded?: boolean) => (
    <button type="button" className={css.row} onClick={onClick} data-site-editor-row={name} aria-expanded={expanded}>
      <span>{label}</span><span className={css.value}>{value}</span>
    </button>
  );

  const settings = <>
    {heading(t("siteBuilder:editorSettings"), false)}
    {props.publishingAllowed ? null : <p className={css.help} data-share-publishing-off="true">{t("topBar:publishingTurnedOff")}</p>}
    <span className={css.label}>{t("siteBuilder:previewShare")}</span>
    <div className={css.card} data-site-editor-preview-card>
      <button type="button" className={css.cardOpen} onClick={() => open("share")} aria-label={t("siteBuilder:previewShare")}>
        {preview.previewImageFileId ? <SiteImageThumbnail className={css.cardImage} session={props.session} fileId={preview.previewImageFileId} /> : <>
          <span className={css.cardTop} />
          <span className={css.cardText}>
            <strong>{drawn(title, props.page)}</strong>
            {props.siteBranding ? <small>{t("publicSiteView:madeWithLeafen")}</small> : null}
          </span>
        </>}
      </button>
      <span className={css.cardTools}>
        {/* The site's own address, which Notion's editor has no place
            for, opens from the card: said on it too. */}
        <button type="button" className={css.cardTool} onClick={() => open("share")}
          data-site-preview-edit>{t("siteBuilder:previewEdit")}</button>
        {preview.previewImageFileId ? (
          <button type="button" className={css.cardTool} disabled={props.disabled}
            onClick={() => props.onSitePreviewChange({ ...preview, previewImageFileId: "" })}>{t("siteBuilder:assetRemove")}</button>
        ) : null}
        <label className={css.cardTool} data-site-preview-upload>
          {fileInput("preview")}
          {uploading === "preview" ? t("siteBuilder:assetUploading")
            : t(preview.previewImageFileId ? "siteBuilder:assetReplace" : "siteBuilder:assetUpload")}
        </label>
      </span>
    </div>
    <ShareErrorLine text={errors.previewImageFileId ?? ""} className={css.imageInvalid} data-site-preview-invalid />
    <span className={css.label}>{t("siteBuilder:theme")}</span>
    <div className={css.tiles} role="radiogroup" aria-label={t("siteBuilder:theme")}>
      {([["system", LaptopIcon, "themeSystem"], ["light", SunIcon, "themeLightMode"], ["dark", MoonIcon, "themeDarkMode"]] as const)
        .map(([value, Icon, label]) => (
          <button key={value} type="button" role="radio" aria-checked={props.siteTheme === value} className={css.tile}
            disabled={props.disabled} onClick={() => props.onThemeChange(value)} data-site-theme-tile={value}>
            <span className={css.tileBox}><Icon size="regular" aria-hidden="true" /></span>
            <span className={css.tileLabel}>{t(`siteBuilder:${label}`)}</span>
          </button>
        ))}
    </div>
    <div ref={faviconRef} className={css.favicon}>
      {row("favicon", t("siteBuilder:favicon"), <>
        {preview.faviconFileId ? <SiteImageThumbnail className={css.faviconImage} session={props.session} fileId={preview.faviconFileId} />
          : <img className={css.faviconImage} src="/favicon-32.png" alt="" />}
        <ChevronDown size="small" aria-hidden="true" />
      </>, () => setFaviconOpen((value) => !value), faviconOpen)}
      {faviconOpen ? (
        <div className={css.menu} data-site-favicon-menu
          onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); setFaviconOpen(false); } }}>
          <button type="button" className={css.menuItem} disabled={props.disabled}
            onClick={() => { props.onSitePreviewChange({ ...preview, faviconFileId: "" }); setFaviconOpen(false); }}>
            <img className={css.faviconImage} src="/favicon-32.png" alt="" />
            <span>{t("siteBuilder:faviconLeafenLogo")}</span>
            {preview.faviconFileId ? null : <CheckIcon size="small" aria-hidden="true" />}
          </button>
          {preview.faviconFileId ? (
            <div className={css.menuItem} data-chosen>
              <SiteImageThumbnail className={css.faviconImage} session={props.session} fileId={preview.faviconFileId} />
              <span>{t("siteBuilder:faviconUploaded")}</span>
              <CheckIcon size="small" aria-hidden="true" />
            </div>
          ) : null}
          <label className={css.upload} data-site-favicon-upload>
            {fileInput("favicon")}
            {uploading === "favicon" ? t("siteBuilder:assetUploading") : t("siteBuilder:assetUpload")}
          </label>
          <p className={css.menuHint}>{t("siteBuilder:faviconHint")}</p>
        </div>
      ) : null}
    </div>
    <ShareErrorLine text={errors.faviconFileId ?? ""} className={css.imageInvalid} data-site-favicon-invalid />
    {row("header", t("siteBuilder:header"), <>
      {headerDefault ? t("siteBuilder:headerDefault") : null}<ChevronRight size="small" aria-hidden="true" />
    </>, () => open("header"))}
    {row("analytics", t("siteBuilder:googleAnalyticsTitle"), <>
      {t(preview.googleAnalyticsId ? "siteBuilder:on" : "siteBuilder:off")}<ChevronRight size="small" aria-hidden="true" />
    </>, () => open("analytics"))}
  </>;

  const header = <>
    {heading(t("siteBuilder:header"), true)}
    {([
      ["breadcrumbPath", props.siteBreadcrumbs, props.onBreadcrumbsChange, <span className={css.pathGlyph}>…/</span>],
      ["search", props.siteSearch, props.onSearchChange, <Search size="small" />],
      ["share", preview.showShare, (showShare: boolean) => props.onSitePreviewChange({ ...preview, showShare }), <ShareSiteIcon size="small" />],
      ["duplicateAsTemplate", preview.allowDuplicate, (allowDuplicate: boolean) => props.onSitePreviewChange({ ...preview, allowDuplicate }), <Copy size="small" />],
      ["branding", props.siteBranding, props.onBrandingChange, <img className={css.switchLogo} src="/favicon-32.png" alt="" />],
    ] as const).map(([name, on, change, icon]) => (
      // Each switch with the icon its header item has, as Notion's rows (seen 2026-11-06).
      <label key={name} className={css.switchRow} data-site-style-switch={name}>
        <span className={css.switchName}><span className={css.switchIcon} aria-hidden="true">{icon}</span>{t(`siteBuilder:${name}`)}</span>
        <SwitchInput checked={on} disabled={props.disabled} onChange={(event) => change(event.target.checked)} />
      </label>
    ))}
    <span className={`${css.label} ${css.section} ${css.labelWithTip}`}>
      {t("siteBuilder:shortcuts")}
      <IconTipButton label={t("siteBuilder:shortcutsLimit", { count: MAX_SHORTCUTS })} className={css.labelTip}>
        <QuestionCircleIcon size="small" aria-hidden="true" />
      </IconTipButton>
    </span>
    <p className={css.help} data-site-shortcuts-help>{t("siteBuilder:shortcutsHelp")}</p>
    {shortcuts.map((candidate, index) => {
      return <div key={candidate.id} className={css.shortcut} data-site-shortcut={candidate.id} {...reorder.row(candidate.id)}>
        <span className={css.handle} aria-label={t("siteBuilder:moveShortcut")} role="button" tabIndex={props.disabled ? -1 : 0}
          {...reorder.handle(candidate.id, !props.disabled && shortcuts.length > 1,
            { above: shortcuts[index - 1]?.id, below: shortcuts[index + 1]?.id })}>
          <DragHandleIcon size="small" aria-hidden="true" />
        </span>
        <span className={css.shortcutTitle}>{drawn(pageTitle(candidate, props.untitled), candidate)}</span>
        <button type="button" className={css.remove} aria-label={t("siteBuilder:removeShortcut")} disabled={props.disabled}
          onClick={() => props.onToggleNavigation(candidate.id)}><X size="small" aria-hidden="true" /></button>
      </div>;
    })}
    {/* Notion's "+ 페이지 추가" (seen 2026-11-06) is a tinted button under the
        rows, empty or not; pressed, it becomes a search field over the
        site's pages, up to five shortcuts. */}
    {picking ? (
      <div className={css.pickBox} onKeyDown={(event) => {
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setPicking(false); }
      }}>
        <label className={css.pickSearch}>
          <Search size="small" aria-hidden="true" />
          <input autoFocus={fieldFocusOnOpen()} value={pickQuery} placeholder={t("siteBuilder:choosePage")} aria-label={t("siteBuilder:choosePage")}
            onChange={(event) => setPickQuery(event.target.value)}
            onBlur={(event) => { if (!event.currentTarget.closest(`.${css.pickBox}`)?.contains(event.relatedTarget as Node)) setPicking(false); }} />
        </label>
        <div className={css.picker} role="listbox" aria-label={t("siteBuilder:shortcuts")}>
          {found.map((candidate) => (
            <button key={candidate.id} type="button" role="option" aria-selected="false" className={css.menuItem}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => { props.onToggleNavigation(candidate.id); setPicking(false); }}>
              <PageIconGlyph page={{ icon: candidate.icon ?? "", iconType: candidate.iconType ?? "none", kind: candidate.kind,
                hasContent: candidate.hasContent, workspaceId: candidate.workspaceId }} size={16} />
              <span>{drawn(pageTitle(candidate, props.untitled), candidate)}</span>
            </button>
          ))}
          {found.length ? <span className={css.pickRule} aria-hidden="true" /> : null}
          <p className={css.menuHint}>{t("siteBuilder:shortcutsNone")}</p>
        </div>
      </div>
    ) : shortcuts.length < MAX_SHORTCUTS ? (
      <button type="button" className={css.addPage} disabled={props.disabled}
        onClick={() => { setPickQuery(""); setPicking(true); }} data-site-add-shortcut>
        <Plus size="small" aria-hidden="true" />{t("siteBuilder:addShortcut")}
      </button>
    ) : null}
  </>;

  const analytics = <>
    {heading(t("siteBuilder:googleAnalyticsTitle"), true)}
    <p className={css.help}>{t("siteBuilder:googleAnalyticsHelp")}</p>
    <label className={css.field} data-site-analytics>{t("siteBuilder:googleAnalyticsField")}
      <input value={preview.googleAnalyticsId} disabled={props.disabled} aria-invalid={!analyticsValid || Boolean(errors.googleAnalyticsId) || undefined}
        onChange={(event) => props.onSitePreviewChange({ ...preview, googleAnalyticsId: event.target.value.trim().toUpperCase() })} />
    </label>
    <ShareErrorLine text={analyticsValid ? errors.googleAnalyticsId ?? "" : t("siteBuilder:googleAnalyticsInvalid")}
      className={css.invalid} data-site-analytics-invalid />
  </>;

  const share = <>
    {heading(t("siteBuilder:previewShare"), true)}
    <label className={css.field}>{t("siteBuilder:address")}
      <span className={css.slug} data-invalid={errors.slug ? "true" : undefined}><span>/site/</span>
        <input value={props.siteSlug} disabled={props.disabled} aria-invalid={errors.slug ? true : undefined}
          onChange={(event) => props.onSlugChange(event.target.value)} data-site-slug /></span>
    </label>
    <ShareErrorLine text={errors.slug ?? ""} className={css.invalid} data-site-address-taken />
  </>;

  return createPortal(
    <div ref={ref} className={css.editor} style={{ zIndex }} role="dialog" aria-modal="true" aria-label={t("siteBuilder:customizeStyle")}
      data-site-style-editor>
      <header className={css.bar}>
        <button type="button" className={css.cancel} onClick={props.onCancel} data-site-editor-cancel>
          <X size="micro" aria-hidden="true" />{t("common:actions.cancel")}
        </button>
        <span className={css.barTitle}>{drawn(title, props.page)}</span>
        <button type="button" className={css.publish} disabled={!canPublish || props.busy} onClick={props.onPublish} data-site-update>
          {props.busy ? t("siteBuilder:saving") : t("siteBuilder:publishChanges")}
        </button>
      </header>
      <div className={css.body}>
        <div className={css.frame} data-site-editor-preview>
          <div className={css.chrome}>
            <span className={css.dots} aria-hidden="true"><i /><i /><i /></span>
            <span className={css.address}>
              {preview.faviconFileId ? <SiteImageThumbnail className={css.faviconImage} session={props.session} fileId={preview.faviconFileId} />
                : <img className={css.faviconImage} src="/favicon-32.png" alt="" />}
              <span>{host}</span>
            </span>
          </div>
          {props.siteLink ? <iframe ref={setFrame} className={css.site} title={title} src={sitePreviewSrc(props.siteLink)} /> : null}
        </div>
        <aside className={css.panel} data-site-editor-screen={screen}
          onKeyDown={(event) => { if (event.key === "Escape" && (faviconOpen || picking)) { event.preventDefault(); setFaviconOpen(false); setPicking(false); } }}>
          {screen === "settings" ? settings : screen === "header" ? header : screen === "analytics" ? analytics : share}
          {uploadError ? <ShareErrorLine text={uploadError} className={css.failure} data-site-upload-failure />
            : <ShareErrorLine text={uploadWaiting ?? ""} className={css.failure} waiting data-site-upload-waiting />}
          <ShareErrorLine text={refusedScreen ? "" : props.errorCode} className={css.failure} data-site-editor-failure />
        </aside>
      </div>
    </div>,
    document.body,
  );
}
