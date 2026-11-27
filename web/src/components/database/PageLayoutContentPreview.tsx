import { useEffect, useState, type ReactNode } from "react";
import type { ContentGraph } from "@/contentGraph";
import { collectContentGraphWithText } from "@/lib/authenticatedContentGraph";
import { afterOwnWrites, ownWritesInFlight } from "@/lib/ownWritesInFlight";
import { pageTextOpenHere, pageTextStored } from "@/lib/pageTextStored";
import { StructuredEditor } from "@/StructuredEditor";
import type { Session } from "@/types";
import styles from "./DatabasePageLayoutBuilder.module.css";

const ignore = () => {};

// How long a read that failed waits before it is tried again, longer each time.
const RETRY_MS = [1_000, 2_000, 4_000, 8_000];

/**
 * The builder's "콘텐츠" module: the previewed page's body as the page draws
 * it, read only, as Notion's builder shows it (seen 2026-11-12: its text,
 * heading, to-do and list at the page's sizes, wherever the builder was
 * opened from). Its blocks with their words and its databases' settings are
 * read, not their rows. A press on it chooses the module, as on any card.
 * It is read at once, and again once what this tab wrote to the page is on
 * the server, where some may not be: its blocks on their way, and its text
 * typed here (2026-11-12: words typed just before opening the builder were
 * missing from it until it was opened again). A read that fails is tried
 * again. `empty` stands while it is read and for a page with no block at
 * all; a body of empty lines draws them blank, as Notion's builder does
 * (seen 2026-11-25 in Chrome at 1440: "비어 있음" only for a page with no
 * block; one empty line, made through its API or by a press in the empty
 * body, two, an empty heading or to-do and a line of a space drew blank).
 */
export default function PageLayoutContentPreview({ session, pageId, workspaceId, empty }: {
  session: Session;
  pageId: string;
  workspaceId: string;
  empty: ReactNode;
}) {
  const [read, setRead] = useState<{ pageId: string; graph: ContentGraph } | null>(null);
  useEffect(() => {
    let current = true;
    let timer: number | undefined;
    const stop = new AbortController();
    // The later of two reads is the one shown.
    let shown = 0;
    let made = 0;
    const load = async () => {
      const turn = ++made;
      const result = await collectContentGraphWithText(session, pageId, () => current);
      if (current && result && turn > shown) {
        shown = turn;
        setRead({ pageId, graph: result.graph });
      }
    };
    const read = async (attempt: number) => {
      try {
        if (ownWritesInFlight() || pageTextOpenHere(pageId)) {
          void load().catch(() => undefined);
          await afterOwnWrites();
          // Not stored in time, the page is read as the server has it.
          await pageTextStored(session, pageId, stop.signal).catch(() => false);
          if (!current) return;
        }
        await load();
      } catch {
        if (current && attempt < RETRY_MS.length) timer = window.setTimeout(() => void read(attempt + 1), RETRY_MS[attempt]);
      }
    };
    void read(0);
    return () => {
      current = false;
      stop.abort();
      window.clearTimeout(timer);
    };
  }, [pageId, session]);
  const graph = read?.pageId === pageId ? read.graph : null;
  if (!graph || graph.blocks.length === 0) return <>{empty}</>;
  return (
    <div className={styles.contentPreview} data-layout-content-preview>
      <StructuredEditor pageId={pageId} workspaceId={workspaceId} session={session} collaborationDocument={null} graph={graph}
        loading={false} readOnly fixedBody pageStarter={false} showPageOutline={false}
        onAddParagraph={ignore} onAddDatabase={ignore} onChangeParagraph={ignore} />
    </div>
  );
}
