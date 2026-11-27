"use client";
import { outOfTrashIn, pagesByIdOf } from "@/lib/pageIndex";

import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { api } from "@/api";
import { PlaceLine } from "@/components/PlaceLine";
import { useProductPages } from "@/components/product/productPagesContext";
import { loadSession } from "@/lib/authSessionStore";
import { usePageNames } from "@/lib/pageNames";
import { pagePlace, type Place } from "@/lib/pagePath";
import styles from "./EnterpriseScopePicker.module.css";
import { ReadFailed } from "@/components/ReadFailed";

export type EnterpriseScopeChoice = { id: string; label: string; detail?: string; place?: Place };
export type EnterpriseScopeKind = "workspace" | "custodian" | "page";

/**
 * What a legal hold or a content search covers, by name: the organization's
 * workspaces and people and the pages of the workspace open behind the
 * settings, searched and added one at a time, each one shown as chosen.
 */
export function EnterpriseScopePicker({ kind, organizationId, workspaces, selected, onChange, label }: {
  kind: EnterpriseScopeKind;
  organizationId: string;
  workspaces: readonly { id: string; name: string }[];
  selected: readonly EnterpriseScopeChoice[];
  onChange(next: EnterpriseScopeChoice[]): void;
  label: string;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("workspaceSettingsDialog");
  const productPages = useProductPages();
  const [query, setQuery] = useState("");
  const [people, setPeople] = useState<EnterpriseScopeChoice[]>([]);
  // A people search that failed: said in the results, with a way to search again — not "결과 없음".
  const [failed, setFailed] = useState(false);
  const [searchTurn, setSearchTurn] = useState(0);

  useEffect(() => {
    if (kind !== "custodian") return;
    const session = loadSession();
    if (!session) return;
    const controller = new AbortController();
    setFailed(false);
    void api.searchOrganizationPeople(session, { organizationId, query, limit: 8, peopleOnly: true }, { signal: controller.signal })
      .then((found) => setPeople(found.people.flatMap((person) => (person.userId ? [{
        id: person.userId, label: person.displayName || person.email || person.userId, detail: person.email || undefined,
      }] : []))))
      .catch(() => { if (!controller.signal.aborted) { setPeople([]); setFailed(true); } });
    return () => controller.abort();
  }, [kind, organizationId, query, searchTurn]);

  const results = useMemo<EnterpriseScopeChoice[]>(() => {
    const needle = query.trim().toLowerCase();
    const matches = (text: string) => !needle || text.toLowerCase().includes(needle);
    if (kind === "custodian") return people;
    if (kind === "workspace") {
      return workspaces.filter((workspace) => matches(workspace.name)).slice(0, 8)
        .map((workspace) => ({ id: workspace.id, label: workspace.name }));
    }
    // Each page with where it sits, as Notion's lists name it, so two pages of
    // one name tell apart (2026-11-06: two "시작하기").
    const pages = productPages?.pages ?? [];
    const pagesById = pagesByIdOf(pages), outOfTrash = outOfTrashIn(pages);
    return pages
      .filter((page) => outOfTrash(page) && matches(pageNames.title(page)))
      .slice(0, 8)
      .map((page) => ({ id: page.id, label: pageNames.title(page), place: pagePlace(page, pagesById) }));
  }, [kind, people, productPages?.pages, query, workspaces, pageNames]);

  const chosen = new Set(selected.map((choice) => choice.id));
  const open = results.filter((result) => !chosen.has(result.id));
  return (
    <div className={styles.picker} data-enterprise-scope-picker={kind}>
      <span>{label}</span>
      {selected.length > 0 ? (
        <ul className={styles.chosen}>
          {selected.map((choice) => (
            <li key={choice.id}>
              {choice.label}
              <button type="button" aria-label={t("enterpriseScopeRemove", { name: choice.label })}
                onClick={() => onChange(selected.filter((other) => other.id !== choice.id))}>×</button>
            </li>
          ))}
        </ul>
      ) : null}
      <input type="search" value={query} placeholder={t("enterpriseScopeSearch")} aria-label={label}
        onChange={(event) => setQuery(event.target.value)} />
      <ul className={styles.results}>
        {open.map((result) => (
          <li key={result.id}>
            <button type="button" onClick={() => { onChange([...selected, result]); setQuery(""); }}>
              <span>{result.label}</span>{result.place?.length ? <small><PlaceLine place={result.place} /></small> : result.detail ? <small>{result.detail}</small> : null}
            </button>
          </li>
        ))}
        {kind === "custodian" && failed ? (
          <li><ReadFailed message={t("enterpriseScopePeopleLoadFailed")} onRetry={() => setSearchTurn((turn) => turn + 1)} /></li>
        ) : open.length === 0 ? <li className={styles.none}>{t("enterpriseScopeNone")}</li> : null}
      </ul>
    </div>
  );
}
