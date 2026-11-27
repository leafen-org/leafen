type TemplateLike = { id: string; isDefault: boolean; deleted?: boolean };
type ViewLike = { config: Readonly<Record<string, unknown>> };

/** Where a view keeps a default of its own: a template's id, or "" for the empty page. */
export const VIEW_DEFAULT_TEMPLATE = "defaultTemplateId";

/**
 * The template a view makes its new pages from, as Notion's (seen
 * 2026-11-03): the view's own default when it has one ("" the empty page),
 * which wins over the database's; else the database's. A view's own default
 * whose template has gone falls back to the database's. Undefined is the
 * empty page.
 */
export function viewDefaultTemplate<Template extends TemplateLike>(templates: readonly Template[], view: ViewLike): Template | undefined {
  const own = view.config[VIEW_DEFAULT_TEMPLATE];
  if (own === "") return undefined;
  const chosen = typeof own === "string" ? templates.find((template) => template.id === own && !template.deleted) : undefined;
  return chosen ?? templates.find((template) => template.isDefault && !template.deleted);
}

/**
 * A view's default as the making of its new page names it: its template —
 * named before the templates were read, one its database's default stands
 * for once gone (`orDefault`), as viewDefaultTemplate falls back — or none,
 * its database's default as the server names it then.
 */
export type NamedPageTemplate = { templateId?: string; orDefault?: true };

/**
 * The template a view's new page starts from as it is made, given its
 * database's templates as read here (viewDefaultTemplate), or null while
 * they aren't read yet — its toolbar reads them after the view draws, never
 * while the server is out of reach: then the view's own default when it has
 * one ("" the empty page), else its database's default as the server names it
 * as it makes the page (no `templateId`), which leaves a database without one
 * as it was. Taken for the empty page, a row made before that read ("+ 새
 * 작업" pressed as the page opened, or offline) never took the default
 * (2026-11-25). A view's own default deleted before that read is not known
 * here: the server takes its database's default instead as it makes the page
 * (content_graph_row_templates.go), as it would here once read — named
 * alone, it left the row unfilled (2026-11-25). Null is the empty page.
 */
export function newPageTemplate(templates: readonly TemplateLike[] | null, view: ViewLike): NamedPageTemplate | null {
  if (templates) {
    const chosen = viewDefaultTemplate(templates, view);
    return chosen ? { templateId: chosen.id } : null;
  }
  const own = view.config[VIEW_DEFAULT_TEMPLATE];
  if (own === "") return null;
  return typeof own === "string" ? { templateId: own, orDefault: true } : {};
}

/**
 * The template a view's new pages start from as something kept names it — a
 * sidebar shortcut "사이드바에 단축키 추가" makes — given its database's
 * templates as read (null not yet): its id, "" the empty page; null while it
 * can't be told, the templates unread and the view not keeping the empty page
 * as its own. Kept as the empty page then, a shortcut added as the page
 * opened, or offline, made its pages without the default for good (2026-11-25).
 */
export function keptPageTemplate(templates: readonly TemplateLike[] | null, view: ViewLike): string | null {
  if (templates) return viewDefaultTemplate(templates, view)?.id ?? "";
  return view.config[VIEW_DEFAULT_TEMPLATE] === "" ? "" : null;
}

/** The view's settings with a default of its own ("" the empty page), or none (null) to follow the database's. */
export function withViewDefaultTemplate<View extends ViewLike>(view: View, templateId: string | null): View {
  const config: Record<string, unknown> = { ...view.config };
  if (templateId === null) delete config[VIEW_DEFAULT_TEMPLATE];
  else config[VIEW_DEFAULT_TEMPLATE] = templateId;
  return { ...view, config };
}
