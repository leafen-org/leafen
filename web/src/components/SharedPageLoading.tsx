import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import { publicOpening, publicOpeningFrame, type PublicOpeningRead } from "@/lib/publicOpeningShape";
import { StandaloneTopBarTitle } from "./page/TopBarTitle";
import { ViewLinkFrameHost } from "./ViewLinkFrameHost";
import coverStyles from "./page/PageCover.module.css";
import headerStyles from "./page/PageHeader.module.css";
import pageStyles from "./page/PageView.module.css";
import frameStyles from "./ViewLinkView.module.css";
import styles from "./SharedPageLoading.module.css";

/**
 * Server-authoritative public shares never render cached content before the
 * access check. Keep this fallback in the synchronous shell so a deferred
 * share-route chunk cannot introduce a blank frame. It is drawn in the page
 * about to open's own frame (lib/publicOpeningShape) — a full-page
 * database's canvas, a full-width page or a page's column, its cover and its
 * icon — with the page's own classes and marks (PageView .doc, PageCover
 * .cover, PageHeader .header), so its head's rules lay its title where the
 * page's title will stand and nothing moves when the page comes; a view link
 * made for a tablet or a phone, in a wider window, in that screen's frame
 * (ViewLinkFrameHost). pageId and read name the page opened from a page
 * already read.
 */
export function SharedPageLoading({ pageId, read }: { pageId?: string; read?: PublicOpeningRead } = {}) {
  const { t } = useTranslation("sharedPageView");
  const opening = publicOpening(pageId, read);
  const frame = publicOpeningFrame();
  const database = opening.shape === "database";
  const cover = opening.cover ? "true" : "false";
  // A database's name has its icon beside it, a page's title under it, a
  // picture in its own box (PageHeader).
  const icon = opening.icon === "none" ? null : (
    <div className={headerStyles.iconWrap}>
      <span className={headerStyles.bigIcon} aria-hidden="true"
        data-icon-picture={opening.icon === "picture" && !database ? "true" : undefined}>
        <span className={`${styles.iconFill} ${styles.part}`} />
      </span>
    </div>
  );
  const screen = (
    <>
      {frame.bar ? <StandaloneTopBarTitle title={t("title")} /> : null}
      {/* Without the bar the page takes the whole window, as a site's does (PublicSiteView .content). */}
      <div className={pageStyles.scroll} style={frame.bar ? undefined : { "--topbar-height": "0px" } as CSSProperties}
        data-database-canvas={database ? "true" : undefined}>
        {opening.cover ? (
          <div className={coverStyles.cover} data-database={database ? "true" : undefined}>
            <span className={`${styles.coverFill} ${styles.part}`} />
          </div>
        ) : null}
        <div
          className={pageStyles.doc}
          data-page-search-root
          data-kind="page"
          data-has-cover={cover}
          data-database-page={database ? "true" : undefined}
          data-full-width={opening.shape === "full-width" ? "true" : "false"}
          data-small-text={opening.smallText ? "true" : "false"}
          data-public-read-only="true"
          aria-busy="true"
          aria-label={t("loading")}
          role="status"
        >
          {/* A site's page embedded without its title keeps its head's room, its title alone gone (PublicSiteView). */}
          <div className={headerStyles.header} data-page-header-root data-has-cover={cover}
            data-has-icon={icon && !database ? "true" : "false"} data-database-head-icon={icon && database ? "true" : undefined}
            data-database-head={database ? "true" : undefined}>
            {database ? null : icon}
            <div className={headerStyles.titleRow}>
              {database ? icon : null}
              {frame.title ? (
                <div className={headerStyles.titleHeading}>
                  <span className={headerStyles.title}><span className={`${styles.title} ${styles.part}`} /></span>
                </div>
              ) : null}
            </div>
          </div>
          <div className={styles.body} data-shape={opening.shape}>
            {database ? <span className={`${styles.views} ${styles.part}`} /> : null}
            <span className={`${styles.line} ${database ? styles.tableLine : ""} ${styles.part}`} />
            <span className={`${styles.line} ${styles.lineShort} ${styles.part}`} />
          </div>
        </div>
      </div>
    </>
  );
  if (!frame.viewLink) return screen;
  // The screen the link was made for, in its frame, as its page will be
  // (ViewLinkView): the window its page is laid in is the frame's.
  const { width, height } = frame.viewLink;
  const frameWindow = { width, height, "--app-height": `${height}px`, "--page-window-height": `${height}px` } as CSSProperties;
  return (
    <ViewLinkFrameHost screen={frame.viewLink}>
      <div className={`${frameStyles.frame} ${styles.frame}`} style={frameWindow}>{screen}</div>
    </ViewLinkFrameHost>
  );
}
