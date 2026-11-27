import { MyTasksDatabaseView } from "@/components/myTasks/MyTasksDatabaseView";
import { useMyTaskActions } from "@/components/myTasks/useMyTaskActions";
import type { MyTasksRead } from "@/components/myTasks/useMyTasksRead";
import { pushRoute } from "@/lib/router";
import type { Session } from "@/types";
import { taskHref } from "./homeTaskHref";
import widgets from "./HomeWidgets.module.css";

const noLock = () => {}, noFavorite = () => {};

/**
 * Home's "내 작업" with sources, as Notion's Home widget (seen 2026-11-24 in
 * the test workspace, two tabs): the "내 작업" page's first view itself
 * (MyTasksDatabaseView) — its filters, sort, grouping and properties, so the
 * reader's open tasks under the view's "담당자: 나" and "상태" filters,
 * grouped by status where the view groups them, each group's "+ 새 작업" and
 * an empty group's "필터 편집하기 · + 새 작업"; none to show, "일치하는 작업이
 * 없습니다." and "+ 새 작업". It stands in a card at most 222px tall that
 * scrolls, the view's tools — filter, sort, search, "전체 페이지로 열기" to the
 * page, settings and the blue "새 작업" asking which source (useMyTaskActions)
 * — at the end of the widget's head (`tools`) while the pointer is on it. A
 * task opens over the home.
 */
export default function HomeMyTasksView({ session, workspaceId, read, tools, onSourcesSaved }: {
  session: Session; workspaceId: string; read: MyTasksRead; tools: HTMLElement; onSourcesSaved(): void;
}) {
  const actions = useMyTaskActions({ session, workspaceId, read, openHref: (rowId) => taskHref({ rowId }), onSourcesSaved });
  if (!read.tasks) return null;
  return <>
    <div className={widgets.taskView} data-home-my-tasks-view>
      <MyTasksDatabaseView session={session} workspaceId={workspaceId} tasks={read.tasks} sources={actions.picked} widgetHead={tools}
        newRowControl={actions.newTaskButton} emptyRows={actions.noMatchingTasks} sourceSettings={actions.sourceSettings} settingsFoot={actions.settingsFoot}
        onLockControls={noLock} onFavorite={noFavorite} onOpenRow={(rowId) => pushRoute(taskHref({ rowId }))} onChanged={read.reload} onTaskMade={read.taskMade} />
    </div>
    {actions.dialogs}
  </>;
}
