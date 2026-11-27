import type { SharePage } from "./topBarShareModel";
import { useContext } from "react";
import { useTranslation } from "react-i18next";
import { TableIcon, DotsHorizontal, FileText, InfoIcon, Search } from "@/icons/leafen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { RichTextLine } from "../database/RichTextCellText";

import { PageBodyLead } from "./pageBodyLead";
import { TitleWithCustomEmoji } from "./TitleWithCustomEmoji";
import { pageTitle } from "./topBarShareModel";
import styles from "./TopBarSitePublishIntro.module.css";
import { ShareErrorLine } from "./ShareErrorLine";
import { phoneScreen } from "@/lib/screenKind";

/**
 * The share sheet's classes that let the publish tab fill it, on a touch
 * screen wider than a phone (TouchSheetLayout reads it as a phone's), as
 * Notion's iPad (TopBarSitePublishIntro.module.css); none on a phone or a
 * computer, or while another tab is chosen.
 */
export function publishSheetFill(sheet: boolean, publishChosen: boolean) {
  const fill = sheet && !phoneScreen();
  return { body: fill && publishChosen ? styles.fillBody : undefined, tabs: fill && publishChosen ? styles.fillTabs : undefined,
    panel: fill ? styles.fillPanel : undefined };
}

/**
 * The publish tab before a page is published, as Notion's (seen 2026-10-04,
 * 2026-10-08): "웹에 게시" over a line saying what it does, a small preview
 * of the page as a site in a browser frame, one blue "게시" button and what
 * publishing shows to whom. The site's settings come once it is published.
 * A phone's previews it on a phone, as Notion's (seen 2026-10-27 at 375px):
 * a 290px phone 24px round with "9:41" and its marks over the site's bar,
 * the page's title 24px, under a 20px heading; a 50px button and a 15px
 * notice. Its lines draw their equations, mentions and marks as the page
 * does, as Notion's (seen 2026-11-13: an inline equation drawn as math).
 * On a computer the page is drawn small as Notion's (read 2026-11-14): its
 * title and the start of its first blocks, cut and faded at the foot.
 */
export function TopBarSitePublishIntro({
  busy,
  disabled,
  errorCode,
  onPublish,
  page,
  publishingAllowed,
  subpages,
  untitled,
}: {
  busy: boolean;
  disabled: boolean;
  errorCode: string;
  onPublish: () => void;
  page: SharePage;
  /** Whether the organization lets the page be published to the web. */
  publishingAllowed: boolean;
  /** Pages under this one, the first of which the preview lists. */
  subpages: SharePage[];
  untitled: string;
}) {
  const { t } = useTranslation(["siteBuilder", "topBar"]);
  // Its titles draw their custom emoji, as the site will (TitleWithCustomEmoji).
  const title = <TitleWithCustomEmoji text={pageTitle(page, untitled)} named={page} workspaceId={page.workspaceId} />;
  const lead = useContext(PageBodyLead)();
  const { phone } = useSidebarLayout(true);
  return (
    <div className={styles.intro} data-share-panel="publish" data-site-unpublished="true" data-phone={phone ? "true" : undefined}>
      <div className={styles.heading}>
        <strong>{t("siteBuilder:publishToWeb")}</strong>
        <span>{t("siteBuilder:publishToWebDescription")}</span>
      </div>
      {publishingAllowed ? null : (
        <p className={styles.turnedOff} data-share-publishing-off="true">{t("topBar:publishingTurnedOff")}</p>
      )}
      <div className={styles.preview} aria-hidden="true" data-site-publish-preview>
        {phone ? <div className={styles.statusBar}>
          <span>9:41</span>
          <span className={styles.statusMarks}>
            <svg viewBox="0 0 15 15"><path d="M1 13h2V10H1zm4 0h2V7H5zm4 0h2V4H9zm4 0h2V1h-2z" fill="currentColor" /></svg>
            <svg viewBox="0 0 16 11"><path d="M8 10.5 5.6 8a3.4 3.4 0 0 1 4.8 0zM3.4 5.8a6.5 6.5 0 0 1 9.2 0l-1.3 1.3a4.6 4.6 0 0 0-6.6 0zM1.2 3.6a9.6 9.6 0 0 1 13.6 0l-1.3 1.3a7.8 7.8 0 0 0-11 0z" fill="currentColor" /></svg>
            <svg viewBox="0 0 8 14"><rect x="0.5" y="1.5" width="7" height="12" rx="1.5" fill="none" stroke="currentColor" /><rect x="2" y="0" width="4" height="1.5" fill="currentColor" /><rect x="2" y="5" width="4" height="7" fill="currentColor" /></svg>
          </span>
        </div> : <div className={styles.frameBar}><span /><span /><span /></div>}
        <div className={styles.siteBar}>
          <span className={styles.siteBarTitle}>{title}</span>
          <Search size={phone ? "tiny" : "micro"} />
          <DotsHorizontal size={phone ? "tiny" : "micro"} />
        </div>
        <div className={styles.page}>
          <div className={styles.pageTitle}>{title}</div>
          <div className={phone ? undefined : styles.blocks}>
            {lead.map((line, index) => (
              <div key={index} className={styles.pageText} data-heading={line.type.includes("heading") ? "true" : undefined}><RichTextLine text={line.text} spans={line.spans} /></div>
            ))}
            {subpages.slice(0, 3).map((subpage) => (
              <div key={subpage.id} className={styles.pageRow}>
                {subpage.kind === "database" ? <TableIcon size="tiny" /> : <FileText size="tiny" />}<span><TitleWithCustomEmoji text={pageTitle(subpage, untitled)} named={subpage} workspaceId={subpage.workspaceId} /></span>
              </div>
            ))}
          </div>
        </div>
      </div>
      {phone ? null : <div className={styles.fade} aria-hidden="true" />}
      <ShareErrorLine text={errorCode} />
      <button type="button" className={styles.publish} data-menu-item data-site-publish
        disabled={disabled || !publishingAllowed} onClick={onPublish}>
        {busy ? t("siteBuilder:saving") : t("siteBuilder:publishShort")}
      </button>
      <p className={styles.notice}><InfoIcon size="small" aria-hidden="true" />{t("siteBuilder:publishNotice")}</p>
    </div>
  );
}
