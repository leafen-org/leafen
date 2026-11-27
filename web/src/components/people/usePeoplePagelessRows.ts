import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DatabasePagelessRows } from "@/components/database/databasePagelessRows";
import { editorName } from "@/lib/peopleDirectory";
import { personPageHref } from "@/lib/peopleProfiles";
import { rowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";

// The kinds of property whose value someone writes, not one worked out where
// it shows (a formula, a rollup) or kept by the row itself (its times, its
// editors, its ID), nor a button, which is pressed.
const WRITTEN: ReadonlySet<string> = new Set([
  "rich_text", "number", "select", "multi_select", "status", "date", "person", "checkbox", "url", "email", "phone", "place",
  "files", "relation",
]);
const NONE: ReadonlySet<string> = new Set();

/**
 * People's rows' menu (databasePagelessRows.ts), as Notion's People shows
 * its owner (seen 2026-11-15 twice in a free test workspace): "속성 편집 ›"
 * lists About — not a deactivated person's — and the owner's own properties
 * that hold a value (not an ID, its times, its editors or a formula), none for
 * anyone else, and so does the selection bar, About alone a lone button it
 * leaves out (selectionBarProperties); "링크 복사" copies the person's page
 * (personPageHref); the foot names when the row was last edited and by whom — Leafen,
 * which made it (LEAFEN_SYSTEM_USER_ID), with its mark where Notion's says
 * "Notion App" — then its name's words and letters.
 */
export function usePeoplePagelessRows({ rows, properties, nameId, aboutId, writes, readerId }: {
  rows: readonly GraphRow[];
  /** Its owner's own properties. */
  properties: readonly GraphProperty[];
  nameId: string;
  aboutId: string;
  /** Whether the reader writes values, its owner. */
  writes: boolean;
  readerId: string;
}): DatabasePagelessRows {
  const { t } = useTranslation("blockHandle");
  return useMemo(() => {
    const byId = new Map(rows.map((row) => [row.id, row]));
    const values = properties.filter(({ type }) => WRITTEN.has(type)).map(({ id }) => id);
    const withAbout: ReadonlySet<string> = new Set([aboutId, ...values]);
    const withoutAbout: ReadonlySet<string> = new Set(values);
    return {
      writable: (rowId) => {
        const row = byId.get(rowId);
        if (!writes || !row) return NONE;
        return row.readOnlyValues?.includes(aboutId) ? withoutAbout : withAbout;
      },
      link: personPageHref,
      foot: (rowId) => {
        const row = byId.get(rowId);
        if (!row) return undefined;
        const name = row.values[nameId]?.value;
        const stats = rowMenuDocumentStats([typeof name === "string" ? name : ""], 0);
        const editorId = row.lastEditedBy || undefined;
        return {
          at: row.updatedAt ?? "",
          counts: t("footer.counts", { words: stats.words, chars: stats.characters }),
          ...(editorId ? { name: editorName(editorId, readerId), editorId } : {}),
        };
      },
    };
  }, [aboutId, nameId, properties, readerId, rows, t, writes]);
}
