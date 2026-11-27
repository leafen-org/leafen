import { SwitchInput } from "@/components/Switch";
import { type ReactNode, useId, useRef } from "react";
import { Trans, useTranslation } from "react-i18next";
import { ArchiveIcon, ArrowBendUpLeftIcon, CommentIcon, DatabaseCylinderIcon, FileText, ListIcon, TableIcon, TitlePropertyIcon, X } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import type { PageHistoryDatabaseKeeps } from "@/lib/pageHistoryService";
import type { PageHistoryDatabase } from "@/pageHistoryWire.generated";
import type { Page } from "@/types";
import { PageHistoryRestoreCards } from "./PageHistoryRestoreCards";
import styles from "./PageHistoryRestorePanel.module.css";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";

// Notion lists its comments and reactions as restored, and its restore
// removes those added after the version; Leafen's keeps every comment and
// reaction, so they are listed as what stays.
const restoredItems = [
  { key: "content", icon: FileText },
  { key: "moved", icon: ArrowBendUpLeftIcon },
  { key: "metadata", icon: ListIcon },
] as const;

const keptItems = [{ key: "comments", icon: CommentIcon }] as const;

function RestoreItems({ items }: { items: readonly { key: string; icon: typeof FileText }[] }) {
  const { t } = useTranslation("updatesPanel");
  return (
    <ul className={styles.historyRestoreItems}>
      {items.map(({ key, icon: Icon }) => (
        <li key={key} className={styles.historyRestoreItem}>
          <Icon size="regular" aria-hidden="true" />
          <span>
            <span className={styles.historyRestoreItemName}>{t(`updatesPanel:versions.restoreItems.${key}`)}</span>
            <span className={styles.historyRestoreItemHint}>{t(`updatesPanel:versions.restoreItems.${key}Hint`)}</span>
          </span>
        </li>
      ))}
    </ul>
  );
}

// A full-page database's panel, as Notion's (seen 2026-11-05): all three
// switches on at first, and a row's own page restored from its own history;
// the rows as an archive box and the note under a question mark in a circle
// (seen 2026-11-06).
const databaseRestoredItems = [
  { key: "databaseMetadata", icon: TitlePropertyIcon },
  { key: "databaseProperties", icon: ListIcon },
  { key: "databaseViews", icon: TableIcon },
  { key: "databaseRows", icon: ArchiveIcon },
  { key: "databaseMoved", icon: ArrowBendUpLeftIcon },
] as const;

const databaseKeepSwitches = [
  { key: "keepDatabaseProperties", label: "restoreDatabaseProperties" },
  { key: "keepRowValues", label: "restoreRowValues" },
  { key: "keepRowNames", label: "restoreRowNames" },
] as const;

/**
 * What "복원 시작" opens over the version history, as Notion's (seen
 * 2026-09-30, the option 2026-10-01): the page and time it goes back to,
 * what the restore brings back and what it leaves as it is (the comments and
 * reactions), for a version whose databases read otherwise
 * now the option to restore them too (off at first) over their names, which it keeps as they
 * are or restores, and "이 버전 복원" under the page now and the version as
 * stacked cards (2026-10-01) and over a note that no version is lost; the
 * version itself beside them.
 */
