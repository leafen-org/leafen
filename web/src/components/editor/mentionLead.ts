import { searchLeadOf } from "@/components/search/searchLead";
import type { Page } from "@/lib/types";
import type { MentionItem, RowPageMentionCandidate } from "./blockPickerModel";

/** A page or a database row's page the reader visited, as the "@" menu offers it. */
export type MentionVisit = { page: Page; row?: undefined } | { row: RowPageMentionCandidate; page?: undefined };

/**
 * The page links with their lead first, as Notion's "@" and "[[" menus give
 * it (seen 2026-11-24 in two tabs): up to three of the pages and database
 * rows the reader opened lately (useMentionVisits) whose titles answer the
 * words typed — the search window's lead, by its rule (searchLeadOf), the
 * page on screen among them — then the rest the menu found without them, as
 * Notion leaves them out of what it asks its server for. A row the menu's
 * own search found leads as found there. Nothing leads before a word is
 * typed.
 */
export function withMentionLead(found: readonly MentionItem[], visited: readonly MentionVisit[], query: string,
  item: (visit: MentionVisit) => MentionItem): MentionItem[] {
  const lead = searchLeadOf(visited, (visit) => visit.page ? visit.page.title : visit.row.title, query).map(item);
  if (!lead.length) return [...found];
  const leading = new Set(lead.map(({ id }) => id));
  return [...lead, ...found.filter(({ id }) => !leading.has(id))];
}
