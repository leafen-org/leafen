import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { TopBar } from "@/components/page/TopBar";
import { TopBarLock } from "@/components/page/TopBarLock";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import type { DatabaseLockControls } from "@/components/database/databaseStructureLock";
import { useProductPages } from "@/components/product/productPagesContext";
import { CheckboxIcon, Star, StarFilled, TaskPileIcon } from "@/icons/leafen";
import { setDocumentChrome } from "@/lib/documentChrome";
import { ADD_TO_PEEK_PARAM, pagePeekHref } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import { MyTasksDatabaseView } from "./MyTasksDatabaseView";
import { useMyTaskActions } from "./useMyTaskActions";
import { useMyTasksRead } from "./useMyTasksRead";
import type { Session } from "@/types";
import styles from "./MyTasksPage.module.css";

/**
 * "내 작업", as Notion's page (seen 2026-11-02, 2026-11-05): the tasks
 * assigned to you in the task databases you pick as its sources (at most
 * ten), in a database view of their own (MyTasksDatabaseView), and "새 작업"
 * asking which source it goes in (useMyTaskActions). Until you pick a source
 * the view's tools stay, "새 작업" dim, over what it shows and a link to pick
 * them (seen 2026-11-08).
 */
// A task opens beside this page, its database named in the peek's bar to move it to another.
const myTaskHref = (rowId: string) => `${pagePeekHref(rowId)}&${ADD_TO_PEEK_PARAM}=true`;

export default function MyTasksPage() {
  const pages = useProductPages();
  const session = pages?.session;
  const workspaceId = pages?.workspaceId;
  const read = useMyTasksRead(session, workspaceId);
  if (!session || !workspaceId) return null;
  return <MyTasksPageBody session={session} workspaceId={workspaceId} read={read} />;
}

function MyTasksPageBody({ session, workspaceId, read }: { session: Session; workspaceId: string; read: ReturnType<typeof useMyTasksRead> }) {
  const { t } = useTranslation("myTasks");
  const [lock, setLock] = useState<DatabaseLockControls | null>(null);
  const [favorite, setFavorite] = useState<{ on: boolean; change(next: boolean): void } | null>(null);
  const onFavorite = useCallback((on: boolean, change: (next: boolean) => void) => setFavorite({ on, change }), []);
  const actions = useMyTaskActions({ session, workspaceId, read, openHref: myTaskHref });
  const { sources, tasks, failed } = read;

  useEffect(() => { setDocumentChrome({ title: t("title") }); }, [t]);

  // With no source the page's own empty screen under the view stands alone, no group heads nor "no results" over it,
  // grouped or not (Notion's, seen 2026-11-16 grouped by status with no source).
  const noSourceRows = () => null;
  return (
    <>
      <TopBar title={t("title")} crumbs={lock?.locked ? <TopBarLock unlockedForMe={lock.unlockedForMe} onLockAgain={lock.relock}
        onUnlockForMe={lock.unlockForMe} onUnlockForEveryone={lock.unlockForEveryone} /> : <></>} screenActions={favorite ? (
        <button type="button" className={topBarStyles.iconAction} aria-label={t("favorite")} title={t("favorite")} aria-pressed={favorite.on} data-favorite={favorite.on ? "true" : undefined}
          data-my-tasks-favorite onClick={() => favorite.change(!favorite.on)}>
          {favorite.on ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
        </button>
      ) : null} />
      <main className={styles.page} data-my-tasks-page>
        <h1 className={styles.title}>{t("title")}</h1>
        {!sources || !tasks ? <div className={styles.bar}><span className={styles.viewTab}><CheckboxIcon size="small" aria-hidden="true" />{t("viewName")}</span><div className={styles.tools}>{actions.newTaskButton}</div></div> : null}
        {failed ? (
          <div className={styles.empty} role="alert">
            <p>{t("failed")}</p>
            <button type="button" className={styles.emptyButton} onClick={read.reload}>{t("tryAgain")}</button>
          </div>
        ) : !sources || !tasks ? (
          <p className={styles.note} role="status">{t("loading")}</p>
        ) : (
          <>
            <MyTasksDatabaseView session={session} workspaceId={workspaceId} tasks={tasks} sources={actions.picked}
              newRowControl={actions.newTaskButton} emptyRows={actions.empty ? noSourceRows : actions.noMatchingTasks} sourceSettings={actions.sourceSettings} settingsFoot={actions.settingsFoot} onLockControls={setLock} onFavorite={onFavorite} onOpenRow={(rowId) => pushRoute(myTaskHref(rowId))}
              onChanged={read.reload} onTaskMade={read.taskMade} />
            {actions.empty ? (
              <div className={styles.empty} data-my-tasks-empty="sources">
                <TaskPileIcon size="empty" className={styles.emptyArt} aria-hidden="true" />
                <p>{t("emptySources")}</p>
                <button type="button" className={styles.emptyLink} onClick={actions.openSources} data-my-tasks-configure>{t("configureSources")}</button>
              </div>
            ) : null}
          </>
        )}
      </main>
      {actions.dialogs}
    </>
  );
}
