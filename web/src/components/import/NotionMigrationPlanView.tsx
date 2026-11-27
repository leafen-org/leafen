import { i18next } from "@/i18n";
import { useTranslation } from "react-i18next";

import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import type {
  NativeNotionMigration,
  NativeNotionMigrationIssue,
  NativeNotionMigrationPlan,
  NativeNotionMigrationPlanProblem,
  NativeNotionMigrationPlanProperty,
  NativeNotionMigrationPlanState,
  NativeNotionMigrationProperty,
  NativeNotionMigrationSummary,
} from "../../notionImportWire.generated";
import { nativeNotionMigrationPlanProblems } from "../../notionImportWire.generated";
import { localizedPropertyTypeLabel } from "../database/propertyTypes";
import type { PropertyType } from "../../lib/types";
import { useProductPages } from "../product/productPagesContext";
import styles from "./NotionMigrationWindow.module.css";

// The plan, read only: where it goes, the
// problems that keep it from running, and each database's line with its
// properties' mapping — for a database of several data sources, each data
// source's under its name, as Notion keeps each source's properties apart.
// The agenda is the draft's "그대로 옮길 수 없는 것" over it, said once
// (2026-11-07: it came again at the plan's end). It is changed in the talk
// with the agent or by approving the draft, never here.

const problemCodes = new Set<string>(Object.values(nativeNotionMigrationPlanProblems));

const agendaCodes = [
  "property_type_unsupported", "formula_function_unsupported", "formula_reference_unresolved", "database_views_unavailable",
  "database_unavailable", "page_unavailable", "blocks_unavailable", "unsupported_block_type", "database_source_unavailable", "svg_file_cleaned",
  "property_setting_omitted", "formats_not_read", "person_not_member", "sharing_not_moved", "history_not_moved", "automations_not_moved",
  "comments_unreadable", "view_filter_deleted_property", "view_filters_unread", "table_row_blocks_omitted", "computed_kept_beside",
] as const;

/** The scan's issues to settle, counted by reason. */
export function NotionMigrationAgendaList({ issues, className }: { issues: readonly NativeNotionMigrationIssue[]; className?: string }) {
  const { t } = useTranslation("importDialog");
  const counts = new Map<string, number>();
  for (const issue of issues) counts.set(issue.code, (counts.get(issue.code) ?? 0) + 1);
  return (
    <ul className={className} data-notion-migration-agenda-list>
      {[...counts.entries()].map(([code, count]) => (
        <li key={code} data-notion-migration-issue={code}>
          {(agendaCodes as readonly string[]).includes(code)
            ? t(`importDialog:migration.issue.${code as typeof agendaCodes[number]}`, { count })
            : t("importDialog:migration.issue.other", { code, count })}
        </li>
      ))}
    </ul>
  );
}

const changeKinds = ["renamed", "skipped", "merged", "split", "properties"] as const;

/** What a saved plan changes from the scan's draft, counted by kind; only the
 * agent changes a plan (approving the draft saves it as it is). */
function notionMigrationPlanChanges(plan: NativeNotionMigrationPlan, draft: NativeNotionMigrationPlan) {
  const counts = { renamed: 0, skipped: 0, merged: 0, split: 0, properties: 0, settings: false };
  for (const database of plan.databases) {
    const before = draft.databases.find((candidate) => candidate.sourceId === database.sourceId);
    if (database.action === "skip" && before?.action !== "skip") counts.skipped += 1;
    if (database.action === "merge" && before?.action !== "merge") counts.merged += 1;
    if (database.action === "import" && database.title && database.title !== (before?.title ?? "")) counts.renamed += 1;
    if (database.split.length && !before?.split.length) counts.split += 1;
    const sources = [{ sourceId: "", properties: database.properties }, ...database.dataSources ?? []];
    for (const source of sources) {
      const was = source.sourceId
        ? before?.dataSources?.find((candidate) => candidate.sourceId === source.sourceId)?.properties
        : before?.properties;
      for (const property of source.properties) {
        const wasProperty = was?.find((candidate) => candidate.sourceId === property.sourceId);
        if (!wasProperty || JSON.stringify(wasProperty) !== JSON.stringify(property)) counts.properties += 1;
      }
    }
  }
  for (const page of plan.pages) {
    const before = draft.pages.find((candidate) => candidate.sourceId === page.sourceId);
    if (page.action === "skip" && before?.action !== "skip") counts.skipped += 1;
    if (page.action === "import" && page.title && page.title !== (before?.title ?? "")) counts.renamed += 1;
  }
  counts.settings = plan.destinationPageId !== draft.destinationPageId || plan.comments !== draft.comments
    || plan.files !== draft.files || JSON.stringify(plan.people) !== JSON.stringify(draft.people);
  return counts;
}

/** What the agent changed in a saved plan, said as a list, or [] when it is
 * the draft as it is. */
export function useNotionMigrationPlanChangeList(state: NativeNotionMigrationPlanState | null) {
  const { t } = useTranslation("importDialog");
  const changes = state?.plan && state.draft ? notionMigrationPlanChanges(state.plan, state.draft) : null;
  return changes ? [
    ...changeKinds.filter((kind) => changes[kind] > 0)
      .map((kind) => t(`importDialog:plan.change.${kind}`, { count: changes[kind] })),
    ...(changes.settings ? [t("importDialog:plan.change.settings")] : []),
  ] : [];
}

function typeLabel(type: string) {
  const leafenType = type === "people" ? "person" : type;
  return i18next.exists(`propertyTypes:labels.${leafenType}`) ? localizedPropertyTypeLabel(leafenType as PropertyType) : type;
}

const sourceKey = (id: string) => id.replaceAll("-", "").toLowerCase();

