import { i18next } from "@/i18n";
import type { GraphDataSource } from "@/contentGraph";
import { hasExplicitPageIcon } from "@/components/page/PageIcon";
import type { DataSourceIcon } from "@/lib/nativeDatabaseViewProductAdapter";
import { inlineDatabasePlaceholderTitle } from "@/components/editor/databaseTitles";
import type { TextSpan } from "@/lib/types";
import { DatabaseTitleField, DatabaseTitleText } from "./DatabaseTitleField";
import { DatabaseTitleIcon } from "./DatabaseTitleIcon";
import styles from "./InlineDatabasePeekTitle.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * An inline database opened alone in a peek is titled by its name, as
 * Notion's side peek shows it like a page (seen 2026-10-10): its icon, then
 * its name on the same line, as on its own page (InlineDatabasePageTitle;
 * Notion's, seen 2026-11-09: a 36px icon box over its views' tabs, an emoji
 * or built-in icon filling it, a picture 32px in it, the 32px name 8px
 * after it), changed in place as its own title is, rich text as it is
 * (DatabaseTitleField). Enter and Escape leave it. A view block — a
 * dashboard, a link still waiting for its source — shows its icon alone, as
 * on its own page (seen 2026-11-09). Its icon opens its picker as a page's
 * (DatabaseTitleIcon).
 */
export function InlineDatabasePeekTitle({ source, workspaceId, pageId, readOnly, nameless = false, placeholder = inlineDatabasePlaceholderTitle(), onChange, onIconChange }: {
  source: GraphDataSource;
  workspaceId: string;
  /** The page a picture picked for its icon is kept with. */
  pageId: string;
  readOnly: boolean;
  nameless?: boolean;
  /** What it shows while untitled: "새 데이터베이스", or its sources' names (inlineDatabaseName). */
  placeholder?: string;
  onChange: (title: string, spans: TextSpan[]) => void;
  onIconChange: (icon: DataSourceIcon) => void;
}) {
  const icon = { icon: source.icon ?? "", iconType: source.iconType ?? "none", workspaceId };
  const spans = source.titleSpans as TextSpan[] | undefined;
  return (
    <div className={styles.header} data-inline-database-peek-title data-nameless={nameless ? "true" : undefined}>
      {hasExplicitPageIcon(icon) ? <DatabaseTitleIcon className={styles.icon} icon={icon} size={36} box={36}
        onIconChange={readOnly ? undefined : onIconChange} uploadTarget={{ workspaceId, pageId }}
        dataAttributes={{ "data-inline-database-peek-icon": "" }} /> : null}
      {nameless ? null : <h1 className={styles.heading}>
        {readOnly ? (
          <span className={styles.title}><DatabaseTitleText title={source.title} spans={spans} fallback={placeholder} /></span>
        ) : (
          <DatabaseTitleField className={styles.title} value={source.title} spans={spans} placeholder={placeholder}
            ariaLabel={i18next.t("databaseView:databaseTitle")} onTitleChange={onChange}
            onKeyDown={(event) => {
              if (!fieldEnter(event)) return undefined;
              (event.target as HTMLElement).blur();
              return true;
            }} />
        )}
      </h1>}
    </div>
  );
}
