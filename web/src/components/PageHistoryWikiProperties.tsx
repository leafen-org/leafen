import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import type { PageHistorySnapshotResponse } from "@/pageHistoryWire.generated";
import { PropertyIcon } from "./database/PropertyIcon";
import { WikiMore } from "./database/RowPageWikiProperties";
import barStyles from "./database/RowPageWikiProperties.module.css";
import { PageHistoryCellValue } from "./PageHistoryCellValue";
import { PageHistoryProperties, pageHistoryValueEmpty, type HistoryVersion, type PageHistoryProperty } from "./PageHistoryProperties";
import styles from "./PageHistoryWikiProperties.module.css";
import type { PageHistoryWikiBar } from "./usePageHistoryProperties";

type HistoryValues = NonNullable<PageHistorySnapshotResponse["values"]>;

/**
 * A wiki page version's properties as its page shows them, as Notion's
 * version preview draws them (seen 2026-11-18 at 1416, the same places and
 * sizes as the page's): the wiki's bar — Owner, Verification and Tags side
 * by side, each its icon and name over its value in the version, an empty
 * one "비어 있음" — drawn as the page's (RowPageWikiProperties), and "⋯"
 * opening its other properties, each name beside its value then, without
 * "속성 추가". A value the version changed is outlined as a change.
 */
export function PageHistoryWikiProperties({ wiki, properties, values, pageId, title, changed, changeColor }: {
  wiki: PageHistoryWikiBar;
  properties: readonly PageHistoryProperty[];
  values: HistoryValues;
  pageId: string;
  title: string;
  changed?: ReadonlySet<string>;
  changeColor?: string;
}) {
  const { t } = useTranslation("rowProperties");
  const version: HistoryVersion = { id: pageId, title, values, properties };
  const bar = wiki.bar.flatMap((id) => properties.filter((property) => property.id === id));
  const placed = new Set([...wiki.bar, ...wiki.hidden]);
  const others = new Set(properties.filter((property) => property.type !== "title" && !placed.has(property.id)).map(({ id }) => id));
  return (
    <div className={`${barStyles.bar} ${styles.bar}`} data-page-history-wiki-properties>
      {bar.map((property) => (
        <div key={property.id} className={barStyles.property} data-row-wiki-property={property.id}>
          <div className={barStyles.label}>
            <PropertyIcon property={property} size="small" />
            <span>{property.name}</span>
          </div>
          <div className={styles.value} data-history-changed={changed?.has(property.id) ? "true" : undefined}
            style={changed?.has(property.id) && changeColor ? { "--history-change-color": changeColor } as CSSProperties : undefined}>
            {pageHistoryValueEmpty(property, values[property.id]?.value)
              ? <span className={styles.empty}>{t("empty")}</span>
              : <span className={styles.text}><PageHistoryCellValue property={property} cell={values[property.id]} version={version} /></span>}
          </div>
        </div>
      ))}
      {others.size > 0 ? (
        <WikiMore label={t("wikiMore")}>
          <PageHistoryProperties only={others} properties={properties} values={values} pageId={pageId} title={title} changed={changed} changeColor={changeColor} />
        </WikiMore>
      ) : null}
    </div>
  );
}
