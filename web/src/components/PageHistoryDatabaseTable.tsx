import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import type { PageHistoryRowMark } from "@/lib/pageHistoryChanges";
import type { PageHistoryDatabase, PageHistoryDatabaseRow } from "@/pageHistoryWire.generated";
import type { Page } from "@/lib/types";
import { cardShowsPageIcon } from "./database/CardTitleIcon";
import { PropertyIcon } from "./database/PropertyIcon";
import { tableColumnWidth } from "./database/tableViewModel";
import { PageIconGlyph } from "./page/PageIcon";
import { PageHistoryCellValue } from "./PageHistoryCellValue";
import styles from "./PageHistoryDatabaseTable.module.css";

/**
 * A database's table in a version's preview, as Notion's (seen 2026-10-01,
 * 2026-11-07): the properties the version kept, each head with its icon, at
 * the widths its first view gives them (a title 280, the others 200) and an
 * empty column on to the preview's edge; its first rows with the values they
 * had, each named with its page's icon and each value drawn as the table's
 * cell draws it (PageHistoryCellValue). Read only, with
 * what the version changed in its rows marked when given.
 */
export function PageHistoryDatabaseTable({ database, marks, renamed, changeColor, workspaceId }: {
  database: PageHistoryDatabase;
  marks?: readonly PageHistoryRowMark[];
  /** The properties the version renamed, whose column heads are marked. */
  renamed?: ReadonlySet<string>;
  changeColor?: string;
  /** The workspace the rows' own picture icons are kept in. */
  workspaceId?: string;
}) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const properties = database.properties ?? [];
  if (properties.length === 0) return null;
  const titleId = properties.find((property) => property.type === "title")?.id;
  const rows = marks ?? (database.rows ?? []).map((row): PageHistoryRowMark => ({ row }));
  const viewConfig = { propertyWidths: database.views?.[0]?.propertyWidths };
  const widths = properties.map((property) => tableColumnWidth(viewConfig, property.id, property.type));
  const style = {
    "--history-table-width": `${widths.reduce((sum, width) => sum + width, 0)}px`,
    ...(changeColor ? { "--history-change-color": changeColor } : {}),
  } as CSSProperties;
  return (
    <div className={styles.scroll} data-page-history-database-table={database.blockId} style={style}>
      <table className={styles.table}>
        <colgroup>
          {widths.map((width, index) => <col key={properties[index].id} style={{ width }} />)}
          <col />
        </colgroup>
        <thead>
          <tr>
            {properties.map((property) => (
              <th key={property.id} scope="col" data-history-head-changed={renamed?.has(property.id) ? "true" : undefined}>
                <span className={styles.head}>
                  <span className={styles.headIcon} aria-hidden="true"><PropertyIcon property={property} size="small" /></span>
                  <span className={styles.headName}>{property.name}</span>
                </span>
              </th>
            ))}
            <th aria-hidden="true" />
          </tr>
        </thead>
        <tbody>
          {rows.map(({ row, change, changed }) => (
            <tr key={row.id} data-history-row={row.id} data-history-row-change={change}>
              {properties.map((property) => (
                <td key={property.id} data-history-cell-changed={changed?.has(property.id) ? "true" : undefined}>
                  {property.type === "title" ? <RowTitle row={row} titleId={property.id} workspaceId={workspaceId} untitled={t("common:labels.untitled")} />
                    : <PageHistoryCellValue property={property} cell={row.values[property.id]}
                      version={{ id: row.id, title: String(row.values[titleId ?? ""]?.value ?? ""), values: row.values, properties }} />}
                </td>
              ))}
              <td aria-hidden="true" />
            </tr>
          ))}
        </tbody>
      </table>
      {database.hasMoreRows ? <div className={styles.more}>{t("updatesPanel:versions.moreDatabaseRows")}</div> : null}
    </div>
  );
}

/**
 * A row's name with its page's icon before it as the table shows it: its
 * own icon, else a page's (with lines when it holds anything); an untitled
 * row shows one only with an icon of its own or content.
 */
function RowTitle({ row, titleId, workspaceId, untitled }: {
  row: PageHistoryDatabaseRow;
  titleId: string;
  workspaceId?: string;
  untitled: string;
}) {
  const text = String(row.values[titleId]?.value ?? "").trim();
  const page = {
    icon: row.icon ?? "", iconType: (row.iconType || "none") as Page["iconType"], workspaceId, hasContent: row.hasContent === true,
  };
  return (
    <span className={styles.title}>
      {text || cardShowsPageIcon(page) ? (
        <span className={styles.titleIcon} aria-hidden="true" data-history-row-icon>
          <PageIconGlyph page={page} size={18} fallback="file" />
        </span>
      ) : null}
      <span className={styles.titleText}>{text || untitled}</span>
    </span>
  );
}
