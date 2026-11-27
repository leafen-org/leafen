import { useTranslation } from "react-i18next";
import { usePageNames } from "@/lib/pageNames";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { InfoIcon } from "@/icons/leafen";
import { HoverTip } from "../IconTipButton";
import { DataSourceListRow } from "./DataSourceListRow";
import styles from "./RelationPropertyCreationDialog.module.css";

/**
 * A database "관계형 대상" offers, its mark, its name and where it sits
 * (DataSourceListRow: 28px alone, 45 with where it sits). The
 * database the relation is made in ends with an ⓘ whose tip says "이 데이터베이스",
 * so it is told from others of its name, as Notion's (seen 2026-11-16 at
 * 1440: a 16px circled "i" in the text's color, centred in the line 8px
 * from its end, the tip 6px after it, centred on it).
 */
export function RelationCreateTargetRow({ database, own, onChoose }: {
  database: RelationDatabaseOption;
  /** The database the relation is made in. */
  own: boolean;
  onChoose: (id: string) => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("propertyTypeConfig");
  const title = pageNames.title(database);
  return (
    <DataSourceListRow className={styles.relationCreateTarget} data-relation-target={database.id}
      aria-label={own ? t("thisDatabase", { name: title }) : title} onClick={() => onChoose(database.id)}
      source={database.mark ?? {}} name={title} place={database.place}
      end={own ? <HoverTip label={t("thisDatabaseTip")} placement="right" className={styles.relationCreateTargetOwn}>
        <InfoIcon size="small" aria-hidden="true" />
      </HoverTip> : null} />
  );
}

