"use client";
import { outOfTrashIn, pagesByIdOf } from "@/lib/pageIndex";

import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/api";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { viewDisplayName } from "@/components/database/databaseViewShared";
import { inlineDatabasePagesOf, pagesWithHeldDatabases } from "@/components/editor/inlineDatabasePages";
import { useProductPages } from "@/components/product/productPagesContext";
import { usePageNames } from "@/lib/pageNames";
import { newId } from "@/lib/ids";
import type { AutomationAction, DbView, Page } from "@/lib/types";
import type { Page as ListedPage } from "@/types";
import { publicHttpUrl } from "./automationActionModel";
import { formPlace, useChosenRowPage, type FoundForm } from "./automationOpenTargetChoices";
import { AutomationOpenTargetList } from "./AutomationOpenTargetList";
import styles from "./automationActionEditor.module.css";
import chipStyles from "./automationHeadChip.module.css";

type OpenAction = Extract<AutomationAction, { type: "open_page" | "open_form" | "open_url" }>;

/**
 * "페이지, 양식 또는 URL 열기", as Notion's (seen 2026-11-02): one action
 * whose chip, asking for a page, form or link until one is chosen, opens a 280px
 * list (AutomationOpenTargetList) — a field to search or paste a link into,
 * the workspace's pages under "페이지" with where each sits, the databases
 * their bodies hold among them, which the action opens alone, and database
 * rows — and, for the words typed, the forms found under "폼"; a link pasted
 * offers itself first. What is chosen makes the action a page's, a form's or
 * a link's, as saved ones already are.
 */
