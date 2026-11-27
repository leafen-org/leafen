import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { DatabaseViewSettingsItem } from "@/components/database/DatabaseViewSettingsPanel";
import { useProductPages } from "@/components/product/productPagesContext";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import { applyDatabaseStarter } from "@/databaseStarterApi";
import { saveMyTaskSources } from "@/databaseToolbarConfigApi";
import type { DatabaseMyTaskSource } from "@/databaseToolbarConfigWire.generated";
import { i18next } from "@/i18n";
import { DataPathIcon, Plus, SlidersIcon, TaskPileIcon } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { newId } from "@/lib/ids";
import { makeMyTask } from "@/lib/myTaskMadeHere";
import { createNativePage } from "@/lib/pageCreationService";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useProductToast } from "@/lib/productToast";
import { pushRoute, useSearchParams } from "@/lib/router";
import type { Session } from "@/types";
import { MyTaskPersonalDatabaseDialog } from "./MyTaskPersonalDatabaseDialog";
import { MyTaskDatabaseIcon, MyTaskSourcesDialog } from "./MyTaskSourcesDialog";
import { pickedMyTaskSources, type MyTasksRead } from "./useMyTasksRead";
import styles from "./MyTasksPage.module.css";

// A task with nothing given it but what "새 작업" gave: no name, icon, cover or body, and values only where they were put.
async function discardUntouched(session: Session, rowId: string) {
  const { row, body, properties } = await readDatabaseRowPage(session, rowId, { limit: 1 });
  const title = properties.find((property) => property.type === "title");
  const named = title && String(row.values[title.id]?.value ?? "").trim();
  if (named || row.icon || row.cover || body.some((block) => block.type !== "paragraph" || String(block.content.text ?? "").trim())) return;
  await createDatabaseRowPageService(session, () => {}).trash(rowId);
}

/**
 * What "내 작업" does wherever its view shows — its page, Home's widget:
 * "새 작업" asking which source it goes in, as Notion's (seen 2026-11-08:
 * the sources to add to, then "⇄ 작업 출처 ›" and "새 개인 페이지 작업
 * 데이터베이스" in grey, every row's words 8px after its 20px icon), the
 * sources window, the view settings' "작업 출처" and "작업이 보이지 않나요?",
 * and "일치하는 작업이 없습니다." with no task to show. A task opens by
 * `openHref`; one just made and left untouched goes as it closes, as Notion's
 * (seen 2026-11-05), and a task opened may have changed: the list is read
 * again as it closes.
 */
