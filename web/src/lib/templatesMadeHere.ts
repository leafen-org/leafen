import { emptyContentGraph } from "@/contentGraph";
import type { DatabaseToolbarTemplateMetadataWire } from "@/databaseToolbarConfigWire.generated";
import { graphPositionBetween } from "@/lib/graphPosition";
import { pageCopyTitle } from "@/lib/pageDuplicationService";
import { tellPagesNotYetMade } from "@/lib/pagesNotYetMade";
import { consumePageTitleFocus, dropHeldTyping, holdTypingWhileRowOpens, pageTitleFocusAsked, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { phoneScreen } from "@/lib/screenKind";
import { forgetShownGraph, holdShownGraph } from "@/lib/shownGraphs";

// A database template made from its menu shows, and opens, on the press,
// before the server has made it (user decision 2026-11-12: a press answers
// on the next frame). A template is a page: what asks the server about it —
// its page's read, its body's room — waits until the server has made it
// (pagesNotYetMade), as a row made here does, instead of being refused.
// Its maker writes it meanwhile, as a row made here (rowMadeHere.ts): its
// title and body take typing from its first frame (2026-11-16: drawn
// read-only until the server's read, the name typed right after "새 템플릿"
// on a NAS-class server went nowhere).

const making = new Map<string, Promise<boolean>>();
// The templates made here in this window, whose maker writes their page before the server has read it.
const madeHere = new Set<string>();
const listeners = new Set<() => void>();
tellPagesNotYetMade({
  has: (pageId) => making.has(pageId),
  ready: () => Promise.resolve(),
  subscribe(listener) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  },
});

/**
 * The template is being made: what asks the server about it waits until
 * `made` settles, made or refused (false, or a failure). With its maker
 * (`principalId`) its body draws empty from what this tab holds
 * (shownGraphs.ts), as a row made here does, so it takes typing at once.
 * One without — a template's copy, whose body the server copies — opens to
 * read until its page's read comes, as a row's peek does. Gives `made` back.
 */
export function holdTemplateUntilMade<T>(templateId: string, made: Promise<T>, principalId?: string): Promise<T> {
  const settled = made.then((result) => result !== false, () => false);
  making.set(templateId, settled);
  if (principalId) {
    madeHere.add(templateId);
    holdShownGraph(principalId, templateId, { bootstrapRevision: 0, graphRevision: 0, graph: emptyContentGraph, canEdit: true, canSuggest: false });
  }
  void settled.then((ok) => {
    if (!ok) {
      madeHere.delete(templateId);
      if (principalId) forgetShownGraph(principalId, templateId);
      // Refused before its title took what was typed (a copy's opens to read): it is let go now, as the page won't take it.
      if (consumePageTitleFocus(templateId)) dropHeldTyping();
    }
    if (making.get(templateId) !== settled) return;
    making.delete(templateId);
    for (const listener of [...listeners]) listener();
  });
  return made;
}

/** Settles once a template made here is made or refused; at once for any other. */
export function templateMade(templateId: string): Promise<void> {
  return (making.get(templateId) ?? Promise.resolve(true)).then(() => undefined);
}

type TemplateWire = DatabaseToolbarTemplateMetadataWire;

/**
 * A template's copy as its menus show it, and its page opens, from the press
 * of its "복제" until the server has made it (user decision 2026-11-12: a
 * press answers on the next frame — waiting for the server left the menu as
 * it was for 0.3–0.7 s on a NAS-class server, 2026-11-16): named and placed
 * as the server makes it (duplicateDatabaseToolbarTemplate) — its title's
 * copy, else its name's, right after its template — with its icon, values
 * and repeat. Its body is the server's copy, read once made.
 */
