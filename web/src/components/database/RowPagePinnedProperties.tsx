import { type ReactNode, useCallback, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { ChevronLeft, ChevronRight } from "@/icons/leafen";
import type { Page, TextSpan } from "@/lib/types";
import { inlineSign } from "@/lib/readingDirection";
import { InfoIcon } from "../icons";
import sheet from "./databaseSheet";
import { galleryPresentationProperty } from "./galleryViewModel";
import type { PropertyValueReadContext } from "./PropValue";
import { usePropertyDescriptionTip } from "./PropertyDescriptionTip";
import { PropertyIcon } from "./PropertyIcon";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { pinnedShown, type RowPageDetails } from "./RowPageLayoutBody";
import { usePinnedPropertyMenu, type PinnedPropertyMenu } from "./RowPagePinnedPropertyMenu";
import { RowPagePropertyValue } from "./RowPagePropertyList";
import styles from "./RowPageLayout.module.css";
import nameStyles from "./RowPagePinnedName.module.css";

/**
 * Whether "세부 정보 보기" is the last line under the title: the layout has a
 * details panel and pins no property the page shows under it.
 */
export function detailsToggleLast(pinned: readonly string[], properties: readonly { id: string }[], details?: RowPageDetails) {
  return Boolean(details?.panel) && !pinnedShown(pinned, properties);
}

/** How many pinned properties Notion still lets grow past 200 (seen 2026-11-14). */
const ROOMY_PINNED = 3;

/**
 * A page layout's pinned properties under the title, as Notion's (seen
 * 2026-10-12): side by side, each with its icon and name over its value, or
 * its value alone where the layout turns the labels off. Where the layout
 * has a details panel, "세부 정보 보기" over them opens it (seen 2026-11-06).
 * They keep one line, scrolled sideways past the column's end with a fade
 * and an arrow where more go on (seen 2026-11-14). A name opens its menu
 * where the page has one (usePinnedPropertyMenu).
 */
export function RowPagePinnedProperties({ pinned, properties, labels, icons, values, valueRow, editableRow, propertyValueContext,
  onRichTextChange, propertyCellEnvironment, details, menu }: {
  pinned: readonly string[];
  properties: readonly GraphProperty[];
  labels: boolean;
  icons: boolean;
  values: Readonly<Record<string, { value?: unknown } | undefined>>;
  valueRow: Page;
  editableRow?: Page;
  propertyValueContext: PropertyValueReadContext;
  onRichTextChange?: (property: GraphProperty, value: string, spans?: TextSpan[]) => void;
  propertyCellEnvironment?: PropertyCellEnvironment;
  details?: RowPageDetails;
  menu?: PinnedPropertyMenu;
}) {
  const { t } = useTranslation("rowProperties");
  const names = usePinnedPropertyMenu(menu, properties, editableRow ?? valueRow, valueRow.id);
  const shown = pinned.flatMap((id) => {
    const index = properties.findIndex((property) => property.id === id);
    return index < 0 ? [] : [{ property: properties[index], presentationProperty: galleryPresentationProperty(properties[index], index) }];
  });
  const toggle = details?.panel ? (
    <button type="button" className={styles.detailsToggle} aria-expanded={details.open} onClick={() => details.setOpen(!details.open)} data-row-details-toggle data-row-pinned-zone>
      {t(details.open ? "layoutDetailsHide" : "layoutDetailsShow")}
    </button>
  ) : null;
  if (shown.length === 0) return toggle;
  return (
    <>{toggle}<PinnedPropertyLine shown={shown.map(({ property }) => property)} labels={labels} icons={icons} names={names}
      value={(property) => (
        <RowPagePropertyValue property={property} presentationProperty={shown.find((one) => one.property === property)!.presentationProperty}
          value={values[property.id]} valueRow={valueRow} editableRow={editableRow} propertyValueContext={propertyValueContext}
          onRichTextChange={onRichTextChange} propertyCellEnvironment={propertyCellEnvironment} />
      )} />{names.layer}</>
  );
}

/**
 * The pinned properties' line, on a page and on the layout builder's title
 * card alike: each a column of its icon and name over its value's box.
 */
export function PinnedPropertyLine({ shown, labels, icons, value, names, preview = false }: {
  shown: readonly GraphProperty[];
  /** Which names open a menu, and the open one's (usePinnedPropertyMenu). */
  names?: Pick<ReturnType<typeof usePinnedPropertyMenu>, "openId" | "opens" | "onOpen">;
  labels: boolean;
  icons: boolean;
  value: (property: GraphProperty) => ReactNode;
  /** The layout builder's title card shows it. */
  preview?: boolean;
}) {
  // Up to three, a text value takes its whole line and the others grow to 350,
  // or to 200 beside a text; more than three, each to 200 (Notion's, seen 2026-11-14).
  const roomy = shown.length <= ROOMY_PINNED;
  const opens = names?.opens, onOpen = names?.onOpen;
  const narrow = !roomy || shown.some((property) => property.type === "rich_text");
  return (
    <PinnedStrip labels={labels} preview={preview}>
      {shown.map((property) => (
        <div key={property.id} className={styles.pinnedProperty} data-row-pinned-property={property.id}
          data-width={roomy && property.type === "rich_text" ? "line" : narrow ? "narrow" : "roomy"}>
          {labels ? <PinnedLabel property={property} icons={icons} open={names?.openId === property.id}
            onOpen={opens?.(property) && onOpen ? (element) => onOpen(property, element) : undefined} /> : null}
          <div className={`${styles.pinnedValue} ${sheet.rowPinnedValue}`} aria-label={labels ? undefined : property.name} data-property-value-box>
            {value(property)}
          </div>
        </div>
      ))}
    </PinnedStrip>
  );
}

/**
 * A pinned property's icon and name; a described one keeps room for its "i"
 * after the name, drawn while the pointer is on the line, its name and
 * description in a tip above (Notion's, seen 2026-11-14: the "i" 2 after the
 * name, its slot taken whether shown or not). Where it opens the
 * property's menu it is a button, shaded under the pointer and while its menu
 * is open, its words not chosen by a long press (Notion's, seen 2026-11-16:
 * a 6-round cell, pointer, user-select none).
 */
function PinnedLabel({ property, icons, open, onOpen }: { property: GraphProperty; icons: boolean; open: boolean; onOpen?(element: HTMLElement): void }) {
  const { t } = useTranslation("rowProperties");
  const { description, handlers, tip } = usePropertyDescriptionTip(property, "above");
  const name = <>
    {icons ? <PropertyIcon property={property} size="small" /> : null}
    <span>{property.name}</span>
    {description ? <span className={styles.pinnedInfo} aria-description={description} data-property-described {...handlers}>
      <InfoIcon size="tiny" aria-hidden="true" /></span> : null}
    {tip}
  </>;
  return onOpen ? (
    <button type="button" className={`${styles.pinnedLabel} ${nameStyles.name}`} aria-haspopup="menu" aria-expanded={open} aria-label={t("propertyOptions", { name: property.name })}
      data-row-pinned-name data-open={open || undefined} onClick={(event) => onOpen(event.currentTarget)}>{name}</button>
  ) : <div className={styles.pinnedLabel}>{name}</div>;
}

/** The pinned properties' line, scrolled sideways; an arrow at an end where more go on scrolls a view on. */
function PinnedStrip({ labels, preview, children }: { labels: boolean; preview: boolean; children: ReactNode }) {
  const { t } = useTranslation("rowProperties");
  const scrollerRef = useRef<HTMLDivElement>(null);
  const [goesOn, setGoesOn] = useState({ before: false, after: false });
  const measure = useCallback(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    // Read from the right, a line scrolls to negative scrollLeft: how far along is its size.
    const along = Math.abs(scroller.scrollLeft);
    const before = along > 1, after = along + scroller.clientWidth < scroller.scrollWidth - 1;
    setGoesOn((now) => now.before === before && now.after === after ? now : { before, after });
  }, []);
  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(scroller);
    if (scroller.firstElementChild) observer.observe(scroller.firstElementChild);
    return () => observer.disconnect();
  }, [children, measure]);
  // A view on, less the fade, so what stood under the fade comes out whole.
  const go = (side: -1 | 1) => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    scroller.scrollBy({ left: side * Math.max(scroller.clientWidth - 100, 100) * inlineSign(scroller), behavior: "smooth" });
  };
  return (
    <div className={styles.pinnedStrip} data-before={goesOn.before || undefined} data-after={goesOn.after || undefined}
      data-layout-pinned={preview || undefined} data-row-pinned-zone>
      <div ref={scrollerRef} className={styles.pinnedScroller} onScroll={measure}>
        <div className={styles.pinned} data-row-pinned-properties data-labels={labels ? "true" : "false"}>{children}</div>
      </div>
      {goesOn.before ? <button type="button" className={styles.pinnedArrow} data-side="before" aria-label={t("pinnedScrollBefore")}
        onClick={() => go(-1)}><ChevronLeft size="regular" aria-hidden="true" /></button> : null}
      {goesOn.after ? <button type="button" className={styles.pinnedArrow} data-side="after" aria-label={t("pinnedScrollAfter")}
        onClick={() => go(1)}><ChevronRight size="regular" aria-hidden="true" /></button> : null}
    </div>
  );
}
