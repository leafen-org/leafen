import { useTranslation } from "react-i18next";

import { pageHref } from "@/lib/navigation";
import {
  nativeNotionImportBlockIssueReasons,
  type NativeNotionImportJob,
  type NativeNotionImportNotWhole,
  type NativeNotionImportNotWholePage,
} from "@/notionImportWire.generated";
import styles from "./ImportDialog.module.css";
import own from "./NotionImportNotWhole.module.css";
import { listedPageNameKind, untitledPageDisplayTitle } from "../../lib/pageTitle";

type NotionImportBlockIssuesProps = {
  job: NativeNotionImportJob;
  /** Another tool's import names that tool where Notion's names Notion (2026-11-07: an Evernote import's said "Notion"). */
  source?: string;
};

// The reasons whose issue names a block by its type, said by the type's name:
// one Notion gives as "unsupported", or of a type with no name here, is an
// unknown kind, never its key (2026-11-20, as the report page says it).
const blockReasons = new Set<string>([
  nativeNotionImportBlockIssueReasons.unsupportedBlock, nativeNotionImportBlockIssueReasons.invalidBlock,
  nativeNotionImportBlockIssueReasons.settingOmitted, nativeNotionImportBlockIssueReasons.textLinkOmitted,
  nativeNotionImportBlockIssueReasons.textReferenceUnresolved, nativeNotionImportBlockIssueReasons.titleShortened,
  nativeNotionImportBlockIssueReasons.textReferenceOutside, nativeNotionImportBlockIssueReasons.tableRowBlocksOmitted,
  nativeNotionImportBlockIssueReasons.linkUnresolved, nativeNotionImportBlockIssueReasons.linkOutside,
  nativeNotionImportBlockIssueReasons.syncedUnresolved, nativeNotionImportBlockIssueReasons.nestedPageMoved,
]);

// What an item of the tally counts: by its kind, and for a problem a
// database's views have as a whole, or one of its data sources, that instead
// — as the agent's status and the report page say it (notion_import_not_whole.go).
function notWholeUnit(item: NativeNotionImportNotWhole) {
  switch (item.reason) {
    case nativeNotionImportBlockIssueReasons.viewsUnavailable:
    case nativeNotionImportBlockIssueReasons.viewRowOrderUnavailable:
    case nativeNotionImportBlockIssueReasons.databaseUnavailable:
      return "database";
    case nativeNotionImportBlockIssueReasons.dataSourceUnavailable:
      return "data_source";
    case nativeNotionImportBlockIssueReasons.linkedSourceOutside:
    case nativeNotionImportBlockIssueReasons.linkedSourceUnavailable:
      if (item.kind === "block") return "linked_database";
  }
  return item.kind;
}

/**
 * The first pages an item was on, each linked — a template as the template —
 * with what was there when it has a name —
 * a block by its kind, a property or view by its own; one with
 * none, a linked database in a row page say, is what the line counts — and
 * how many more (2026-11-23: a move said its 672 row pages only in the
 * report, each "Untitled"; notion_import_issue_places.go).
 */
function NotWholePages({ item }: { item: NativeNotionImportNotWhole }) {
  const { t } = useTranslation(["importDialog"]);
  const pages: NativeNotionImportNotWholePage[] = item.pages ?? [];
  if (!pages.length) return null;
  const rest = Math.max(0, (item.pagesTotal || pages.length) - pages.length);
  // A template is named as the template.
  const title = (page: NativeNotionImportNotWholePage) => page.template
    ? (page.pageTitle.trim()
      ? t("importDialog:blockIssues.notWhole.template", { name: page.pageTitle })
      : t("importDialog:blockIssues.notWhole.templateUnnamed"))
    : page.pageTitle || untitledPageDisplayTitle(listedPageNameKind(page));
  const what = (page: NativeNotionImportNotWholePage) => blockReasons.has(item.reason)
    ? t(`importDialog:blockIssues.blockTypes.${page.subject}`, { defaultValue: t("importDialog:blockIssues.blockTypes.unsupported") })
    : page.subject;
  return (
    <span data-notion-not-whole-pages>
      {": "}
      {pages.map((page, index) => (
        <span key={`${index}\u0000${page.pageId}`}>
          {index > 0 ? ", " : null}
          {page.pageId ? <a href={pageHref(page.pageId)}>{title(page)}</a> : title(page)}
          {what(page) ? ` · ${what(page)}` : null}
          {page.count > 1 ? ` (${page.count})` : null}
        </span>
      ))}
      {rest > 0 ? ` ${t("importDialog:blockIssues.notWhole.morePages", { count: rest })}` : null}
    </span>
  );
}

