import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";

/*
 * A full-page database's name typed here, laid over what the page list reads
 * of its page until the server names the page after it. The name goes to the
 * database — its source, or its block where the page's title is the
 * database's own — through the page's content writes, collected until the
 * typing pauses (nativeContentGraphWriteCoordinator.ts), and the server
 * titles the page from it once it is saved (full_page_database_name.go,
 * database_page_name_spans.go). A list read meanwhile — the one the page's
 * own making asks for once the server has it — listed the page by its name
 * from before, and the page's title, its window and the sidebar lost the
 * name typed until the server answered: the title, its caret left then by
 * "+ 새 페이지" or a press anywhere, showed blank (2026-11-26, a write held
 * 1.5 s; on a server cut off, until it came back). A read takes the name as
 * it takes a title the page outbox keeps (withKeptPageChanges): while its
 * write is out, and once it is answered while a read still lists the page by
 * the name it was typed over — a read begun before the answer.
 */
type Kept = { title: string; titleSpans?: TextSpan[]; over: string; answered: boolean };

const kept = new Map<string, Kept>();

/**
 * The name typed for a full-page database's page, `written` its write: kept
 * for the list's reads (withDatabaseNamesKept) and shown in the list at once
 * (`show`, the page projected with it, as the page's title is typed).
 */
export function nameTypedForPage(page: Page, title: string, spans: readonly TextSpan[] | undefined, written: void | Promise<boolean>, show?: (page: Page) => void) {
  const was = kept.get(page.id);
  // The name it was typed over is the one listed before this run of typing was answered.
  const name: Kept = { title, ...(spans?.length ? { titleSpans: [...spans] } : {}), over: was && !was.answered ? was.over : page.title, answered: false };
  kept.set(page.id, name);
  const settle = (saved: boolean) => {
    if (kept.get(page.id) !== name) return;
    if (saved) name.answered = true;
    else kept.delete(page.id);
  };
  if (written) void written.then(settle, () => settle(false));
  else settle(true);
  show?.({ ...page, title });
}

/** A list read with the database names typed here over what it read of their pages (nameTypedForPage). */
export function withDatabaseNamesKept<List extends { pages: Page[] }>(list: List): List {
  if (!kept.size) return list;
  let changed = false;
  const pages = list.pages.map((page) => {
    const name = kept.get(page.id);
    if (!name) return page;
    // Answered, and read by a list that has it, or what came after it: the list's stands.
    if (name.answered && page.title !== name.over) {
      kept.delete(page.id);
      return page;
    }
    if (page.title === name.title && (!name.titleSpans || JSON.stringify(page.titleSpans) === JSON.stringify(name.titleSpans))) return page;
    changed = true;
    return { ...page, title: name.title, ...(name.titleSpans ? { titleSpans: name.titleSpans } : {}) };
  });
  return changed ? { ...list, pages } : list;
}
