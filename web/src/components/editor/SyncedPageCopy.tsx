import { useContext, useEffect, useState } from "react";
import { api } from "@/api";
import type { GraphBlock } from "@/contentGraph";
import { InfoIcon } from "@/icons/leafen";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { SyncedOriginalTrail, syncedPageShownAround } from "./syncedBlockContext";
import type { SyncedBlockSource } from "./syncedBlockModel";
import { SyncedOriginalEditor } from "./SyncedOriginalEditor";
import styles from "./editorStyles.generated.module.css";
import copyStyles from "./SyncedPageCopy.module.css";

/**
 * A synced copy showing a whole page's body draws the page's top level where
 * it stands and edits it there, with that page's editor and under its
 * permission, as Notion's does (seen 2026-11-27, two tabs at two times).
 * Copies showing pages that show copies go in no further than a page's body
 * shown twice on the way in, the open page's own counted, as Notion's
 * (seen 2026-11-27 twice, in two tabs: a copy of a page on that page shows
 * its other blocks once; pages A and B each holding a copy of the other
 * show, opened at A, A's lines, B's, A's again and B's again): the copy that
 * would show a page a third time shows nothing, not even a notice. While the
 * page is in the trash it says so with a way to open the page, its content
 * coming back once the page is restored.
 */
export function SyncedPageCopy({ adapter, block, source }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  source: SyncedBlockSource;
}) {
  const trail = useContext(SyncedOriginalTrail);
  const session = adapter.session;
  // Whether the page is in the trash: null until read.
  const [trashed, setTrashed] = useState<boolean | null>(null);
  const inside = syncedPageShownAround(trail, adapter.pageId, source.pageId) >= 2;
  useEffect(() => {
    if (!session || inside) return;
    let cancelled = false;
    api.syncedBlockLocations(session, adapter.pageId, block.id)
      .then((read) => { if (!cancelled) setTrashed(read.original.inTrash === true); })
      .catch(() => { if (!cancelled) setTrashed(false); });
    return () => { cancelled = true; };
  }, [adapter.pageId, block.id, inside, session]);
  if (!session) return <div className={styles.syncedMissing}>{blockItemText("synced.unavailable")}</div>;
  if (inside || trashed === null) return null;
  if (trashed) {
    return <div className={copyStyles.trashed} contentEditable={false} data-synced-page-trashed>
      <InfoIcon size="small" aria-hidden="true" />
      <span>{blockItemText("synced.pageInTrash")}{" "}
        <button type="button" className={copyStyles.open} onClick={() => pushRoute(pageHref(source.pageId))}>
          {blockItemText("synced.openPage")}</button></span>
    </div>;
  }
  return <SyncedOriginalEditor session={session} pageId={source.pageId} blockId={source.blockId} wholePage
    workspaceId={adapter.workspaceId} readOnly={adapter.readOnly} copy={{ blockId: block.id, page: () => adapter }} />;
}
