import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { readMyTasksToTheEnd, saveMyTaskSources } from "@/databaseToolbarConfigApi";
import type { DatabaseMyTask } from "@/databaseToolbarConfigWire.generated";
import { displayDate } from "@/lib/displayDate";
import { lazyScreen } from "@/lib/lazyScreen";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import type { Session } from "@/types";
import { CalendarIcon, CheckboxIcon, Plus, TaskPileIcon } from "@/icons/leafen";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { MyTaskSourcesDialog } from "@/components/myTasks/MyTaskSourcesDialog";
import { pickedMyTaskSources, useMyTasksRead } from "@/components/myTasks/useMyTasksRead";
import { HomeSectionHead } from "./HomeRecents";
import { taskHref } from "./homeTaskHref";
import styles from "./HomeView.module.css";
import widgets from "./HomeWidgets.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";

type Ready = { state: "ready"; tasks: DatabaseMyTask[] };
type Load = { state: "loading" } | { state: "failed" } | { state: "offline" } | Ready;

// The tasks last read in each workspace this visit, shown while the server
// doesn't answer.
const lastRead = new Map<string, Ready>();

function dueText(task: DatabaseMyTask) {
  if (!task.dueStart) return "";
  return task.dueEnd && task.dueEnd !== task.dueStart ? `${task.dueStart} → ${task.dueEnd}` : task.dueStart;
}

/**
 * The tasks assigned to the reader in the task databases "내 작업" takes
 * them from (pickedMyTaskSources), for Home's "예정된 이벤트" — only the
 * reader's, never someone else's or one assigned to no one, as Notion's home
 * widget lists them (seen 2026-11-24, two tabs). All of them, read a window at a
 * time (readMyTasksToTheEnd): the first shows at once, then the whole list;
 * read again, the list in view stays until the whole list replaces it.
 */
export function useHomeMyTasks(session: Session, workspaceId: string): { load: Load; reload(): void } {
  const [load, setLoad] = useState<Load>({ state: "loading" });
  const shown = useRef(load);
  shown.current = load;
  const [attempt, setAttempt] = useState(0);
  const key = `${session.user.id}\u0000${workspaceId}`;
  useEffect(() => {
    const controller = new AbortController();
    let stopRetry: (() => void) | undefined;
    // Reading again keeps this workspace's tasks in view until the answer.
    const inView = shown.current.state === "ready" && lastRead.get(key) === shown.current;
    if (!inView) setLoad({ state: "loading" });
    readMyTasksToTheEnd(session, workspaceId, { signal: controller.signal, own: true,
      onFirst: (first) => { if (!inView && !controller.signal.aborted) setLoad({ state: "ready", tasks: first }); } })
      .then((tasks) => {
        const ready: Ready = { state: "ready", tasks };
        lastRead.set(key, ready);
        setLoad(ready);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (!isServerUnreachable(error)) {
          setLoad({ state: "failed" });
          return;
        }
        // Without its server Home shows the tasks it last read, or that they
        // aren't available offline, and reads them when the server answers.
        setLoad(lastRead.get(key) ?? { state: "offline" });
        stopRetry = retryWhenServerMayAnswer(() => setAttempt((value) => value + 1));
      });
    return () => {
      controller.abort();
      stopRetry?.();
    };
  }, [attempt, key, session, workspaceId]);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);
  return { load, reload };
}

