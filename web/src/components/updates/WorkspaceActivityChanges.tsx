import { Fragment, useEffect, useRef, useState, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { pageHref } from "@/lib/navigation";
import { rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import {
  type WorkspaceActivityChange,
  type WorkspaceActivityChangesResponse,
  type WorkspaceActivityRecord,
} from "@/workspaceActivityWire.generated";
import { PropertyIcon } from "../database/PropertyIcon";
import { RichTextLine } from "../database/RichTextCellText";
import { richTextParts } from "@/lib/richTextSummary";
import { addedText } from "@/lib/pageUpdates";
import { blockAnchor } from "@/lib/blockAddress";
import type { Page } from "@/types";
import { ActivityChangePage } from "./ActivityChangePage";
import { untitledActivityName } from "./activityNames";
import { updateLineBlank, updateLineBoxed } from "@/lib/updateLineKinds";
import { UpdateLine } from "./UpdateLine";
import { UpdatePropertyValue } from "./UpdatePropertyValue";
import { UPDATE_CHANGES_SHOWN, UpdateChangesMore } from "./UpdateChangesMore";
import styles from "../UpdatesPanelFeed.module.css";

// What an "edited the page" update changed, under it as Notion's workspace
// updates list it (seen 2026-10-19): an added line blue on a pale blue band,
// a changed line with what was added so, a sub-page or an inline database
// made in it by its glyph and name on that band, deleted lines and sub-pages struck
// through, each under its own "삭제됨", a database page's property under its name and type
// with its new value — a text's added words marked, an option its chip, a
// number as it reads, as the page's updates tab draws it — each as it was then. Five show; "N개 항목 더 보기"
// shows the rest in place, as the page's updates tab folds them
// (UpdateChangesMore). They're read once the update comes into view. A
// line, and a property's text value, draws its equations, mentions and marks
// as the page does, as Notion's (seen 2026-11-13), the way the page's
// updates tab draws its lines (lib/richTextSummary.ts), each by its block's
// kind (UpdateLine).

const deletedKinds = new Set<WorkspaceActivityChange["kind"]>(["deleted", "deletedPage"]);

/** A line added, changed or deleted, its added words marked in a changed one. */
function ChangedLine({ change }: { change: WorkspaceActivityChange }) {
  const text = change.kind === "added" ? change.added : change.text;
  if (change.kind !== "changed") {
    const spans = change.spans && richTextParts(change.spans, change.spans.map((span) => span.text).join(""),
      { before: "", added: text, after: "" });
    return <RichTextLine text={text} spans={spans?.added} />;
  }
  const spans = change.spans && richTextParts(change.spans, change.spans.map((span) => span.text).join(""), change);
  return (
    <><RichTextLine text={change.before} spans={spans?.before} /><span data-added><RichTextLine text={change.added} spans={spans?.added} /></span>
      <RichTextLine text={change.after} spans={spans?.after} /></>
  );
}

/**
 * A property's value in an update: a text by its words, those added marked,
 * any other by its stored value as its cell shows it (UpdatePropertyValue).
 */
function PropertyChange({ change }: { change: WorkspaceActivityChange }) {
  const property = { id: change.blockId, name: change.text, type: change.propertyType || "rich_text", config: change.propertyConfig };
  if (change.value !== undefined) return <UpdatePropertyValue property={property} value={change.value} was={change.previousValue} />;
  const cleared = !change.after;
  const shown = cleared ? change.before : change.after;
  const words = cleared ? { before: shown, added: "", after: "" } : addedText(change.before, change.after);
  const spans = change.spans && richTextParts(change.spans, change.spans.map((span) => span.text).join(""), words);
  return <UpdatePropertyValue property={property} value={shown} words={words} spans={spans} />;
}

export function WorkspaceActivityChanges({ item, read, onOpen, pageOf, untitledPage }: {
  item: WorkspaceActivityRecord;
  read: (item: WorkspaceActivityRecord) => Promise<WorkspaceActivityChangesResponse>;
  onOpen: (item: WorkspaceActivityRecord, href: string) => void;
  /** A sub-page as the reader's list holds it, for its own icon (ActivityChangePage). */
  pageOf?: (pageId: string) => Page | undefined;
  /** What a sub-page with no name is called: a database's "새 데이터베이스" (activityNames.ts). */
  untitledPage: (pageId: string) => string;
}) {
  const { t } = useTranslation(["updatesPanel", "common", "pageAnalytics"]);
  const box = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);
  const [result, setResult] = useState<WorkspaceActivityChangesResponse | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const element = box.current;
    if (!element || seen) return;
    const observer = new IntersectionObserver(([entry]) => { if (entry?.isIntersecting) setSeen(true); },
      { root: element.closest("[data-inbox-list]"), rootMargin: "200px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, [seen]);

  useEffect(() => {
    if (!seen) return;
    let live = true;
    // The lines only show more of the update; it stands without them.
    read(item).then((next) => { if (live) setResult(next); }, () => undefined);
    return () => { live = false; };
  }, [seen, read, item]);

  const changes = result?.changes ?? [];
  const shown = open ? changes : changes.slice(0, UPDATE_CHANGES_SHOWN);
  // "N개 항목 더 보기" folds the changes read and counts those past them; with
  // none of the read ones folded, those past them are said at once — a button
  // that showed nothing more was all a long edit's update had (2026-11-13).
  const hidden = changes.length - shown.length;
  const folded = hidden > 0 ? hidden + (result?.more ?? 0) : 0;
  const rest = open || hidden === 0 ? result?.more ?? 0 : 0;
  const follow = (href: string) => (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    event.preventDefault();
    onOpen(item, href);
  };

  return (
    <div ref={box} className={styles.activityChanges} data-activity-changes={result ? changes.length : undefined}>
      {shown.map((change) => {
        const deleted = deletedKinds.has(change.kind);
        const page = change.kind === "page" || change.kind === "deletedPage";
        // An inline database made in the edit opens alone, by its block (Notion's, seen 2026-11-16).
        const database = change.kind === "database";
        if (database) rememberInlineDatabaseAddress(change.blockId, item.pageId);
        const href = change.kind === "page" || database ? pageHref(change.blockId)
          : deleted || change.kind === "property" ? pageHref(item.pageId) : pageHref(item.pageId) + blockAnchor(item.pageId, change.blockId);
        if (change.kind === "property") return (
          // Notion's: the property's icon and name, then its new value, or
          // the old one struck when it was cleared (seen 2026-10-19), drawn
          // as the page's updates tab draws it (UpdatePropertyValue).
          <a key={change.blockId} className={styles.activityChange} data-change={change.after ? "property" : "clearedProperty"} href={href}
            onClick={follow(href)}>
            <span className={styles.activityChangeProperty}>
              <PropertyIcon property={{ type: change.propertyType || "rich_text", config: { icon: change.propertyIcon } }} size="tiny" />
              {/* A page's new title under "Aa 제목", as the page's updates tab names it (workspace_activity_title_change.go). */}
              <span>{change.text || (change.propertyType === "title" ? t("pageAnalytics:updates.titleLabel") : "")}</span>
            </span>
            <span className={styles.activityChangeText}><PropertyChange change={change} /></span>
          </a>
        );
        return (
          <Fragment key={change.blockId}>
            {/* Each deleted line under its own "삭제됨", even lines deleted at once (Notion's, seen 2026-11-13). */}
            {deleted
              ? <span className={styles.activityChangeLabel}>{t("updatesPanel:inbox.activity.deleted")}</span> : null}
            <a className={styles.activityChange} data-change={change.kind} data-block-type={change.blockType} href={href} onClick={follow(href)}
              data-boxed={!page && !database && updateLineBoxed(change.blockType, change.kind) ? "" : undefined}
              data-blank={change.kind === "added" && updateLineBlank(change.blockType) ? "" : undefined}>
              <span className={styles.activityChangeText}>
                {page ? <ActivityChangePage page={pageOf?.(change.blockId)}>{change.text || untitledPage(change.blockId)}</ActivityChangePage>
                  : database ? <ActivityChangePage database>{change.text || untitledActivityName("dataSource", "")}</ActivityChangePage>
                  : change.kind === "added" || change.kind === "changed" || change.kind === "deleted"
                    ? <UpdateLine change={change.kind} blockType={change.blockType} checked={change.checked} language={change.language}
                      was={change.was} steps={change.steps}
                      text={change.kind === "added" ? change.added : change.kind === "changed" ? change.before + change.added + change.after : change.text}>
                      <ChangedLine change={change} />
                    </UpdateLine>
                    : <ChangedLine change={change} />}
              </span>
            </a>
          </Fragment>
        );
      })}
      {folded > 0 ? <UpdateChangesMore count={folded} onOpen={() => setOpen(true)} /> : null}
      {rest > 0 ? <span className={styles.activityChangesRest}>{t("updatesPanel:inbox.activity.otherChanges", { count: rest })}</span> : null}
    </div>
  );
}
