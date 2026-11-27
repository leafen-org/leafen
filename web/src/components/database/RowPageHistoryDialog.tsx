import { Suspense } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import type { GraphProperty } from "@/contentGraph";
import type { Page, Session } from "@/types";

const PageHistoryDialog = lazyScreen(() => (
  import("../PageHistoryDialog").then(({ PageHistoryDialog: Dialog }) => ({ default: Dialog }))
));

/** A database page's version history, over the page, with its properties. */
export function RowPageHistoryDialog({ rowId, title, workspaceId, lockedForMe, properties, session, onClose, onRestored }: {
  rowId: string;
  title: string;
  workspaceId: string;
  lockedForMe: boolean;
  properties: readonly GraphProperty[];
  session: Session;
  onClose: () => void;
  onRestored: () => void;
}) {
  return (
    <Suspense fallback={null}>
      <PageHistoryDialog overPage properties={properties} session={session}
        page={{ id: rowId, title, workspaceId, isLocked: lockedForMe } as Page}
        onClose={onClose} onPageChange={onRestored} />
    </Suspense>
  );
}
