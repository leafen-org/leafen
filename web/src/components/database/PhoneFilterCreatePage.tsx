import { Plus } from "@/icons/leafen";
import type { Page } from "@/lib/types";
import { PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { RelationCreateWords } from "./PropertyRelationCellView";
import styles from "./DatabasePhoneFilterValues.module.css";

/**
 * What a relation filter's search that finds no page shows where its list
 * was: a group holding one row, its "+" 22 in the icons' quieter colour and
 * "⛁ T에 새 〈words〉 페이지 생성" in the relation picker's words; pressed, the
 * page is made in the related database under those words and chosen (Notion's
 * phone, seen 2026-11-17 on iPhone Safari at 402: a 45 row, the "+" 12 in,
 * the words 10 after it at 17px).
 */
export function PhoneFilterCreatePage({ words, database, onPress }: { words: string; database: Page; onPress(): void }) {
  return (
    <PhoneSheetGroup>
      <PhoneSheetRow icon={<Plus size="regular" className={styles.createPlus} />} more={false} onPress={onPress}
        label={<RelationCreateWords title={words} database={database} phone />} data={{ "data-filter-create-page": "" }} />
    </PhoneSheetGroup>
  );
}
