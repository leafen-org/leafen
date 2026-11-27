import { useEffect, useMemo, useRef, useState } from "react";
import type { Session } from "@/types";
import { dataSourceMentionAddress, dataSourceMentionId, setDataSourceMentionNames } from "@/lib/dataSourceMentionNames";
import { dataSourceDisplayTitle } from "./databaseTitles";
import { keptFurther, MentionImageIconRule, mentionedNow, useMentionWindow } from "./mentionedOnScreen";
import { dataSourceMentionCss, dataSourceMentionElement, type MentionIconDataSource } from "./pageMentionIconCss";
import { dataSourcePreviewAgain, dataSourcePreviewOnce, readShownPreviews } from "./pagePreviews";

// The data sources mentioned on screen (richtextPageLinks.ts
// dataSourceMentionHtml), by their own IDs — not one nothing here answers to,
// drawn as such by its empty mark — and those drawn closed to their reader or
// missing, by the address they keep (richtextPageLinks.ts
// keptTargetAttribute): those are asked how they stand to the reader as the
// others are (useMentionReach.ts), and their names read too, coming back as
// they open to the reader (2026-11-25: left out, a data source's mention went
// on showing its name after its share was taken back, until a reload, and
// one drawn closed stayed so after the share came back).
const MENTIONED_DATA_SOURCES = '[data-mention="database"][data-mention-data-source]:not([data-mention-data-source=""])';
const MARKED_DATA_SOURCES = `[data-mention="database"][data-mention-database^="${dataSourceMentionAddress("")}"]`
  + ":is([data-mention-unavailable], [data-mention-missing])";
export const SHOWN_DATA_SOURCES = `${MENTIONED_DATA_SOURCES}, ${MARKED_DATA_SOURCES}`;
/** The data source a mention on screen (SHOWN_DATA_SOURCES) names, by its own ID. */
export const shownDataSourceIdOf = (element: HTMLElement) => element.dataset.mentionDataSource
  || dataSourceMentionId({ mention: "database", databaseId: element.dataset.mentionDatabase });
// The most mentioned data sources read at a time for the mentions on screen,
// those nearest the view (useMentionWindow).
const MENTION_DATA_SOURCE_LIMIT = 100;
// How long the page list stays still before mentioned sources are read again.
const DATA_SOURCE_NAMES_DELAY_MS = 1000;

/**
 * The data sources mentioned on screen, as their mentions draw them as
 * Notion's do (seen 2026-11-22 in Chrome at 1440): by their names now, which
 * the mentions follow (dataSourceMentionNames.ts) — one without a name as a
 * source list names it — their own icons before them, and "휴지통에 있음" after
 * those in the trash. Each is read at once when it comes on screen, and again
 * once the page list has stayed still after a change: those nearest the view,
 * the others as they come nearer.
 */
export function DataSourceMentionStyles({ mentioned, session, workspaceId = "", pages, trashedLabel }: {
  /** The data sources mentioned on screen (SHOWN_DATA_SOURCES, shownDataSourceIdOf). */
  mentioned: ReadonlySet<string>;
  session?: Session;
  workspaceId?: string;
  /** The workspace's page list, after whose changes (a source renamed, sent to the trash) the sources are read again. */
  pages: readonly unknown[];
  trashedLabel: string;
}) {
  const [read, setRead] = useState<readonly MentionIconDataSource[]>([]);
  const shown = useRef<{ ids: ReadonlySet<string>; pages?: readonly unknown[] }>({ ids: new Set() });
  const names = useRef<ReadonlyMap<string, string>>(new Map());
  const taken = useMentionWindow(useMemo(() => [...mentioned], [mentioned]), SHOWN_DATA_SOURCES, shownDataSourceIdOf, MENTION_DATA_SOURCE_LIMIT);
  useEffect(() => {
    const ids = [...taken];
    if (!session || ids.length === 0) return;
    const was = shown.current;
    const unread = ids.some((id) => !was.ids.has(id));
    // As others only came nearer the view, those read at the last change aren't asked again.
    const kept = (id: string) => unread && was.pages === pages && was.ids.has(id);
    return readShownPreviews(unread ? 0 : DATA_SOURCE_NAMES_DELAY_MS,
      () => Promise.all(ids.map((id) => (kept(id) ? dataSourcePreviewOnce : dataSourcePreviewAgain)(session, id, "refused"))),
      (previews) => {
        shown.current = { ids: taken, pages };
        const still = mentionedNow(SHOWN_DATA_SOURCES, shownDataSourceIdOf);
        names.current = keptFurther(names.current, new Map(previews.flatMap((preview, index) => preview
          ? [[ids[index], dataSourceDisplayTitle(preview.title, preview.sources)] as const] : [])), taken, still);
        // By the sources' addresses, as their mentions keep them.
        setDataSourceMentionNames(new Map([...names.current].map(([id, name]) => [dataSourceMentionAddress(id), name] as const)));
        const now = new Map(previews.flatMap((preview, index) => preview ? [[ids[index], {
          id: ids[index], icon: preview.icon ?? "", iconType: (preview.iconType ?? "none") as MentionIconDataSource["iconType"],
          inTrash: preview.inTrash === true,
        }] as const] : []));
        setRead((was) => {
          const next = [...keptFurther(new Map(was.map((source) => [source.id, source])), now, taken, still).values()];
          return was.length === next.length && was.every((source, index) => (
            source.id === next[index].id && source.icon === next[index].icon
            && source.iconType === next[index].iconType && source.inTrash === next[index].inTrash
          )) ? was : next;
        });
      });
  }, [pages, session, taken]);
  const sources = useMemo(() => read.filter((source) => mentioned.has(source.id)), [mentioned, read]);
  const css = useMemo(() => dataSourceMentionCss(sources, trashedLabel), [sources, trashedLabel]);
  return (
    <>
      {css ? <style data-data-source-mention-icons="">{css}</style> : null}
      {sources.filter((source) => (source.iconType === "image" || source.iconType === "custom_emoji") && source.icon
        && /^[A-Za-z0-9_-]+$/u.test(source.id)).map((source) => (
        <MentionImageIconRule key={`dataSource:${source.id}`} page={{ ...source, workspaceId }}
          selector={`${dataSourceMentionElement(source.id)}::before`} />
      ))}
    </>
  );
}