/** A day as tasks' due dates write it, in the reader's time. */
function localDay(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/**
 * Home's "예정된 이벤트": the reader's open tasks due today or later, soonest
 * first — Notion's home shows upcoming calendar events, which Leafen takes
 * from its own dated tasks (no calendar connection).
 */
export function HomeUpcoming({ load }: { load: Load }) {
  const { t } = useTranslation("homeView");
  const today = new Date();
  const todayKey = localDay(today);
  const tomorrowKey = localDay(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1));
  const upcoming = load.state === "ready" ? load.tasks
    .filter((task) => !task.done && task.dueStart && task.dueStart.slice(0, 10) >= todayKey)
    .sort((left, right) => left.dueStart.localeCompare(right.dueStart))
    .slice(0, 8) : [];
  const day = (task: DatabaseMyTask) => {
    const key = task.dueStart.slice(0, 10);
    return key === todayKey ? t("today") : key === tomorrowKey ? t("tomorrow") : dueText(task);
  };
  return (
    <section className={styles.homeSection} aria-labelledby="home-upcoming-title" data-home-upcoming>
      <HomeSectionHead id="home-upcoming-title" widget="upcoming" icon={<CalendarIcon size="small" aria-hidden="true" />}>{t("upcoming")}</HomeSectionHead>
      {load.state === "ready" && upcoming.length === 0 ? <p className={styles.myTasksNote}>{t("upcomingEmpty")}</p> : null}
      {upcoming.length > 0 ? (
        <ul className={styles.myTasksList}>
          {upcoming.map((task) => (
            <li key={task.rowId}>
              <a className={styles.homeRow} href={taskHref(task)} data-my-task={task.rowId}>
                <span className={styles.myTaskIcon} aria-hidden="true"><PageIconGlyph page={{ icon: task.icon, iconType: task.icon ? task.iconType : "none" }} size={16} /></span>
                <span className={styles.myTaskTitle}>{task.title || task.untitledName || untitledPageDisplayTitle("page")}</span>
                <span className={styles.homeRowNote}>{day(task)}</span>
              </a>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

// The view, its tools and windows come with their code once Home shows them (lazyScreen).
const HomeMyTasksView = lazyScreen(() => import("./HomeMyTasksView"), { entry: true });

/**
 * Home's "내 작업", as Notion's Home widget (seen 2026-11-24 in the test
 * workspace, two tabs): with no source, the card that says what it is for
 * beside sample tasks (HomeMyTasksStart); with sources, the "내 작업" page's
 * first view itself (HomeMyTasksView). A task opens over the home (taskHref).
 */
export function HomeMyTasks({ session, workspaceId, workspaceName, onSourcesSaved }: {
  session: Session; workspaceId: string; workspaceName: string; onSourcesSaved(): void;
}) {
  const { t } = useTranslation("homeView");
  const read = useMyTasksRead(session, workspaceId, { keepOffline: true });
  const [tools, setTools] = useState<HTMLElement | null>(null);
  const [picking, setPicking] = useState(false);
  const picked = pickedMyTaskSources(read.sources);
  const shown = read.sources !== null && read.tasks !== null;
  const view = shown && picked.length > 0;
  useEffect(() => {
    if (shown && window.location.hash === "#my-tasks") document.getElementById("my-tasks")?.scrollIntoView({ block: "start" });
  }, [shown]);
  const saveSources = async (ids: string[]) => {
    read.setSources(await saveMyTaskSources(session, workspaceId, ids));
    read.reload();
    onSourcesSaved();
  };
  return (
    <section id="my-tasks" className={styles.homeSection} aria-labelledby="home-my-tasks-title" data-home-my-tasks data-home-widget>
      {view ? (
        <div className={widgets.taskHead}>
          <h2 id="home-my-tasks-title" className={styles.sectionHead}><CheckboxIcon size="small" aria-hidden="true" /><span>{t("myTasks")}</span></h2>
          <span ref={setTools} className={widgets.taskTools} data-home-my-tasks-tools />
        </div>
      ) : <HomeSectionHead id="home-my-tasks-title" widget="myTasks" icon={<CheckboxIcon size="small" aria-hidden="true" />}>{t("myTasks")}</HomeSectionHead>}
      {!shown && !read.failed && !read.offline ? <p className={styles.myTasksNote}>{t("myTasksLoading")}</p> : null}
      {read.failed && !shown ? <p className={styles.myTasksNote} role="status">{t("myTasksFailed")}</p> : null}
      {read.offline && !shown ? <p className={styles.myTasksNote} role="status">{t("myTasksOffline")}</p> : null}
      {shown && !view ? <HomeMyTasksStart workspaceName={workspaceName} onAddSources={() => setPicking(true)} /> : null}
      {/* What the view waits for holds the view alone, not Home with the head its tools go in. */}
      {view && tools ? <Suspense fallback={null}>
        <HomeMyTasksView session={session} workspaceId={workspaceId} read={read} tools={tools} onSourcesSaved={onSourcesSaved} />
      </Suspense> : null}
      {picking && read.sources ? (
        <MyTaskSourcesDialog candidates={read.sources.candidates} picked={[...picked]} onClose={() => setPicking(false)} onSave={saveSources} />
      ) : null}
    </section>
  );
}

// Notion's sample tasks (seen 2026-11-11): each with its day from today and whether it is work or personal.
const SAMPLE_TASKS = [
  { key: "sampleTaskOne", days: 0, kind: "sampleWork" }, { key: "sampleTaskTwo", days: 1, kind: "sampleWork" },
  { key: "sampleTaskThree", days: 7, kind: "sampleWork" }, { key: "sampleTaskFour", days: 0, kind: "samplePersonal" },
  { key: "sampleTaskFive", days: 7, kind: "samplePersonal" },
] as const;

/**
 * "내 작업" with no source, as Notion's widget (seen 2026-11-11): what it is
 * for and "작업 출처 추가하기", which opens the same sources window as the
 * "내 작업" page, beside sample tasks.
 */
function HomeMyTasksStart({ workspaceName, onAddSources }: { workspaceName: string; onAddSources(): void }) {
  const { t } = useTranslation("homeView");
  const today = new Date();
  const day = (days: number) => displayDate(new Date(today.getFullYear(), today.getMonth(), today.getDate() + days));
  return (
    <div className={widgets.card} data-home-my-tasks-start>
      <div className={widgets.text}>
        <span className={widgets.mark} aria-hidden="true"><TaskPileIcon size="empty" /></span>
        <p>{t("myTasksStartHint", { workspace: workspaceName })}</p>
        <button type="button" className={widgets.start} aria-haspopup="dialog" onClick={onAddSources}>{t("myTasksAddSources")}</button>
      </div>
      <div className={widgets.taskSample} aria-hidden="true">
        {SAMPLE_TASKS.map(({ key, days, kind }) => (
          <div key={key} className={widgets.taskSampleRow}>
            <span className={widgets.taskBox} />
            <span className={widgets.sampleName}>{t(key)}</span>
            <span className={widgets.taskWhen}>{day(days)}</span>
            <span className={widgets.taskKind}>{t(kind)}</span>
          </div>
        ))}
        <div className={widgets.taskSampleRow} data-new><Plus size="regular" className={widgets.sampleIcon} /><span>{t("sampleNewTask")}</span></div>
      </div>
    </div>
  );
}
