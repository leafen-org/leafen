import { createContext, type CSSProperties, useContext } from "react";
import type { PageHistorySnapshotResponse } from "@/pageHistoryWire.generated";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import type { DbProperty, Page } from "@/lib/types";
import { useProductPages } from "@/components/product/productPagesContext";
import { nativeBoardQueryBounds } from "./database/boardViewModel";
import { evaluateFormula, formulaDisplay } from "./database/formula";
import { PersonBadge } from "./database/PersonBadge";
import { normalizePersonIds } from "./database/people";
import { chipStyle } from "./database/colors";
import sheet from "./database/databaseSheet";
import { formatDateForProperty, formatTimestampForProperty } from "./database/dateUtils";
import { formatNumberForProperty } from "./database/numberFormat";
import { statusValueOrDefault } from "@/lib/statusDefaultValue";
import { verificationPropertySummary, verificationPropertyValue } from "./database/structuredPropertyValues";
import { useTranslation } from "react-i18next";
import { PropertyIcon } from "./database/PropertyIcon";
import { PageHistoryCellValue } from "./PageHistoryCellValue";
import styles from "./PageHistoryProperties.module.css";
import { pagesByIdOf } from "@/lib/pageIndex";

type HistoryValues = NonNullable<PageHistorySnapshotResponse["values"]>;

/** A property of the database page whose versions the history shows. */
export type PageHistoryProperty = { id: string; name: string; type: string; config?: Record<string, unknown> };

/** A version's value as text: names, dates and text, joined. */
export function readablePageHistoryValue(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string" || typeof value === "number") return String(value);
  if (typeof value === "boolean") return value ? "✓" : "";
  if (Array.isArray(value)) return value.map(readablePageHistoryValue).filter(Boolean).join(", ");
  if (typeof value === "object") {
    const object = value as Record<string, unknown>;
    for (const key of ["name", "plainText", "text", "title", "start"]) {
      if (key in object) {
        const text = readablePageHistoryValue(object[key]);
        if (key === "start" && object.end) return `${text} → ${readablePageHistoryValue(object.end)}`;
        return text;
      }
    }
  }
  return "";
}

type HistoryOption = { id: string; name: string; color?: string };

/** A version of a database page, whose formulas are worked out from the values it held then. */
export type HistoryVersion = { id: string; title: string; values: Record<string, { value: unknown } | undefined>;
  properties: readonly PageHistoryProperty[] };

/**
 * A formula's value in a version, as Notion's version preview shows it (seen
 * 2026-10-04): worked out from the page's values in that version, not today's.
 */
function historyFormulaText(property: PageHistoryProperty, version: HistoryVersion, pagesById: Record<string, Page>) {
  const properties: Record<string, unknown> = {};
  for (const [id, cell] of Object.entries(version.values)) properties[id] = cell?.value;
  const row = { id: version.id, title: version.title, properties } as unknown as Page;
  // The history's properties are a database's or a graph's; formulas read them as a database's.
  const asDatabase = (from: PageHistoryProperty) => {
    const source = from as PageHistoryProperty & { dataSourceId?: string; databaseId?: string };
    return { ...from, databaseId: source.databaseId ?? source.dataSourceId ?? "" } as unknown as DbProperty;
  };
  const props = version.properties.map(asDatabase);
  const prop = asDatabase(property);
  const bounds = nativeBoardQueryBounds.formula;
  try {
    const shown = formulaDisplay(row, prop, bounds, "", () => evaluateFormula({ row, prop, props, pagesById, bounds }));
    return shown.checked === undefined ? shown.text : shown.checked ? "✓" : "";
  } catch {
    return "";
  }
}

/**
 * A property's value in a version as its cell shows it, as Notion's version
 * preview shows them: options as their coloured chips, dates, times and
 * numbers in the property's formats.
 */
/** The rows a version's relations point at, by name, as the server read them for this reader. */
export const PageHistoryRelationTitles = createContext<Readonly<Record<string, string>>>({});

