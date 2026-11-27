import type { ReactNode } from "react";
import { SettingsSectionIcon } from "./settingsWindowLabels";
import type { SettingsSearchResult } from "./settingsWindowSearch";
import type { SettingsSection } from "./settingsWindowModel";
import styles from "./SettingsWindow.module.css";

/**
 * The settings search's rows under their sections, as Notion lists them: a
 * section's line opens the section at its top, a row opens it at that row
 * (seen 2026-11-07). One that finds nothing says so 12px down and 8px in,
 * 14px/20px in the second text color, wrapping (seen 2026-11-17 at 1440).
 */
export function SettingsSearchResults({ results, noResults, accountIcon, onOpen }: {
  results: SettingsSearchResult[];
  noResults: string;
  /** The account's section shows the person, as the list does. */
  accountIcon: ReactNode;
  /** Opens a section, at the first of `rows` it draws. */
  onOpen: (section: SettingsSection, rows?: string[]) => void;
}) {
  return (
    <div className={styles.results} data-settings-search-results>
      {results.length === 0 ? <div className={styles.noResults} data-settings-search-none>{noResults}</div> : null}
      {results.map((result) => (
        <div key={result.section} className={styles.resultGroup}>
          <button type="button" className={styles.resultPath} data-settings-search-section={result.section} onClick={() => onOpen(result.section)}>
            {result.section === "account" ? accountIcon : <SettingsSectionIcon section={result.section} />}{result.path}
          </button>
          {result.rows.map((row, index) => (
            <button
              key={index}
              type="button"
              className={styles.result}
              data-settings-search-result={result.section}
              onClick={() => onOpen(result.section, [row.title, ...row.drawnBy])}
            >
              <strong>{row.title}</strong>
              {row.description ? <span>{row.description}</span> : null}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}