export function shownTemplateCopy(source: TemplateWire, copyId: string, templates: readonly TemplateWire[]): TemplateWire {
  const titled = source.title !== "";
  const after = templates.reduce<string | undefined>((next, { position }) => (
    position > source.position && (next === undefined || position < next) ? position : next), undefined);
  let position = source.position;
  try {
    position = graphPositionBetween(source.position, after);
  } catch {
    // No room between them: shown beside it until the server places it.
  }
  const copy: TemplateWire = { ...source, id: copyId, name: titled ? source.name : pageCopyTitle(source.name),
    title: titled ? pageCopyTitle(source.title) : "", position, isDefault: false, templateAuthorityRevision: 0, deleted: false,
    updatedAt: new Date().toISOString() };
  delete copy.repeatPausedReason;
  if (!source.repeat || !source.repeatNextRunAt) {
    delete copy.repeat;
    delete copy.repeatNextRunAt;
  }
  return copy;
}

/** The templates shown with a template's copy right after it (shownTemplateCopy); as they are once it shows, or without its template. */
export function withTemplateCopy(templates: TemplateWire[], sourceId: string, copyId: string): TemplateWire[] {
  const source = templates.find(({ id }) => id === sourceId);
  if (!source || templates.some(({ id }) => id === copyId)) return templates;
  const copy = shownTemplateCopy(source, copyId, templates);
  return templates.flatMap((template) => template === source ? [template, copy] : [template]);
}

/** Whether the template was made here in this window, its maker writing its page before the server has read it. */
export function templateMadeHere(templateId: string) {
  return madeHere.has(templateId);
}

// How long a template's page is waited for to take its title once it is made.
const TITLE_WAIT_MS = 5000;
// The templates whose title was asked for at the press that made them.
const askedAtPress = new Set<string>();

/** Whether the press that made the template asked for its title (openTemplateAtItsTitle): its page needn't ask again. */
export function templateTitleAskedAtPress(templateId: string) {
  return askedAtPress.has(templateId);
}

/**
 * Makes a template that opens at its title — "새 템플릿", a template's
 * "복제" — as Notion's, whose title takes what is typed right after the
 * press: what is typed from the press on is held, and its page, asked
 * before it is drawn, types it into its title once that can take it, as a
 * new row's page does (holdTypingWhileRowOpens: on a touch screen nothing
 * takes the focus at the press, the keyboard down until its title takes
 * the caret; rowPageTitleCaret.ts). Refused
 * (`make` gives false, or the making held by holdTemplateUntilMade is), or
 * its page not opened by a few seconds after it was made, what was held is
 * let go.
 */
export async function openTemplateAtItsTitle(templateId: string, make: () => Promise<unknown>) {
  const letGo = () => { if (consumePageTitleFocus(templateId)) dropHeldTyping(); };
  let made: unknown;
  try {
    made = await holdTypingWhileRowOpens(() => {
      askedAtPress.add(templateId);
      requestPageTitleFocus(templateId);
      return make();
    });
  } catch {
    letGo();
    return;
  }
  if (made === false || !await (making.get(templateId) ?? Promise.resolve(true))) letGo();
  else if (pageTitleFocusAsked(templateId)) window.setTimeout(() => { if (pageTitleFocusAsked(templateId)) letGo(); }, TITLE_WAIT_MS);
}

/**
 * Makes a template's copy, its "복제", that opens as the copy, and says whether
 * its title takes the caret: at its title's end on a pointer's screen or a
 * tablet's, as a new template does (openTemplateAtItsTitle; Notion's desktop,
 * seen 2026-11-04); on a phone with none, the keyboard down and a letter typed
 * going nowhere, as Notion's phone (seen 2026-11-25 on a Galaxy A90's Chrome at
 * 411 in two tabs: "⋯ › 복제" in its database's templates sheet opened the copy
 * whole, nothing focused).
 */
export function openTemplateCopy(templateId: string, make: () => Promise<unknown>) {
  if (!phoneScreen()) {
    void openTemplateAtItsTitle(templateId, make);
    return true;
  }
  void make().catch(() => undefined);
  return false;
}