export function HistoryValue({ property, value, version }: {
  property: PageHistoryProperty; value: unknown; version?: HistoryVersion;
}) {
  const config = property.config as DbProperty["config"];
  const pages = useProductPages()?.pages;
  const relationTitles = useContext(PageHistoryRelationTitles);
  const { t } = useTranslation(["tableView", "topBar"]);
  // A verification reads as its page's value says it (PropValue): "인증 완료", until when, or expired.
  if (property.type === "verification") {
    return verificationPropertySummary(value, { verified: t("tableView:verified"), expired: t("topBar:expired"),
      verifiedUntil: (date) => t("tableView:verifiedUntil", { date }) });
  }
  if (property.type === "relation") {
    const ids = Array.isArray(value) ? value.map(String) : [];
    return ids.filter((id) => id in relationTitles)
      .map((id) => relationTitles[id] || untitledPageDisplayTitle("page")).join(", ");
  }
  if (property.type === "formula" && version) {
    // The pages a formula names are read as the page list knows them.
    return historyFormulaText(property, version,
      pagesByIdOf(pages ?? []) as unknown as Record<string, Page>);
  }
  // A status that set none shows its default, as the row's page does (statusValueOrDefault).
  if (property.type === "status") value = statusValueOrDefault(config, value);
  if (property.type === "select" || property.type === "status" || property.type === "multi_select") {
    const ids = (Array.isArray(value) ? value : value ? [value] : []).map((item) =>
      item && typeof item === "object" ? String((item as { id?: unknown }).id ?? "") : String(item));
    const options = (config?.options ?? []) as HistoryOption[];
    return ids.map((id) => options.find((option) => option.id === id)).filter((option): option is HistoryOption => Boolean(option))
      .map((option) => <span key={option.id} className={sheet.chip} style={chipStyle(option.color)}><span className={sheet.chipText}>{option.name}</span></span>);
  }
  // People by name, as the cells show them, not by their ids; a page's maker
  // and last editor too, which the version's preview reads from its versions
  // (page_history_automatic_values.go).
  if (property.type === "person" || property.type === "created_by" || property.type === "last_edited_by") {
    const rollup = nativeBoardQueryBounds.formula.rollup;
    const people = [...new Set(normalizePersonIds(value, {
      maxNodes: rollup.maxPersonNodes, maxDepth: rollup.maxPersonDepth, maxEntries: rollup.maxPeopleEntries,
    }))];
    return people.map((id, index) => <span key={id}>{index > 0 ? ", " : null}<PersonBadge id={id} /></span>);
  }
  if (property.type === "date") return formatDateForProperty(value, { config });
  if (property.type === "created_time" || property.type === "last_edited_time") return formatTimestampForProperty(value, { config });
  if (property.type === "number") return formatNumberForProperty(value, { config: property.config ?? {} });
  return readablePageHistoryValue(value);
}

/**
 * A database page version's properties, each with the value it had then, as
 * Notion's version preview lists them as the row's page does (measured
 * 2026-11-15): each name with its icon, an empty value saying so, the
 * values as their cells draw them (PageHistoryCellValue).
 */
export function PageHistoryProperties({ properties, values, pageId, title, changed, changeColor, only }: {
  properties: readonly PageHistoryProperty[];
  values: HistoryValues;
  pageId: string;
  title: string;
  /** The properties whose values the version changed: each value is a change, outlined while the stepper is on it. */
  changed?: ReadonlySet<string>;
  changeColor?: string;
  /** These alone, without the line under the title's properties: a wiki page's others under "⋯". */
  only?: ReadonlySet<string>;
}) {
  const { t } = useTranslation("rowProperties");
  const version: HistoryVersion = { id: pageId, title, values, properties };
  const shown = properties.filter((property) => property.type !== "title" && (!only || only.has(property.id)));
  if (shown.length === 0) return null;
  return (
    <dl className={styles.list} data-page-history-properties data-bare={only ? "true" : undefined}>
      {shown.map((property) => (
        <div key={property.id} className={styles.property}>
          <dt className={styles.name}>
            <span className={styles.icon} aria-hidden="true"><PropertyIcon property={property} size="small" /></span>
            <span className={styles.nameText}>{property.name}</span>
          </dt>
          <dd className={styles.value} data-history-changed={changed?.has(property.id) ? "true" : undefined}
            style={changed?.has(property.id) && changeColor ? { "--history-change-color": changeColor } as CSSProperties : undefined}>
            {pageHistoryValueEmpty(property, values[property.id]?.value)
              ? <span className={styles.empty}>{t("empty")}</span>
              : <PageHistoryCellValue property={property} cell={values[property.id]} version={version} />}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * A value that holds nothing, which says "비어 있음"; a box shows as itself,
 * ticked or not; an unverified page's verification is empty, as its page's (isEmptyRowProperty).
 */
export function pageHistoryValueEmpty(property: PageHistoryProperty, value: unknown) {
  if (property.type === "checkbox" || property.type === "formula" || property.type === "status") return false;
  if (property.type === "verification") return (verificationPropertyValue(value)?.state ?? "unverified") === "unverified";
  return value === null || value === undefined || value === "" || (Array.isArray(value) && value.length === 0);
}
