import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { CommentIcon, DotsHorizontal, VerifiedIcon } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Page, TextSpan } from "@/lib/types";
import { galleryPresentationProperty } from "./galleryViewModel";
import type { PropertyValueReadContext } from "./PropValue";
import { PropertyIcon } from "./PropertyIcon";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { RowPagePropertyValue } from "./RowPagePropertyList";
import headerStyles from "../page/PageHeader.module.css";
import styles from "./RowPageWikiProperties.module.css";

/**
 * A wiki page's properties under its title, as Notion's (seen 2026-10-15):
 * the wiki's Owner, Verification and Tags side by side, each its icon and
 * name over its value, and "⋯" opening the page's other properties — a
 * property added there joins them, not the line.
 */
export function RowPageWikiProperties({ bar, properties, values, valueRow, editableRow, propertyValueContext,
  onRichTextChange, propertyCellEnvironment, more, verifyRequest = 0, lined = true }: {
  bar: readonly string[];
  properties: readonly GraphProperty[];
  values: Readonly<Record<string, { value?: unknown } | undefined>>;
  valueRow: Page;
  editableRow?: Page;
  propertyValueContext: PropertyValueReadContext;
  onRichTextChange?: (property: GraphProperty, value: string, spans?: TextSpan[]) => void;
  propertyCellEnvironment?: PropertyCellEnvironment;
  /** The page's other properties, which "⋯" opens; none, no "⋯". */
  more: ReactNode;
  /** Counts the header's "인증 추가", each opening the Verification's menu. */
  verifyRequest?: number;
  /** The line under it, which comments under it draw instead. */
  lined?: boolean;
}) {
  const { t } = useTranslation("rowProperties");
  const barRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (verifyRequest) barRef.current?.querySelector<HTMLElement>("[data-structured-property='verification'] button[aria-haspopup='dialog']")?.click();
  }, [verifyRequest]);
  const shown = bar.flatMap((id) => {
    const index = properties.findIndex((property) => property.id === id);
    return index < 0 ? [] : [{ property: properties[index], presentationProperty: galleryPresentationProperty(properties[index], index) }];
  });
  return (
    <div ref={barRef} className={styles.bar} data-row-wiki-properties data-lined={lined ? undefined : "false"}>
      {shown.map(({ property, presentationProperty }) => (
        <div key={property.id} className={styles.property} data-row-wiki-property={property.id}>
          <div className={styles.label}>
            <PropertyIcon property={property} size="small" />
            <span>{property.name}</span>
          </div>
          <div className={styles.value} data-property-value-box>
            <RowPagePropertyValue property={property} presentationProperty={presentationProperty} value={values[property.id]}
              valueRow={valueRow} editableRow={editableRow} propertyValueContext={propertyValueContext}
              onRichTextChange={onRichTextChange} propertyCellEnvironment={propertyCellEnvironment} />
          </div>
        </div>
      ))}
      {more ? <WikiMore label={t("wikiMore")}>{more}</WikiMore> : null}
    </div>
  );
}

/**
 * A wiki page's controls over its title after "Add cover", as Notion's (seen
 * 2026-10-15): "인증 추가" while it is not verified, and "댓글 추가" while it
 * shows no comments. A phone's head leaves "인증 추가" out, as Notion's.
 */
export function RowPageWikiControls({ onVerify, onComment }: { onVerify?: () => void; onComment?: () => void }) {
  const { t } = useTranslation("pageHeader");
  const { phone } = useSidebarLayout(true);
  return <>
    {onVerify && !phone ? (
      <button type="button" className={headerStyles.ctrlBtn} data-page-header-control data-row-wiki-verify onClick={onVerify}>
        <VerifiedIcon size="tiny" weight="fill" aria-hidden="true" />
        <span>{t("addVerification")}</span>
      </button>
    ) : null}
    {onComment ? (
      <button type="button" className={headerStyles.ctrlBtn} aria-label={t("addPageComment")} data-page-header-control
        data-row-wiki-comment onClick={onComment}>
        <CommentIcon size="tiny" weight="fill" aria-hidden="true" />
        <span>{t("addComment")}</span>
      </button>
    ) : null}
  </>;
}

/**
 * A wiki bar's "⋯" and the page's other properties it opens, as Notion's
 * (measured 2026-11-18 at 1416, a page's and a version preview's bar alike):
 * the button stands in a box as tall as the columns beside it, centred over
 * all but the box's last 8px — 7px under the columns' top while they are a
 * name over a one-line value — and the list opens under that box, at the
 * columns' foot, with no gap, at the button's start.
 */
export function WikiMore({ label, children }: { label: string; children: ReactNode }) {
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  return (
    <div ref={boxRef} className={styles.moreBox}>
      <button type="button" className={styles.more} aria-label={label} aria-haspopup="dialog" aria-expanded={Boolean(anchor)}
        data-row-wiki-more onClick={() => {
          const rect = boxRef.current?.getBoundingClientRect();
          setAnchor((open) => open || !rect ? null : { top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right });
        }}>
        <DotsHorizontal size="regular" aria-hidden="true" />
      </button>
      {anchor ? <WikiMoreProperties anchor={anchor} label={label} onClose={() => setAnchor(null)}>{children}</WikiMoreProperties> : null}
    </div>
  );
}

// The page's other properties under "⋯", as Notion's list them there: each
// name beside its value, then "속성 추가" (a version's, without it: PageHistoryWikiProperties).
function WikiMoreProperties({ anchor, label, onClose, children }: { anchor: PopoverRect; label: string; onClose(): void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0 }), [anchor],
    undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={ref} className={styles.popover} role="dialog" aria-label={label} data-row-wiki-more-properties
      style={place ? popoverPlacementStyle(place) : { visibility: "hidden" }}>
      {children}
    </div>,
    document.body,
  );
}
