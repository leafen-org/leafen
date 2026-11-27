import type { ListState } from "react-stately";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import type { Place } from "@/lib/pagePath";
import { DataSourceMark } from "./database/DataSourceMark";
import { PlaceLine } from "./PlaceLine";
import type { MoveDatabaseResult } from "./useMoveSearches";
import styles from "./MoveToDialog.module.css";

/**
 * A database a move search found, standing among its pages
 * (rankedAmongPages): its source's mark, as Notion's (seen 2026-11-11,
 * 2026-11-15: "🍎 사과 DB"), its name, and where it is under it as a page's
 * result says it (seen 2026-11-10), named by both (rowNameProps); none
 * given, its name alone.
 */
export function MoveDatabaseOption<T>({ database, location, state, itemKey }: {
  database: Pick<MoveDatabaseResult, "id" | "title" | "mark">;
  location: Place;
  state: ListState<T>;
  itemKey: string;
}) {
  return (
    <NativePickerOption state={state} itemKey={itemKey} className={styles.result}
      data-move-database={database.id} data-placed={location.length ? "true" : undefined}>{({ labelProps, placeProps }) => <>
        <span className={styles.icon}><DataSourceMark source={database.mark ?? {}} /></span>
        <span className={styles.text}><span {...labelProps}>{database.title}</span>
          {location.length ? <span {...placeProps}><PlaceLine place={location} /></span> : null}</span>
      </>}
    </NativePickerOption>
  );
}
