import { useMemo, useRef, useState, type ComponentProps, type ReactNode } from "react";
import pageStyles from "@/components/page/PageView.module.css";
import { TopBarLock } from "@/components/page/TopBarLock";
import { TopBarSidebarToggle } from "@/components/page/TopBarSidebarToggle";
import { usePageColumn } from "@/components/page/usePageColumn";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { inlineDatabaseAddressPage } from "@/lib/inlineDatabaseAddress";
import { inlineDatabaseOpenedAlone } from "@/lib/inlineDatabaseFullPage";
import { databaseTemplateActions, useDatabaseTemplates } from "@/lib/databaseTemplateRequests";
import type { Session } from "@/types";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { TemplateEditingBanner } from "./TemplateEditingBanner";
import { RowPageBodyEditor, type OpenedRowPageDatabase } from "./RowPageBodyEditor";
import { RowPageDatabaseAloneActions } from "./RowPageDatabaseAloneActions";
import { RowPagePath, type RowPathDatabase } from "./RowPagePath";
import { useOpenedAloneDatabaseLock } from "./openedAloneDatabaseLock";
import { useFullRowWindowTitle } from "./useFullRowWindowTitle";

/**
 * One of a database page's own databases opened alone — by its "전체 페이지로
 * 열기", by its restore from the trash, by a link to it — as Notion opens it
 * (seen 2026-11-15, Chrome, the database in a row's body: its address
 * /p/<database>?v=, its path "… / 행 하나 / w1146 안쪽 DB", its name as the
 * page's title over its views): the page shows the database alone, as a
 * page's inline database opened alone does (inlineDatabaseFullPage), its path
 * running through the page, whose piece opens it, and its bar's right as
 * Notion's (RowPageDatabaseAloneActions). Drawn as the page before, the
 * database's address stayed on the page whole.
 */
export function RowPageDatabaseAlone({ blockId, children, onReturn, pathDatabase, readOnly, rowTitle, session, snapshot, template }: {
  /** The database's block in the page's body. */
  blockId: string;
  /** What the page keeps beside it, as its database's templates (StandaloneRowTemplates). */
  children?: ReactNode;
  /** Returns to the database the page is in. */
  onReturn(): void;
  pathDatabase?: RowPathDatabase;
  readOnly: boolean;
  rowTitle: string;
  session: Session;
  snapshot: DatabaseRowPageSnapshot;
  /**
   * The page is a template, of the database named: one of its own databases
   * opened alone (its "↗", its restore from the trash) shows alone under the
   * band of the template being edited, as Notion's (seen 2026-11-15, Chrome:
   * /p/<database>?v=, "… / tpl / 새 데이터베이스", "다음에서 템플릿을 편집
   * 중입니다. 〈database〉 ⓘ 복제 주기 ⌄"), not in the template's window over
   * its database, which the template's address opens.
   */
  template?: Pick<ComponentProps<typeof TemplateEditingBanner>, "databaseTitle" | "databaseIcon" | "viewing">;
}) {
  const [opened, setOpened] = useState<OpenedRowPageDatabase | null>(null);
  // Its column reaches the page's edges as a page's does, its views' tables with it.
  const docRef = useRef<HTMLDivElement>(null);
  usePageColumn(docRef);
  const title = opened?.title || inlineDatabasePlaceholderTitle();
  const icon = opened?.icon ?? "", iconType = opened?.iconType ?? "none";
  useFullRowWindowTitle(true, title, { icon, iconType, workspaceId: snapshot.workspaceId, id: blockId });
  const alone = useMemo(() => ({ blockId, title, titleSpans: opened?.titleSpans, icon: { icon, iconType } }),
    [blockId, icon, iconType, opened?.titleSpans, title]);
  // Locked, it shows its lock after the path, as a page's database alone does (TopBar).
  const aloneLock = useOpenedAloneDatabaseLock();
  const lock = aloneLock?.blockId === blockId && aloneLock.lock.locked ? aloneLock.lock : null;
  // The template's name, which its page's values don't hold, and its repeat, as its database's templates offer them (StandaloneRowTemplates).
  const templates = useDatabaseTemplates(template ? snapshot.row.dataSourceId : undefined);
  const pageTitle = (template && templates.find(({ id }) => id === snapshot.row.id)?.name) || rowTitle;
  const templateActions = template && !readOnly ? databaseTemplateActions(snapshot.row.dataSourceId) : undefined;
  const repeating = templateActions?.template(snapshot.row.id);
  return (
    <>
      <header className={topBarStyles.topbar} data-row-database-alone={blockId} data-topbar-bar>
        <TopBarSidebarToggle />
        <RowPagePath row={snapshot.row} title={pageTitle} workspaceId={snapshot.workspaceId} database={pathDatabase}
          onReturn={onReturn} alone={alone} fallback={null} trailing={lock ? <TopBarLock unlockedForMe={lock.unlockedForMe}
            onLockAgain={lock.relock} onUnlockForMe={lock.unlockForMe} onUnlockForEveryone={lock.unlockForEveryone} /> : null} />
        <RowPageDatabaseAloneActions blockId={blockId} readOnly={readOnly} rowTitle={pageTitle} session={session} snapshot={snapshot} title={title} />
      </header>
      {template ? <TemplateEditingBanner {...template} repeat={templateActions && repeating ? {
        template: repeating, onSave: (repeat) => templateActions.setRepeat(snapshot.row.id, repeat) } : undefined} /> : null}
      <div data-page-scroll data-database-canvas="true" className={`${pageStyles.scroll} nscroll`}>
        <div ref={docRef} className={pageStyles.doc} data-page-search-root data-kind="database" data-inline-database-page="true" data-database-page="true"
          data-has-cover="false" data-full-width="false" data-font={snapshot.row.font} data-small-text={snapshot.row.smallText ? "true" : "false"}>
          <RowPageBodyEditor key={`${snapshot.row.id}:${blockId}`} session={session} rowId={snapshot.row.id}
            workspaceId={snapshot.workspaceId} readOnly={readOnly} onlyBlockId={blockId} onOpenedDatabase={setOpened} />
        </div>
      </div>
      {children}
    </>
  );
}

/** The database of the page's own body its address opens alone, if any. */
export function rowPageDatabaseOpenedAlone(rowId: string) {
  const blockId = inlineDatabaseOpenedAlone();
  return blockId && blockId !== rowId && inlineDatabaseAddressPage(blockId) === rowId ? blockId : null;
}