/** Under one of the three counts, each of what it counts and why, in the words the agent and the report say it in, and the first pages it was on. */
function NotWholeItems({ items }: { items: NativeNotionImportNotWhole[] }) {
  const { t } = useTranslation(["importDialog"]);
  if (!items.length) return null;
  return (
    <ul className={own.items} data-notion-not-whole={items[0].kind}>
      {items.map((item) => (
        <li key={`${item.reason}\u0000${item.source ?? ""}`} data-notion-not-whole-reason={item.reason}>
          {t(`importDialog:blockIssues.notWhole.${notWholeUnit(item)}`, {
            count: item.count,
            // One showing a database outside the import names it, one line for each.
            why: item.source
              ? t(`importDialog:blockIssues.notWhole.why.${item.reason}_named`, {
                source: item.source,
                defaultValue: t(`importDialog:blockIssues.notWhole.why.${item.reason}`, { defaultValue: item.reason }),
              })
              : t(`importDialog:blockIssues.notWhole.why.${item.reason}`, { defaultValue: item.reason }),
          })}
          <NotWholePages item={item} />
        </li>
      ))}
    </ul>
  );
}

// Says, for a finished Notion import, the Notion blocks it left out or
// brought over only in part, the database properties that lost settings or
// values, and the database views it could not bring over whole: under each
// count, each why with the first pages it was on; the report lists them all
// (2026-11-06: a real workspace's list by page ran past the window's height).
export function NotionImportBlockIssues({ job, source = "" }: NotionImportBlockIssuesProps) {
  const { t } = useTranslation(["importDialog"]);
  const say = (key: string, notion: string, values: Record<string, unknown> = {}) =>
    t(`importDialog:blockIssues.${source ? key : notion}`, { ...values, name: source });
  const blockTotal = job.report.blockIssues;
  const propertyTotal = job.report.propertyIssues;
  const viewTotal = job.report.viewIssues;
  const total = blockTotal + propertyTotal + viewTotal;
  // Under each count what it is, one tally with the counts (2026-11-19: the
  // window said "views 21" and the agent's report other things by other counts).
  const notWhole = job.report.notWhole ?? [];
  const of = (kind: NativeNotionImportNotWhole["kind"]) => notWhole.filter((item) => item.kind === kind);

  if (total === 0) return null;
  return (
    <div className={styles.omissionNotice}>
      {blockTotal > 0 ? <strong>{say("toolSummary", "summary", { count: blockTotal })}</strong> : null}
      {blockTotal > 0 ? <NotWholeItems items={of("block")} /> : null}
      {propertyTotal > 0 ? (
        <strong>{say("toolPropertySummary", "propertySummary", { count: propertyTotal })}</strong>
      ) : null}
      {propertyTotal > 0 ? <NotWholeItems items={of("property")} /> : null}
      {viewTotal > 0 ? <strong>{say("toolViewSummary", "viewSummary", { count: viewTotal })}</strong> : null}
      {viewTotal > 0 ? <NotWholeItems items={of("view")} /> : null}
      <p className={styles.omissionHint}>{say("toolInReport", "inReport")}</p>
    </div>
  );
}