/** The scan's database, or data source after a database's first
 * ("<database>:source:<data source>"), a source ID names. */
function scannedSource(summary: NativeNotionMigrationSummary, sourceId: string) {
  const [databaseId = "", dataSourceId] = sourceId.split(":source:");
  const database = summary.databases.find((candidate) => sourceKey(candidate.id) === sourceKey(databaseId));
  if (!database || dataSourceId === undefined) return database;
  const dataSource = database.dataSources?.find((candidate) => sourceKey(candidate.id) === sourceKey(dataSourceId));
  return dataSource ? { title: dataSource.name, properties: dataSource.properties } : undefined;
}

/** The name the scan gave a source a problem or agenda item is about. */
function sourceName(summary: NativeNotionMigrationSummary, issue: NativeNotionMigrationIssue) {
  const source = scannedSource(summary, issue.sourceId);
  if (source && issue.propertyId) {
    return source.properties.find((property) => property.id === issue.propertyId)?.name ?? issue.propertyId;
  }
  return source?.title || issue.sourceTitle || issue.sourceId;
}

/** One data source's properties, each as the plan maps it. */
function PlanPropertiesTable({ head, caption, planned, scanned }: {
  head: string;
  /** The data source's name, for a database of several. */
  caption?: string;
  planned: readonly NativeNotionMigrationPlanProperty[];
  scanned: readonly NativeNotionMigrationProperty[];
}) {
  const { t } = useTranslation("importDialog");
  return (
    <table className={styles.planProperties} data-notion-migration-plan-source={caption}>
      {caption ? <caption>{t("importDialog:plan.dataSource", { name: caption })}</caption> : null}
      <thead>
        <tr><th>{head}</th><th>{t("importDialog:plan.leafen")}</th></tr>
      </thead>
      <tbody>
        {planned.map((property) => {
          const original = scanned.find((candidate) => candidate.id === property.sourceId);
          return (
            <tr key={property.sourceId} data-notion-migration-plan-property={property.action}>
              <td>{original?.name ?? property.sourceId} <small>{original ? typeLabel(original.type) : ""}</small></td>
              <td>
                {property.action === "report" ? <em>{t("importDialog:plan.reportOnly")}</em> : (
                  <>
                    {property.name || original?.name} <small>{typeLabel(property.type || original?.leafenType || "rich_text")}</small>
                    {property.action === "preserve" ? <em>{t("importDialog:plan.preserved")}</em> : null}
                    {property.values.length ? <small>{t("importDialog:plan.values", { count: property.values.length })}</small> : null}
                  </>
                )}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

export function NotionMigrationPlanView({ migration, summary, state, workspaceId }: {
  migration: NativeNotionMigration;
  /** The scan, read a window at a time (useNotionMigrationDraft). */
  summary: NativeNotionMigrationSummary | null;
  state: NativeNotionMigrationPlanState;
  workspaceId: string;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["importDialog", "common"]);
  const productPages = useProductPages();
  const plan = state.plan ?? state.draft;
  if (!summary || !plan) return null;
  const problems = state.plan ? state.check?.problems ?? [] : [];
  const place = plan.destinationPageId
    ? (productPages?.pages ?? []).find((page) => page.id === plan.destinationPageId && page.workspaceId === workspaceId)
    : null;
  const titleOf = (id: string) => summary.databases.find((database) => database.id === id)?.title || untitledPageDisplayTitle("database");
  const head = migration.source || t("importDialog:plan.notion");
  return (
    <div className={styles.plan} data-notion-migration-plan={state.plan ? "saved" : "draft"}>
      {plan.destinationPageId ? <span>{t("importDialog:plan.place", { place: place ? pageNames.title(place) : t("importDialog:migration.privatePages") })}</span> : null}
      {problems.length ? (
        <div className={styles.problems} role="alert" data-notion-migration-plan-problems={problems.length}>
          <strong>{t("importDialog:plan.problems", { count: problems.length })}</strong>
          <ul>
            {problems.map((problem, index) => (
              <li key={`${problem.code}:${problem.sourceId}:${problem.propertyId}:${index}`}>
                {problemCodes.has(problem.code)
                  ? t(`importDialog:plan.problem.${problem.code as NativeNotionMigrationPlanProblem}`, { name: sourceName(summary, problem) })
                  : t("importDialog:migration.issue.other", { code: problem.code, count: 1 })}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {plan.databases.map((database) => {
        const source = summary.databases.find((candidate) => candidate.id === database.sourceId);
        const from = source?.title || untitledPageDisplayTitle("database");
        return (
          <details key={database.sourceId} className={styles.planDatabase} data-notion-migration-plan-database={database.action}>
            <summary>
              <span>{from}</span>
              {database.action === "import" && database.title && database.title !== from ? <span>{`→ ${database.title}`}</span> : null}
              <small>{database.action === "merge"
                ? t("importDialog:plan.action.merge", { into: titleOf(database.into) })
                : t(`importDialog:plan.action.${database.action}`)}</small>
            </summary>
            {database.action !== "skip" ? (
              <>
                <PlanPropertiesTable head={head} planned={database.properties} scanned={source?.properties ?? []}
                  caption={source?.dataSources?.length ? source.sourceName || untitledPageDisplayTitle("database") : undefined} />
                {(database.dataSources ?? []).map((planned) => {
                  const scanned = source?.dataSources?.find((candidate) => sourceKey(candidate.id) === sourceKey(planned.sourceId));
                  return (
                    <PlanPropertiesTable key={planned.sourceId} head={head} planned={planned.properties} scanned={scanned?.properties ?? []}
                      caption={scanned?.name || untitledPageDisplayTitle("database")} />
                  );
                })}
              </>
            ) : null}
          </details>
        );
      })}
    </div>
  );
}