export function useMyTaskActions({ session, workspaceId, read, openHref, onSourcesSaved }: {
  session: Session; workspaceId: string; read: MyTasksRead; openHref(rowId: string): string;
  /** Told once a pick of sources is saved, for what else reads them (Home's "예정된 이벤트"). */
  onSourcesSaved?(): void;
}) {
  const { t } = useTranslation("myTasks");
  const pages = useProductPages();
  const toast = useProductToast();
  const [menuOpen, setMenuOpen] = useState(false);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const [naming, setNaming] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const { sources, setSources, reload, taskMade } = read;
  const peeked = useSearchParams().get("p");
  const wasPeeked = useRef(peeked);
  const fresh = useRef<string | null>(null);
  useEffect(() => {
    const closed = wasPeeked.current;
    wasPeeked.current = peeked;
    if (!closed || peeked) return;
    const untouched = fresh.current === closed;
    fresh.current = null;
    if (!untouched) { reload(); return; }
    void discardUntouched(session, closed).catch(() => false).finally(reload);
  }, [peeked, reload, session]);

  const picked = pickedMyTaskSources(sources);
  const sourceName = (source: { title: string }) => source.title || untitledPageDisplayTitle({ kind: "database", viewBlock: false });
  async function create(source: DatabaseMyTaskSource) {
    setMenuOpen(false);
    // It opens beside the list, its title taking what is typed, as Notion's.
    await holdTypingWhile(async () => {
      try {
        // Made on this device first, it opens at once however the network is (myTaskMadeHere.ts).
        const task = await makeMyTask(session, workspaceId, source);
        taskMade(task, true);
        fresh.current = task.rowId;
        requestPageTitleFocus(task.rowId);
        pushRoute(openHref(task.rowId));
      } catch {
        toast.notify(t("couldntCreate"), "error");
      }
    });
  }

  async function saveSources(ids: string[]) {
    setSources(await saveMyTaskSources(session, workspaceId, ids));
    reload();
    onSourcesSaved?.();
  }

  // "새 개인 페이지 작업 데이터베이스", as Notion's (seen 2026-11-05): an empty task database on a private page,
  // named here and added to the sources; it says so with a way to open it, and the list stays.
  async function createPersonalDatabase(name: string) {
    const page = await createNativePage(session, { title: name, workspaceId });
    const { dataSourceId } = await applyDatabaseStarter(session, page.id, { mutationId: newId(), templateId: "tasks", features: [], views: ["all"], taskDatabase: true,
      language: i18next.resolvedLanguage ?? "en", basePageCursor: page.cursor, baseGraphRevision: 0 });
    await pages?.refreshPages?.();
    await saveSources([...picked.map((source) => source.dataSourceId), dataSourceId]);
    setNaming(false);
    toast.notify(t("databaseCreated"), "success", { label: t("openDatabase"), onClick: () => pushRoute(`/p/${page.id}`) }, undefined, t("databaseCreatedDetail", { name }));
  }

  const rowIcon = (icon: ReactNode) => <span aria-hidden="true" style={{ display: "inline-flex", flex: "none" }}>{icon}</span>;
  const menuRows = (): SidebarMenuRow[] => [
    // Only a source the reader may add to takes a new task, as its own "새로 만들기" would.
    ...picked.filter((source) => source.canEdit).map((source) => ({ key: source.dataSourceId, label: sourceName(source),
      icon: <MyTaskDatabaseIcon icon={source.icon} iconType={source.iconType} />, description: source.path.join(" / ") || undefined,
      data: { "data-my-task-menu-source": "" }, run: () => void create(source) })),
    { key: "sources", label: t("taskSources"), icon: rowIcon(<DataPathIcon size="regular" />), separatorBefore: true, next: true,
      data: { "data-my-task-menu-quiet": "" }, run: () => { setMenuOpen(false); setSourcesOpen(true); } },
    { key: "personal", label: t("newPersonalTaskDatabase"), icon: rowIcon(<Plus size="regular" />),
      data: { "data-my-task-menu-quiet": "" }, run: () => { setMenuOpen(false); setNaming(true); } },
  ];

  const empty = sources !== null && picked.length === 0;
  // With no source it has nowhere to go, dim and still, as Notion's (seen 2026-11-08).
  const newTaskButton = <button type="button" className={styles.newButton} ref={button} disabled={empty} onClick={() => setMenuOpen(true)} data-my-tasks-new>{t("newTask")}</button>;
  const sourceSettings = <DatabaseViewSettingsItem icon={<SlidersIcon size="regular" />} label={t("taskSources")} marker="myTaskSources" meta={String(picked.length)} onClick={() => setSourcesOpen(true)} />;
  const settingsFoot = <DatabaseViewSettingsItem icon={<QuestionCircleIcon size="regular" />} label={t("missingTasks")} next={false} marker="myTasksHelp" onClick={() => toast.notify(t("missingTasksHelp"))} />;
  // No task the view shows: the picture, "일치하는 작업이 없습니다." and a blue
  // "+ 새 작업" where a source takes one, as Notion's (seen 2026-11-08).
  const noMatchingTasks = (addRow?: () => void) => (
    <div className={styles.empty} data-my-tasks-empty="matches">
      <TaskPileIcon size="empty" className={styles.emptyArt} aria-hidden="true" />
      <p>{t("noMatchingTasks")}</p>
      {addRow ? <button type="button" className={styles.emptyLink} onClick={addRow} data-my-tasks-empty-new><Plus size="small" aria-hidden="true" /> {t("newTask")}</button> : null}
    </div>
  );
  const dialogs = <>
    {menuOpen ? (
      <SidebarActionMenu button={button} label={t("newTask")} heading={t("whereToCreate")} highlightFirst data={{ "data-my-task-menu": "" }}
        width={330} rows={menuRows()} backdropLabel={t("close")} close={() => setMenuOpen(false)} />
    ) : null}
    {sourcesOpen && sources ? (
      <MyTaskSourcesDialog candidates={sources.candidates} picked={[...picked]} onSave={saveSources} onClose={() => setSourcesOpen(false)} />
    ) : null}
    {naming ? <MyTaskPersonalDatabaseDialog userName={session.user.name} onCreate={createPersonalDatabase} onClose={() => setNaming(false)} /> : null}
  </>;
  return { picked, empty, newTaskButton, sourceSettings, settingsFoot, noMatchingTasks, dialogs, openSources: () => setSourcesOpen(true) };
}