export function PageHistoryRestorePanel({
  pageTitle, timeLabel, page, versionIcon, currentTime, databases, restoreDatabases, onRestoreDatabasesChange,
  fullPageDatabase, databaseKeeps, onDatabaseKeepsChange, restoring, error, onRestore, onClose, children,
}: {
  pageTitle: string;
  timeLabel: string;
  /** The page as it is now, and when its current version was made, for the front card. */
  page: Pick<Page, "icon" | "iconType" | "title" | "workspaceId" | "kind" | "viewBlock">;
  currentTime: string;
  /** The version's icon, for the cards behind. */
  versionIcon: Pick<Page, "icon" | "iconType">;
  databases: readonly PageHistoryDatabase[];
  restoreDatabases: boolean;
  onRestoreDatabasesChange: (restore: boolean) => void;
  /** The page is itself a database, which the restore brings back with the switches below. */
  fullPageDatabase: boolean;
  databaseKeeps: PageHistoryDatabaseKeeps;
  onDatabaseKeepsChange: (keeps: PageHistoryDatabaseKeeps) => void;
  restoring: boolean;
  error: string | null;
  onRestore: () => void;
  onClose: () => void;
  children: ReactNode;
}) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const titleId = useId();
  // The option is for the databases the restore would change; one the
  // version left as it is now is only kept, as Notion's (seen 2026-11-07). A
  // linked view is neither counted nor named among them, as Notion's (seen
  // 2026-11-25 twice): it stays as it is now without them.
  const countable = databases.filter((database) => !database.linked);
  const changed = countable.filter((database) => !database.unchanged);
  const panelRef = useRef<HTMLDivElement>(null);
  const restoreRef = useRef<HTMLButtonElement>(null);

  // Escape closes it, and only it, once nothing opened over it takes the
  // key; the focus goes back to what opened it. Opened, it leaves the focus
  // on "복원 시작" behind it, a typed letter changing nothing on screen, as
  // Notion's (measured twice on 2026-11-15 in Chrome 1440 in different tabs,
  // no script before the press, the screen and one typed letter after it) —
  // so it shuts nothing of the history window behind it from the keys, and
  // Tab goes on there ("타임라인 닫기" next), as Notion's (seen 2026-11-26 in
  // two tabs): no modal of its own, the history window's Tab ring holding.
  const { zIndex } = useOverlayLayer(true, { kind: "alertdialog", ref: panelRef, modalOwned: () => [panelRef.current?.parentElement], onEscape: onClose,
    initialFocus: "opener", modal: false, restoreFocus: true });

  return (
    <div className={styles.historyRestoreLayer} style={{ zIndex }}>
      <div className={styles.historyRestoreBackdrop} onClick={onClose} aria-hidden="true" />
      <div
        ref={panelRef}
        className={styles.historyRestorePanel}
        role="dialog"
        aria-labelledby={titleId}
        data-page-history-restore="true"

      >
        <section className={styles.historyRestoreSide}>
          {/* The page and time stay put over what scrolls, as Notion's (measured 2026-11-07). */}
          <header className={styles.historyRestoreHead}>
            <h2 id={titleId} className={styles.historyRestoreTitle}>
              <Trans t={t} i18nKey="updatesPanel:versions.restorePage"
                values={{ title: pageTitle || untitledPageDisplayTitle(page) }}
                components={{ name: <strong /> }} />
            </h2>
            <p className={styles.historyRestoreTime}>
              <Trans t={t} i18nKey="updatesPanel:versions.restoreTo" values={{ time: timeLabel }}
                components={{ time: <strong /> }} />
            </p>
          </header>
          <div className={`${styles.historyRestoreScroll} nscroll`}>
            <div className={styles.historyRestoreScrollBody}>
            <div>
            <h3 className={`${styles.historyRestoreHeading} ${styles.historyRestoreHeadingFirst}`}>{t("updatesPanel:versions.restoredItems")}</h3>
            <RestoreItems items={fullPageDatabase ? databaseRestoredItems : restoredItems} />
            {fullPageDatabase ? (databases.length > 0 ? (
              <>
                <h3 className={styles.historyRestoreHeading}>{t("updatesPanel:versions.restoreOptions")}</h3>
                {databaseKeepSwitches.map(({ key, label }) => (
                  <label key={key} className={`${styles.historyRestoreOption} ${styles.historyRestoreSwitch}`}>
                    <span className={styles.historyRestoreOptionText}>
                      <span className={styles.historyRestoreOptionQuestion}>{t(`updatesPanel:versions.${label}`)}</span>
                    </span>
                    <SwitchInput
                      checked={!databaseKeeps[key]}
                      disabled={restoring}
                      onChange={(event) => onDatabaseKeepsChange({
                        ...databaseKeeps, [key]: event.currentTarget.checked ? undefined : true,
                      })}
                    />
                  </label>
                ))}
                <p className={styles.historyRestoreRowPagesNote}>
                  <QuestionCircleIcon size="small" aria-hidden="true" />
                  {t("updatesPanel:versions.restoreRowPagesNote")}
                </p>
              </>
            ) : null) : (
              <>
                {changed.length > 0 ? <>
                <h3 className={styles.historyRestoreHeading}>{t("updatesPanel:versions.restoreOptions")}</h3>
                <label className={styles.historyRestoreOption}>
                  <span className={styles.historyRestoreOptionText}>
                    <span className={styles.historyRestoreOptionQuestion}>
                      {t("updatesPanel:versions.restoreDatabases", { count: changed.length })}
                    </span>
                    <span className={styles.historyRestoreOptionHint}>{t("updatesPanel:versions.restoreDatabasesHint")}</span>
                  </span>
                  <SwitchInput
                   
                    checked={restoreDatabases}
                    disabled={restoring}
                    data-page-history-restore-databases="true"
                    onChange={(event) => onRestoreDatabasesChange(event.currentTarget.checked)}
                  />
                </label>
                </> : null}
                {/* Kept, all of them; restored, the version's go back and those
                    made since leave the page, as Notion's lists them (seen
                    2026-11-06), and those the restore would not change stay.
                    What stays always holds the comments and reactions, over
                    any databases kept. */}
                {(restoreDatabases && changed.length > 0
                  ? [["restored", changed.filter((database) => !database.later)], ["removed", changed.filter((database) => database.later)],
                    ["kept", countable.filter((database) => database.unchanged)]] as const
                  : [["kept", countable]] as const
                ).filter(([kind, listed]) => kind === "kept" || listed.length > 0).map(([kind, listed]) => (
                  <section key={kind} role="group" aria-label={t(`updatesPanel:versions.${kind}Items`)} data-page-history-restore-group={kind}>
                    <h3 className={styles.historyRestoreHeading}>{t(`updatesPanel:versions.${kind}Items`)}</h3>
                    {kind === "removed" ? <p className={styles.historyRestoreOptionHint}>{t("updatesPanel:versions.removedItemsHint")}</p> : null}
                    {kind === "kept" ? <RestoreItems items={keptItems} /> : null}
                    {listed.length > 0 ? <ul className={styles.historyRestoreDatabases} data-page-history-databases={kind}>
                      {listed.map((database) => (
                        <li key={database.blockId}>
                          <DatabaseCylinderIcon size="regular" aria-hidden="true" />
                          <span>{plainTitleOf(database) || untitledPageDisplayTitle("database")}</span>
                        </li>
                      ))}
                    </ul> : null}
                  </section>
                ))}
              </>
            )}
            </div>
            {/* The cards close what scrolls, at its foot when it is short and
                under the databases when they are listed, as Notion's (seen
                2026-11-06). */}
            <div className={styles.historyRestoreCardsSlot}>
              <PageHistoryRestoreCards
                current={{ page, title: plainTitleOf(page) || untitledPageDisplayTitle(page), time: currentTime }}
                version={{ page: { ...versionIcon, workspaceId: page.workspaceId }, title: pageTitle || untitledPageDisplayTitle(page), time: timeLabel }} />
            </div>
            </div>
          </div>
          <div className={styles.historyRestoreFooter}>
            {error ? <p className={styles.historyRestoreError} role="alert">{error}</p> : null}
            <button
              ref={restoreRef}
              type="button"
              className={styles.historyRestoreButton}
              disabled={restoring}
              onClick={onRestore}
            >
              {restoring ? t("updatesPanel:versions.restoring") : t("updatesPanel:versions.restoreThisVersion")}
            </button>
            <p className={styles.historyRestoreNote}>{t("updatesPanel:versions.restoreNote")}</p>
          </div>
        </section>
        <div className={`${styles.historyRestorePreview} nscroll`}>{children}</div>
        <button
          type="button"
          className={styles.historyRestoreClose}
          onClick={onClose}
          aria-label={t("updatesPanel:versions.closeRestore")}
        >
          <X size="regular" />
        </button>
      </div>
    </div>
  );
}
