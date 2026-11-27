import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { usePageLastEditVia } from "@/lib/pageEditedActivity";
import type { Session } from "@/types";
import { DatabaseRowMenu, type DatabaseRowPageLockControl, type RowPageMenuExtras } from "./DatabaseRowMenu";
import { rowPageMenuFoot } from "./rowMenuFoot";

/**
 * A database template's page "…", Notion's page menu (seen 2026-10-30), as
 * an open row page's is (RowContextMenuSurface): its lines as its page gives
 * them, "테마와 레이아웃 › 레이아웃 사용자 지정" among them, and its foot —
 * when and by whom it was last edited, its body's words — as Notion's
 * template page (seen 2026-11-16, Chrome 1440: "최종 편집 일시 · 나 · 오늘
 * 오전 7:59" and "레이아웃 사용자 지정" ending its "테마와 레이아웃"), its
 * "더 보기 › 내보내기" exporting it as a database page's and "가져오기"
 * importing pages under it, which its body holds as sub-pages (seen
 * 2026-11-16). Only what a template's page lacks so far is left out
 * (useDatabaseRowPageMenu's `template`): "연결".
 */
export function TemplatePageMenu({ session, snapshot, title, anchor, extras, readOnly, lock, onClose, onCopyLink, onDuplicate, onTrash, onAnalytics }: {
  session: Session;
  /** The template's page as shown, whose foot it reads. */
  snapshot: DatabaseRowPageSnapshot;
  title: string;
  anchor: { x: number; y: number };
  extras: RowPageMenuExtras;
  readOnly: boolean;
  lock?: DatabaseRowPageLockControl;
  onClose(): void;
  onCopyLink(): Promise<void>;
  onDuplicate?(): Promise<void>;
  onTrash?(): Promise<void>;
  onAnalytics?(): void;
}) {
  const { t } = useTranslation("rowMenu");
  const templateId = snapshot.row.id;
  const via = usePageLastEditVia(session, templateId, snapshot.row.lastEditedBy || snapshot.row.createdBy || null, true);
  return (
    <DatabaseRowMenu anchor={anchor} favorite={false} moveTargets={[]} rowTitle={title} readOnly={readOnly}
      variant="database-row" page={extras.page && { ...extras.page, template: true }}
      pageStyle={readOnly ? undefined : extras.style} lock={lock} onHistory={extras.onHistory}
      suggesting={extras.suggesting} onCustomizeLayout={extras.onCustomizeLayout} onExport={extras.onExport} onImport={extras.onImport}
      notifications={{
        read: async (signal) => (await api.readDatabaseRowNotificationPreference(session, templateId, signal)).level,
        save: async (level) => (await api.saveDatabaseRowNotificationPreference(session, templateId, level)).level,
      }}
      onClose={onClose} onCopyLink={onCopyLink} onDuplicate={onDuplicate} onTrash={onTrash} onAnalytics={onAnalytics}
      foot={{ ...rowPageMenuFoot(snapshot, (count) => t("meta.wordCount", { count }), session.user.id), via }} />
  );
}
