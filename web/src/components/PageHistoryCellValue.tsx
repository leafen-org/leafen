import { useContext } from "react";
import { FileText } from "@/icons/leafen";
import { useProductPages } from "@/components/product/productPagesContext";
import { pageMapOf } from "@/lib/pageIndex";
import type { DbProperty, TextSpan } from "@/lib/types";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import sheet from "./database/databaseSheet";
import { normalizeFileAttachments } from "./database/files";
import { propertyLink } from "./database/propertyLinks";
import { cellSpans } from "./database/richTextCellModel";
import { RichTextCellText } from "./database/RichTextCellText";
import { shortUrlParts, useUrlDisplay } from "./database/urlDisplay";
import { PageIconGlyph } from "./page/PageIcon";
import styles from "./PageHistoryCellValue.module.css";
import { HistoryValue, PageHistoryRelationTitles, type HistoryVersion, type PageHistoryProperty } from "./PageHistoryProperties";

/**
 * A version's value as the database's own cell draws it, read only, as
 * Notion's version preview draws its table and a row page's properties the
 * way the live ones are (seen 2026-11-13 and 2026-11-15): a text with its
 * formatting, a relation's pages as the cell's chips (the page's icon, its
 * name at 500 over a faint line), files as the cell's file chips, a link
 * over a faint line, a box ticked or not; any other kind as HistoryValue
 * reads it (option chips, dates, numbers, people, formulas).
 */
export function PageHistoryCellValue({ property, cell, version }: {
  property: PageHistoryProperty;
  cell?: { value: unknown; spans?: unknown[] };
  version?: HistoryVersion;
}) {
  const value = cell?.value;
  if (property.type === "rich_text") {
    const spans = cellSpans(cell?.spans as TextSpan[] | undefined, value);
    if (spans) return <RichTextCellText spans={spans} />;
  }
  if (property.type === "relation") return <HistoryRelation value={value} />;
  if (property.type === "files") return <HistoryFiles value={value} />;
  if (property.type === "url" || property.type === "email" || property.type === "phone") {
    return <HistoryLink property={property} value={typeof value === "string" ? value : ""} />;
  }
  if (property.type === "checkbox") {
    return (
      <span className={sheet.cellCheck} data-history-checkbox>
        <input type="checkbox" checked={value === true} readOnly tabIndex={-1} aria-label={property.name} />
      </span>
    );
  }
  return <HistoryValue property={property} value={value} version={version} />;
}

/** A relation's pages the reader may see, each as the cell's chip. */
function HistoryRelation({ value }: { value: unknown }) {
  const titles = useContext(PageHistoryRelationTitles);
  const pages = pageMapOf(useProductPages()?.pages ?? []);
  const ids = (Array.isArray(value) ? value.map(String) : []).filter((id) => id in titles);
  if (ids.length === 0) return null;
  return (
    <span className={styles.chips} data-history-relation>
      {ids.map((id) => {
        const page = pages.get(id);
        return (
          <span key={id} className={sheet.relationChip} data-row-relation-chip>
            <span className={sheet.relationPageIcon} aria-hidden="true">
              <PageIconGlyph page={page ?? { icon: "", iconType: "none" }} size={18} fallback="file" />
            </span>
            <span className={sheet.relationChipLink}>
              <span className={sheet.relationChipName}>{titles[id] || untitledPageDisplayTitle("page")}</span>
            </span>
          </span>
        );
      })}
    </span>
  );
}

/** Files as the cell's chips: the file's icon and its name. */
function HistoryFiles({ value }: { value: unknown }) {
  const files = normalizeFileAttachments(value);
  if (files.length === 0) return null;
  return (
    <span className={styles.chips} data-history-files>
      {files.map((file) => (
        <span key={file.id} className={sheet.fileChip}>
          <span className={sheet.fileChipLink}>
            <span className={sheet.fileThumb}><FileText size="regular" aria-hidden="true" /></span>
            <span className={sheet.fileName}>{file.name}</span>
          </span>
        </span>
      ))}
    </span>
  );
}

/** A link's value as the cell shows it, over a faint line; a URL short, as the property shows it. */
function HistoryLink({ property, value }: { property: PageHistoryProperty; value: string }) {
  const display = useUrlDisplay(property as { config?: { showFullUrl?: unknown } | null });
  if (!value.trim()) return null;
  const short = property.type === "url" && !display.full;
  const parts = short ? shortUrlParts(value) : null;
  const words = parts ? <>{parts.host}{parts.path ? <span className={sheet.urlPath}>{parts.path}</span> : null}</> : value;
  return (
    <span className={sheet.linkValue} data-url-short={short ? "" : undefined}>
      {propertyLink(property.type as DbProperty["type"], value) ? <span className={sheet.linkText}>{words}</span> : words}
    </span>
  );
}