export function AutomationOpenTargetPicker({ action, number, views, database, warn = false, onChange }: {
  action: OpenAction;
  number: number;
  /** Whether a missing choice shows red, as Notion's chip on an action saved so (seen 2026-11-09), and what it says. */
  warn?: boolean;
  /** The database's own form views, offered first; the workspace's others are searched for. */
  views: readonly DbView[];
  /** The database the actions belong to, as the editor decides it: its "이 페이지" and forms; none for a page's button. */
  database?: Page;
  onChange(action: AutomationAction): void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("databaseView");
  const product = useProductPages();
  const pages = product?.pages;
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const pagesById = pagesByIdOf(pages ?? []);
  const inlineDatabases = product?.inlineDatabases;
  // The databases pages hold in their bodies are pages here too, each after its page, its block's ID the one
  // opened (inlineDatabasePagesOf) — a full-page one is listed as its page.
  const held = useMemo(() => inlineDatabasePagesOf(inlineDatabases, pagesById), [inlineDatabases, pagesById]);
  // None in the trash or under a page in it (outOfTrashIn).
  const listed = useMemo(() => {
    const outOfTrash = outOfTrashIn(pages ?? []);
    return pagesWithHeldDatabases(pages ?? [], held).filter((page) => outOfTrash(page));
  }, [held, pages]);
  const needle = query.trim().toLowerCase();
  const url = publicHttpUrl(query.trim()) ? query.trim() : "";
  // The workspace's forms the person can read, as Notion's list has them (seen 2026-11-02),
  // read while the list is open or a form chosen elsewhere needs its name.
  const [found, setFound] = useState<FoundForm[]>([]);
  const ownChosen = action.type === "open_form" && views.some((view) => view.id === action.viewId);
  const wantForms = open || (action.type === "open_form" && !ownChosen);
  useEffect(() => {
    const session = product?.session, workspaceId = product?.workspaceId;
    if (!wantForms || !session || !workspaceId) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      api.formViewPicker(session, workspaceId, open ? query.trim() : "", { signal: controller.signal })
        .then((result) => { if (!controller.signal.aborted) setFound(result.items); }, () => undefined);
    }, 150);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [open, product?.session, product?.workspaceId, query, wantForms]);
  const formName = (view: DbView) => formTitleOf(view) || viewDisplayName(view);
  const placeOfForm = (form: FoundForm) => formPlace(form, pagesById, held, (page) => pageNames.title(page));
  // Forms are found by the words typed, none listed before, as Notion's list has "폼" only for a search (seen
  // 2026-11-24 in two tabs, the database's own form too). The own database's forms come first: those of its views on
  // that database alone, a view of another source in the same block being that source's form, found among the
  // workspace's below. Each ends its place at its database (formPlace).
  const ownForms = !database || !needle ? [] : views.filter((view) => view.type === "form" && view.databaseId === database.id
    && formName(view).toLowerCase().includes(needle))
    .map((view) => {
      const read = found.find((form) => form.viewId === view.id);
      return { viewId: view.id, name: formName(view), databaseId: view.databaseId, where: read ? placeOfForm(read) : [pageNames.title(database)] };
    });
  const forms = !needle ? [] : [...ownForms, ...found.filter((form) => !ownForms.some((own) => own.viewId === form.viewId)).map((form) => ({
    viewId: form.viewId, name: viewDisplayName({ name: form.name, type: "form" }), databaseId: form.dataSourceId, where: placeOfForm(form),
  }))];
  const thisPage = t("openTargetThisPage");
  // "이 페이지" is the page a database's button or automation runs on; a page's
  // own button has none to offer, as Notion's list (seen 2026-11-05; 2026-11-24
  // on a page holding a two-source database, its second source's button offering it).
  const thisPageShown = Boolean(database) && (!needle || thisPage.toLowerCase().includes(needle));
  // A row chosen here is named by the page it was chosen with; one chosen before, as the server reads it.
  const [rowChosen, setRowChosen] = useState<ListedPage | null>(null);
  const chosenId = action.type === "open_page" ? action.pageId : "";
  const listedPage = chosenId ? pagesById[chosenId] ?? held.get(chosenId) ?? (rowChosen?.id === chosenId ? rowChosen : undefined) : undefined;
  const rowRead = useChosenRowPage(product?.session, product?.workspaceId, chosenId && !listedPage ? chosenId : "");
  const chosenPage = listedPage ?? rowRead ?? undefined;
  const chosen = action.type === "open_page" ? (action.target === "trigger_page" ? thisPage
    : chosenPage ? pageNames.title(chosenPage) : "")
    : action.type === "open_form" ? (views.find((view) => view.id === action.viewId) ? viewDisplayName(views.find((view) => view.id === action.viewId)!)
      : found.find((form) => form.viewId === action.viewId) ? viewDisplayName({ name: found.find((form) => form.viewId === action.viewId)!.name, type: "form" }) : "")
      : action.url;
  const close = () => { setOpen(false); setQuery(""); };
  // A page or form chosen in place of another keeps where it opens, as Notion's (seen 2026-11-06).
  const kept = action.type !== "open_url" ? action.openIn : undefined;
  const choose = (next: AutomationAction, rowPage?: ListedPage) => {
    if (rowPage) setRowChosen(rowPage);
    onChange(kept && (next.type === "open_page" || next.type === "open_form") ? { ...next, openIn: kept } : next);
    close();
  };
  const id = action.id || newId();
  return <>
    <button ref={button} type="button" className={`${styles.targetButton} ${chipStyles.chip}`} aria-label={t("openTargetForAction", { number })}
      aria-haspopup="dialog" aria-expanded={open} aria-invalid={warn || undefined}
      data-automation-open-target onClick={() => open ? close() : setOpen(true)}>
      {warn ? <WarningFillIcon size="tiny" aria-hidden="true" /> : null}<span>{chosen || t("chooseOpenTarget")}</span><ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {/* Too tall for the room below, it rises over its chip to show whole, as Notion's (seen 2026-11-05). */}
    {open && product?.session && product.workspaceId ? <AutomationOpenTargetList session={product.session} workspaceId={product.workspaceId}
      triggerRef={button} number={number} label={t("openTargetForAction", { number })} listed={listed} pagesById={pagesById}
      query={query} onQuery={setQuery} url={url} thisPage={thisPageShown ? thisPage : ""} forms={forms} action={action} id={id}
      onChoose={choose} onClose={close} /> : null}
  </>;
}

/** A form view's own title, which names it where one is set (Notion's open list, seen 2026-11-05). */
function formTitleOf(view: DbView): string {
  const form = view.config?.leafenForm as { title?: unknown } | undefined;
  return typeof form?.title === "string" ? form.title.trim() : "";
}
