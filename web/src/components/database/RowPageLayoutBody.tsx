import { createContext, Fragment, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { DoubleChevronRight } from "@/icons/leafen";
import type { DatabasePageLayout, PageLayoutModule } from "@/lib/databasePageLayout";
import { PhoneSheet } from "../shell/PhoneSheet";
import { propertyGroupItems, relationShownAs } from "./pageLayoutModel";
import { RelationSectionContext } from "./relationSection";
import styles from "./RowPageLayout.module.css";

/** Whether a property is drawn as a relations group's minimized line, "↗ 작업 추가" (Notion's, seen 2026-11-06). */
export const RelationLineContext = createContext(false);

/** Whether the layout pins a property the page shows under its title (RowPagePinnedProperties). */
export function pinnedShown(pinned: readonly string[], properties: readonly { id: string }[]) {
  return pinned.some((id) => properties.some((property) => property.id === id));
}

/** Whether a page's details panel is open: closed as the page opens, then
 *  as the person opens or closes it — from "세부 정보 보기" under the title or
 *  the panel's "»", as Notion's (seen 2026-11-06; 2026-11-12: closed again
 *  once the page was read again, however the layout was applied). */
export type RowPageDetails = { panel: boolean; open: boolean; setOpen(open: boolean): void };

/**
 * A page's details panel, there while it holds something: given the
 * database's properties, a panel whose property group lists none (every
 * property pinned under the title or in a module of its own) is none, and
 * "세부 정보 보기" isn't offered, as Notion's (seen 2026-11-12: a meeting
 * notes page with its date and attendees pinned had none, and had it once a
 * property was added).
 */
export function useRowPageDetails(layout: DatabasePageLayout | null, propertyIds?: readonly string[]): RowPageDetails {
  const [open, setOpen] = useState<boolean>();
  return { panel: layoutHasDetailsPanel(layout, propertyIds), open: open ?? false, setOpen };
}

/** Whether a layout's details panel holds something to show (useRowPageDetails), which the layout builder's title shows too. */
export function layoutHasDetailsPanel(layout: DatabasePageLayout | null, propertyIds?: readonly string[]) {
  const listed = !layout || !propertyIds || propertyGroupItems(layout, propertyIds).some((item) => item.kind === "property");
  return Boolean(layout?.panel.some((module) => module.kind !== "properties" || listed));
}

/**
 * A database page's modules in its layout's order, as Notion's (seen
 * 2026-10-12): the page's down the page, the details panel's beside it on
 * the right, folded by its "»", and after the page's where the page is narrow.
 * On a phone the panel is a whole screen, "페이지 정보" with "완료", for a
 * member and a published page's visitor alike (Notion's, seen 2026-11-12).
 */
export function RowPageLayoutBody({ layout, details, properties = [], frameMode, valueOf, renderModule }: {
  layout: DatabasePageLayout;
  details: RowPageDetails;
  /** The page's properties: one pinned under the title names the page's property group (RowPagePinnedProperties). */
  properties?: readonly { id: string; type: string }[];
  /** Where the page is drawn: on the page or in a side peek the panel is a column of its frame; a published
   *  page at its own address ("window") a column of the window; on a phone a whole screen; else it opens in place. */
  frameMode?: string;
  /** The page's value of a property: a relation shown as a section draws as one only while it holds pages. */
  valueOf?(propertyId: string): unknown;
  renderModule(module: PageLayoutModule, place: "page" | "panel"): ReactNode;
}) {
  const { t } = useTranslation(["rowProperties", "pageLayout", "common"]);
  const panelOpen = details.panel && details.open;
  // Not on a phone, whose page Notion leaves unnamed under its pinned properties (seen 2026-11-13 at 402pt).
  const pinned = frameMode !== "phone" && pinnedShown(layout.heading.pinned, properties);
  const anchorRef = useRef<HTMLDivElement>(null);
  const [frame, setFrame] = useState<HTMLElement | null>(null);
  const windowed = frameMode === "window";
  // A published page's window is its site, whose theme the panel takes, or the page alone.
  useLayoutEffect(() => setFrame(windowed ? anchorRef.current?.closest<HTMLElement>("[data-site-slug]") ?? document.body
    : anchorRef.current?.closest<HTMLElement>("[data-row-peek-panel]") ?? null), [windowed]);
  const framed = Boolean(frame) && (frameMode === "side" || frameMode === "full" || windowed);
  // The frame makes room for its panel: the page's column narrows, a side peek widens.
  useEffect(() => {
    if (!frame || !framed || !panelOpen) return undefined;
    const key = windowed ? "publicDetailsPanel" : "detailsPanel";
    frame.dataset[key] = "open";
    return () => { delete frame.dataset[key]; };
  }, [frame, framed, panelOpen, windowed]);
  const modules = (list: readonly PageLayoutModule[], place: "page" | "panel") => list.map((module) => {
    // A relations group: "관계형" over its minimized relations, each drawn as a property of its own, a page
    // section under its own name (Notion's, seen 2026-11-06).
    const shown = module.kind === "relations"
      ? module.relations.map((relation) => ({ ...relation, display: relationShownAs(relation.display, valueOf?.(relation.propertyId) ?? [null]) })) : [];
    // A relation that is a card of its own is a page section, empty too — its name, its pages, then "↗ 기존 항목
    // 연결 · + 새 페이지 추가하기" — as a relations group's (Notion's, seen 2026-11-13 on the page and in the builder).
    const relationCard = module.kind === "property" && properties.some((property) => property.id === module.propertyId && property.type === "relation");
    const own = module.kind === "relations" ? null : renderModule(module, place);
    const rendered = module.kind === "relations" ? <>
      {shown.some(({ display }) => display === "minimized") ? <div className={styles.groupLabel} data-relations-heading>{t("pageLayout:relationsHeading")}</div> : null}
      {shown.map((relation) => <RelationLineContext.Provider key={relation.propertyId} value={relation.display === "minimized"}>
        <RelationSectionContext.Provider value={relation.display === "section"}>
          <div data-relation-line={relation.display}>
            {renderModule({ id: `${module.id}:${relation.propertyId}`, kind: "property", propertyId: relation.propertyId }, place)}
          </div>
        </RelationSectionContext.Provider>
      </RelationLineContext.Provider>)}
    </> : relationCard && own !== null ? (
      <RelationSectionContext.Provider value><div data-relation-line="section">{own}</div></RelationSectionContext.Provider>
    ) : own;
    return rendered === null ? <Fragment key={module.id} /> : (
      <div key={module.id} className={styles.module} data-row-layout-module={relationCard ? "relations" : module.kind}
        data-row-layout-relation-card={relationCard ? "true" : undefined}>
        {/* A tabbed layout names its property group, as do the panel and a page with properties pinned under its title,
            as Notion's desktop (seen 2026-11-01, 2026-11-06, 2026-11-13: not for a page module or a panel alone). */}
        {module.kind === "properties" && (layout.structure === "tabbed" || place === "panel" || pinned) ? (
          <div className={styles.groupLabel}
            data-row-layout-properties-heading={layout.structure !== "tabbed" && place === "page" ? "pinned" : ""}>{t("properties")}</div>
        ) : null}
        {rendered}
      </div>
    );
  });
  if (frameMode === "phone") {
    return (
      <div ref={anchorRef} className={styles.frame}>
        {panelOpen ? (
          <PhoneSheet size="screen" title={t("rowProperties:pageInfo")} onClose={() => details.setOpen(false)}
            action={{ label: t("common:actions.done"), onPress: () => details.setOpen(false) }} data={{ "data-row-details-sheet": "true" }}>
            <div className={styles.detailsSheetBody} data-row-layout-panel>{modules(layout.panel, "panel")}</div>
          </PhoneSheet>
        ) : null}
        <div>{modules(layout.page, "page")}</div>
      </div>
    );
  }
  const framePanel = panelOpen && framed && frame ? createPortal(
    <div className={styles.framePanel} data-place={frameMode} data-row-layout-panel>
      <button type="button" className={styles.framePanelClose} aria-label={t("layoutPanelHide")} onClick={() => details.setOpen(false)} data-row-layout-panel-toggle>
        <DoubleChevronRight size="small" aria-hidden="true" />
      </button>
      <aside className={styles.framePanelBody} aria-label={t("layoutPanel")}>{modules(layout.panel, "panel")}</aside>
    </div>, frame) : null;
  return (
    <div ref={anchorRef} className={styles.frame}>
      {framePanel}
      <div className={styles.columns} data-panel={panelOpen && !framed ? "true" : "false"}>
        <div>{modules(layout.page, "page")}</div>
        {panelOpen && !framed ? (
          <aside className={styles.panel} aria-label={t("layoutPanel")} data-row-layout-panel>
            <div className={styles.panelToggle}>
              <button type="button" aria-label={t("layoutPanelHide")} onClick={() => details.setOpen(false)} data-row-layout-panel-toggle>
                <DoubleChevronRight size="small" aria-hidden="true" />
              </button>
            </div>
            {modules(layout.panel, "panel")}
          </aside>
        ) : null}
      </div>
    </div>
  );
}
