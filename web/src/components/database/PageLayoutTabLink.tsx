import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import { FileText, RelationIcon } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import sheetStyles from "./databaseSheet";
import styles from "./PageLayoutAddMenu.module.css";
import tabStyles from "./RowPageLayout.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A relation tab's "기존 항목 연결" under its view, as Notion's (seen
 * 2026-10-12): the source's pages the open page doesn't relate to yet, found
 * by name; the one chosen joins the relation. With no page related yet it
 * is "기존 항목 연결하기", outlined beside the empty view's "+ 새 페이지"
 * (DatabaseNoResults; Notion's, seen 2026-11-12).
 */
export function PageLayoutTabLink({ rows, properties, related, onLink, outlined = false }: {
  /** The tab's source's pages. */
  rows: readonly GraphRow[];
  properties: readonly GraphProperty[];
  related: readonly string[];
  onLink(rowId: string): void;
  outlined?: boolean;
}) {
  const { t } = useTranslation("rowProperties");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const close = () => { setOpen(false); setQuery(""); };
  const place = usePopoverPlacement(menu, open ? (size, viewport) => placeAround(
    popoverRectOf(button.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4, cap: 360, minHeight: 120 },
  ) : null, [open], undefined, { kind: "menu", onEscape: close, onOutsidePress: close, inside: () => [button.current] });
  const title = properties.find((property) => property.type === "title");
  const name = (row: GraphRow) => String(title ? row.values[title.id]?.value ?? "" : "");
  const needle = query.trim().toLocaleLowerCase();
  const choices = rows.filter((row) => !related.includes(row.id) && name(row).toLocaleLowerCase().includes(needle)).slice(0, 50);
  return (
    <>
      <button ref={button} type="button" className={outlined ? sheetStyles.boardNothingAction : tabStyles.tabLink} aria-expanded={open}
        onClick={() => setOpen(!open)} data-row-layout-tab-link>
        <RelationIcon size="small" trim={outlined} aria-hidden="true" />{t(outlined ? "layoutTabLinkExistingEmpty" : "layoutTabLinkExisting")}
      </button>
      {open ? createPortal(
        <div ref={menu} className={styles.menu} role="dialog" aria-label={t("layoutTabLinkExisting")} style={popoverPlacementStyle(place)}
          data-row-layout-tab-link-menu>
          <input className={styles.search} value={query} placeholder={t("layoutTabLinkSearch")} aria-label={t("layoutTabLinkSearch")} autoFocus={fieldFocusOnOpen()}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => { if (fieldEnter(event) && choices[0]) { onLink(choices[0].id); close(); } }} />
          {choices.map((row) => (
            <button key={row.id} type="button" className={styles.row} onClick={() => { onLink(row.id); close(); }} data-row-layout-tab-link-row={row.id}>
              <span className={styles.icon}><FileText size="regular" aria-hidden="true" /></span>
              <span className={styles.text}><span>{name(row) || untitledPageDisplayTitle("page")}</span></span>
            </button>
          ))}
          {choices.length === 0 ? <div className={styles.empty}>{t("layoutTabLinkNone")}</div> : null}
        </div>,
        document.body,
      ) : null}
    </>
  );
}
