import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { DotsHorizontal } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import type { DbProperty, Page } from "@/lib/types";
import styles from "./databaseSheet";
import { DatabasePropertyVisibilityList } from "./DatabasePropertyVisibilityList";
import { PropValue, type PropertyValueReadContext } from "./PropValue";

/** What a relation's picker shows beside each page's name, kept with the relation, as Notion's (seen 2026-11-04). */
export function relationPickerShownIds(prop: DbProperty): string[] {
  const ids = (prop.config as { relationShownProperties?: unknown } | undefined)?.relationShownProperties;
  return Array.isArray(ids) ? ids.map(String) : [];
}

function asView(shownIds: readonly string[], properties: readonly DbProperty[]) {
  const title = properties.filter((property) => property.type === "title").map(({ id }) => id);
  const shown = shownIds.filter((id) => properties.some((property) => property.id === id));
  const rest = properties.map(({ id }) => id).filter((id) => !title.includes(id) && !shown.includes(id));
  // Its rows show their title first.
  return { type: "table", titleFirst: true, config: { propertyOrder: [...title, ...shown, ...rest], hiddenProperties: rest } as Record<string, unknown> };
}

function shownOf(config: Record<string, unknown>, properties: readonly DbProperty[]) {
  const hidden = new Set(Array.isArray(config.hiddenProperties) ? config.hiddenProperties.map(String) : []);
  const order = Array.isArray(config.propertyOrder) ? config.propertyOrder.map(String) : [];
  return order.filter((id) => !hidden.has(id) && properties.some((property) => property.id === id && property.type !== "title"));
}

/** "···" at the head of the picker's list: which of the target's properties its rows show. */
export function RelationPickerProperties({ shownIds, properties, writable, onShown }: {
  shownIds: readonly string[];
  properties: readonly DbProperty[];
  writable: boolean;
  onShown(ids: string[]): void;
}) {
  const { t } = useTranslation("propertyCell");
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  usePopoverAppear(open, panelRef, () => buttonRef.current);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "picker", ref: panelRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current] });
  const place = open ? buttonRef.current?.getBoundingClientRect() : null;
  // From the button's left edge, below it, or up as far as its height needs (Notion's, seen 2026-11-05).
  const [top, setTop] = useState(0);
  useLayoutEffect(() => {
    if (!open || !panelRef.current || !buttonRef.current) return;
    const below = buttonRef.current.getBoundingClientRect().bottom + 4;
    setTop(Math.max(8, Math.min(below, window.innerHeight - panelRef.current.offsetHeight - 8)));
  }, [open, properties.length]);
  return (
    <>
      <button ref={buttonRef} type="button" className={styles.relationPropertiesButton} aria-label={t("relationChooseProperties")}
        title={t("relationChooseProperties")} aria-haspopup="dialog" aria-expanded={open} data-relation-properties
        onClick={(event) => { event.stopPropagation(); setOpen((current) => !current); }}>
        <DotsHorizontal size="small" aria-hidden="true" />
      </button>
      {open && place ? createPortal(
        // A press in it stays in it: drawn apart, it still sits under a page section's line, whose empty
        // places open the relation's picker.
        <div ref={panelRef} className={styles.relationPropertiesPanel} role="dialog" aria-label={t("relationChooseProperties")} onKeyDown={onKeyDown}
          onClick={(event) => event.stopPropagation()}
          style={{ zIndex, top, left: Math.min(place.left, window.innerWidth - 188) }}>
          <DatabasePropertyVisibilityList properties={properties as unknown as readonly GraphProperty[]} search="" writable={writable}
            other={{ asView: asView(shownIds, properties), shownIn: t("relationShownIn"), hiddenIn: t("relationHiddenIn"),
              onChange: (config) => onShown(shownOf(config, properties)) }} />
        </div>, document.body) : null}
    </>
  );
}

/** A page's chosen values beside its name in the picker. */
export function RelationPickerValues({ page, shownIds, properties, context }: {
  page: Page;
  shownIds: readonly string[];
  properties: readonly DbProperty[];
  context: PropertyValueReadContext;
}) {
  const shown = useMemo(() => shownIds.flatMap((id) => properties.filter((property) => property.id === id)), [properties, shownIds]);
  if (!shown.length) return null;
  return (
    <span className={styles.relationOptionValues}>
      {shown.map((property) => <PropValue key={property.id} context={context} row={page} prop={property} interactive={false} />)}
    </span>
  );
}
