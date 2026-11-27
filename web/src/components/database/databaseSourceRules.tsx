import { createContext, useContext, useMemo, type ReactNode } from "react";
import type { GraphDataSource } from "@/contentGraph";
import { DatabaseRowsAddableContext, DatabaseSourcePropertiesOnlyContext } from "./databaseNewRowControl";
import { DatabaseFixedPropertiesContext } from "./untitledRowName";

type SourceRules = { addable: boolean; propertiesOnly: boolean };

// The rules around a view before its source's own were laid on them, for what it opens that is no view of its source.
const OuterSourceRulesContext = createContext<SourceRules | null>(null);

/**
 * What a data source lets every view of it do, decided here for each place
 * that shows one — a database's own page, a linked view on any page, a
 * dashboard's widget, People's page, a phone's (NativeDatabaseViewMount): on
 * top of what the place says, People's source (`peopleDatabase`, which the
 * server reads, people_database_rules.go) takes no new page — no "새로
 * 만들기", no "+ 새 페이지", no layout's "+" — its data source settings are
 * its "속성 편집" alone, and its own five properties (`fixedPropertyIds`)
 * keep their name, kind and settings, their values as its read gives them and
 * written as peopleSourceCells.ts says, wherever a cell, a row's page or a
 * step of ⌘Z writes them. Notion's linked view of People shows
 * no add button and "속성 편집" alone under its data source settings (seen
 * 2026-11-19 in Chrome at 1440, its Business-trial owner), as its People page
 * does (seen 2026-11-15).
 */
export function DatabaseSourceRules({ source, children }: {
  source: Pick<GraphDataSource, "peopleDatabase" | "fixedPropertyIds">;
  children: ReactNode;
}) {
  const addable = useContext(DatabaseRowsAddableContext), propertiesOnly = useContext(DatabaseSourcePropertiesOnlyContext);
  const fixed = useContext(DatabaseFixedPropertiesContext);
  const outer = useMemo(() => ({ addable, propertiesOnly }), [addable, propertiesOnly]);
  const people = source.peopleDatabase === true;
  const ownFixed = source.fixedPropertyIds;
  const kept = useMemo(() => ownFixed?.length ? new Set([...fixed, ...ownFixed]) : fixed, [fixed, ownFixed]);
  return (
    <OuterSourceRulesContext.Provider value={outer}>
      <DatabaseRowsAddableContext.Provider value={addable && !people}>
        <DatabaseSourcePropertiesOnlyContext.Provider value={propertiesOnly || people}>
          <DatabaseFixedPropertiesContext.Provider value={kept}>{children}</DatabaseFixedPropertiesContext.Provider>
        </DatabaseSourcePropertiesOnlyContext.Provider>
      </DatabaseRowsAddableContext.Provider>
    </OuterSourceRulesContext.Provider>
  );
}

/**
 * What a view opens that is no view of its source — a row's page beside it
 * or in the middle, the databases on that page — under the rules around the
 * view, not its source's: a person's page opened from a linked People view
 * takes new rows in its own databases. Its row's properties are still its
 * source's, so the ones it keeps stay kept there (they name no other's).
 */
export function OutsideDatabaseSourceRules({ children }: { children: ReactNode }) {
  const outer = useContext(OuterSourceRulesContext);
  if (!outer) return <>{children}</>;
  return (
    <DatabaseRowsAddableContext.Provider value={outer.addable}>
      <DatabaseSourcePropertiesOnlyContext.Provider value={outer.propertiesOnly}>{children}</DatabaseSourcePropertiesOnlyContext.Provider>
    </DatabaseRowsAddableContext.Provider>
  );
}
