"use client";

import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  IMPORTED_PEOPLE_PART,
  admitImportedPeopleResult,
  importedPeopleFailureMessage,
  importedPeopleSelectedMappings,
  importedPeopleSuggestedSelection,
  type ImportedPeopleMappingBounds,
  type ImportedPeopleMappingService,
  type ImportedPeopleMember,
  type ImportedPeopleResult,
  type ImportedPersonEntry,
} from "@/lib/importedPeopleMappingView";

import chromeStyles from "./WorkspaceSettingsChrome.module.css";
import memberStyles from "./WorkspaceSettingsMembers.module.css";
import responsiveStyles from "./WorkspaceSettingsResponsive.module.css";
import { AppSelect } from "@/components/AppSelect";

// A first look answered sooner than this shows nothing until it has answered.
const LOOKING_SHOWN_AFTER_MS = 400;

// The people come a window of pages at a time; the list reads windows on
// until it has everyone, or as many as it holds, and shows them together
// (2026-11-06: a large import's first windows named no one, and the list
// showed only "더 불러오기", which then replaced the window above its people).
// One person's entries, one a block or a value, are one line: a name or an
// address an entry lacks is a later one's, as the server merges a block's
// (mergeImportedPeopleSource; 2026-11-13: a first mention without a name
// left the line showing the person's ID).
function mergeImportedPeople(into: Map<string, ImportedPersonEntry>, people: readonly ImportedPersonEntry[]) {
  const given = (value: string | null) => (value?.trim() ? value : null);
  for (const person of people) {
    const known = into.get(person.sourceId);
    into.set(person.sourceId, known ? {
      ...known,
      displayName: given(known.displayName) ?? person.displayName,
      email: given(known.email) ?? person.email,
      propertyValueCount: known.propertyValueCount + person.propertyValueCount,
      mentionCount: known.mentionCount + person.mentionCount,
      suggestedUserId: known.suggestedUserId ?? person.suggestedUserId,
      suggestedDisplayName: known.suggestedDisplayName ?? person.suggestedDisplayName,
    } : person);
  }
}

/**
 * The imported people the workspace's pages name, a window at a time, each
 * mapped to one of the workspace's members — the owner and members the
 * setting lists, apps and guests left out.
 */
export function ImportedPeopleMapping({
  bounds,
  members,
  ownPagesOnly = false,
  service,
  workspaceId,
}: {
  bounds: ImportedPeopleMappingBounds;
  members: readonly ImportedPeopleMember[];
  /** A member maps only in the pages they may change, and is told so. */
  ownPagesOnly?: boolean;
  service: ImportedPeopleMappingService;
  workspaceId: string;
}) {
  const { t } = useTranslation(["importedPeopleMapping", "databaseView", "common"]);
  const [result, setResult] = useState<ImportedPeopleResult | null>(null);
  const [selection, setSelection] = useState<Record<string, string>>({});
  const [nextCursor, setNextCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [busy, setBusy] = useState<"scan" | "apply" | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [lookingShown, setLookingShown] = useState(false);
  const actionGeneration = useRef(0);

  useEffect(() => {
    actionGeneration.current += 1;
    setResult(null);
    setSelection({});
    setNextCursor("");
    setHasMore(false);
    setBusy(null);
    setNotice(null);
    setError(null);
    setLookingShown(false);
    // The section is for people a workspace imported: it looks first, and
    // shows only when there is someone to map. A look that takes a moment
    // shows it is looking, and the section goes again if it finds no one
    // (2026-11-13: on a NAS-class server nothing showed for 25–38 s).
    const looking = window.setTimeout(() => setLookingShown(true), LOOKING_SHOWN_AFTER_MS);
    void scan();
    return () => {
      actionGeneration.current += 1;
      window.clearTimeout(looking);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- scan reads the same service and workspace.
  }, [service, workspaceId]);

  async function readWindow(cursor: string, generation: number, from: readonly ImportedPersonEntry[] = []) {
    const people = new Map(from.map((person) => [person.sourceId, person]));
    let next = await service.listPeople(workspaceId, cursor);
    mergeImportedPeople(people, next.people);
    while (next.hasMore && actionGeneration.current === generation) {
      const after = await service.listPeople(workspaceId, next.nextCursor);
      const merged = new Map(people);
      mergeImportedPeople(merged, after.people);
      // A window that would overfill the list is read again by "더 불러오기".
      if (merged.size > bounds.maxPeople) break;
      people.clear();
      merged.forEach((person, id) => people.set(id, person));
      next = after;
    }
    return { ...next, people: [...people.values()] };
  }

  async function scan(cursor = "") {
    const generation = ++actionGeneration.current;
    setBusy("scan");
    setError(null);
    setNotice(null);
    try {
      // "더 불러오기" adds to the list while it holds more.
      const shown = cursor && result && result.people.length < bounds.maxPeople ? result.people : [];
      const next = await readWindow(cursor, generation, shown);
      if (actionGeneration.current !== generation) return;
      setResult(admitImportedPeopleResult(next, bounds));
      setSelection((current) => ({ ...(shown.length ? current : {}), ...importedPeopleSuggestedSelection(next, bounds) }));
      setNextCursor(next.nextCursor);
      setHasMore(next.hasMore);
    } catch (caught) {
      if (actionGeneration.current !== generation) return;
      setError(importedPeopleFailureMessage(
        caught,
        "scan",
      ));
    } finally {
      if (actionGeneration.current === generation) setBusy(null);
    }
  }

  async function apply() {
    const mappings = importedPeopleSelectedMappings(selection, bounds);
    if (!Object.keys(mappings).length) return;
    const generation = ++actionGeneration.current;
    setBusy("apply");
    setError(null);
    setNotice(null);
    try {
      const summary = await service.applyMappings(workspaceId, mappings);
      if (actionGeneration.current !== generation) return;
      const applied = t("importedPeopleMapping:applied", {
        people: summary.mappedPeople,
        pages: summary.changedPages,
        blocks: summary.changedBlocks,
      });
      // Pages the admin can't open — another member's private ones — keep their imported people.
      setNotice(summary.skippedPages > 0
        ? `${applied} ${t("importedPeopleMapping:skipped", { pages: summary.skippedPages })}`
        : applied);
      // The people just linked leave the list as it says so; the rest is
      // read again behind (2026-11-06: the notice said done while the button
      // kept "연결하는 중…" and the person stayed listed for seconds).
      setResult((current) => current && {
        ...current,
        people: current.people.filter((person) => !(person.sourceId in mappings)),
      });
      setSelection({});
      setBusy(null);
    } catch (caught) {
      if (actionGeneration.current !== generation) return;
      setError(importedPeopleFailureMessage(
        caught,
        "apply",
      ));
      setBusy(null);
      return;
    }
    try {
      const next = await readWindow("", generation);
      if (actionGeneration.current !== generation) return;
      setResult(admitImportedPeopleResult(next, bounds));
      setSelection({});
      setNextCursor(next.nextCursor);
      setHasMore(next.hasMore);
    } catch (caught) {
      if (actionGeneration.current !== generation) return;
      setError(importedPeopleFailureMessage(
        caught,
        "refresh",
      ));
    } finally {
      if (actionGeneration.current === generation) setBusy(null);
    }
  }

  const people = result?.people ?? [];
  const selectable = Object.values(selection).some(Boolean);

  // The first look, still going: the section says so, with nothing to press yet.
  const lookingFirst = lookingShown && !result && !error && busy === "scan";
  if (!lookingFirst && !error && !notice && (!result || (people.length === 0 && !hasMore))) return null;
  return (
    // The finished Notion import and its report open the settings here
    // (IMPORTED_PEOPLE_PART), where people kept by their Notion name are mapped.
    <div className={responsiveStyles.subsection} data-testid="imported-people-mapping" data-settings-part={IMPORTED_PEOPLE_PART}
      aria-busy={lookingFirst || undefined}>
      <div className={chromeStyles.sectionHeader}>
        <div>
          <div className={responsiveStyles.subsectionTitle}>{t("importedPeopleMapping:title")}</div>
          <div className={chromeStyles.organizationMeta}>{t(ownPagesOnly ? "importedPeopleMapping:bodyMember" : "importedPeopleMapping:body")}</div>
          {/* Someone Notion names only as who made or last changed a page
              has nothing here to map, so isn't listed: said, so a person
              kept at the import and missing here isn't a puzzle (2026-11-19:
              two kept, one listed). */}
          <div className={chromeStyles.organizationMeta} data-imported-people-only-named>{t("importedPeopleMapping:onlyNamed")}</div>
        </div>
        {lookingFirst ? null : <div className={chromeStyles.sectionActions}>
          <button
            type="button"
            className={chromeStyles.secondaryButton}
            disabled={busy !== null}
            onClick={() => void scan()}
          >
            {busy === "scan"
              ? t("importedPeopleMapping:scanning")
              : result
                ? t("importedPeopleMapping:rescan")
                : t("importedPeopleMapping:scan")}
          </button>
        </div>}
      </div>
      {lookingFirst ? (
        <div className={chromeStyles.notice} data-tone="neutral" role="status">
          {t("importedPeopleMapping:looking")}
        </div>
      ) : null}
      {error ? <div className={chromeStyles.notice}>{error}</div> : null}
      {notice ? (
        <div className={chromeStyles.notice} data-tone="neutral">
          {notice}
        </div>
      ) : null}
      {result && people.length === 0 && !hasMore ? (
        <div className={chromeStyles.notice} data-tone="neutral">
          {t("importedPeopleMapping:empty")}
        </div>
      ) : null}
      {people.length > 0 ? (
        <div className={memberStyles.memberList} aria-busy={busy !== null}>
          {people.map((person) => {
            const label = person.displayName || person.email || person.sourceId;
            return (
              <div key={person.sourceId} className={memberStyles.memberRow}>
                <PersonAvatar className={memberStyles.memberAvatar} name={label} size={34} />
                <span className={memberStyles.memberText}>
                  <strong>{label}</strong>
                  <span>
                    {person.email ? `${person.email} · ` : ""}
                    {t("importedPeopleMapping:usage", {
                      values: person.propertyValueCount,
                      mentions: person.mentionCount,
                    })}
                  </span>
                </span>
                <span className={memberStyles.memberControls}>
                  <AppSelect
                    className={memberStyles.memberSelect}
                    aria-label={t("importedPeopleMapping:mapAria", { label })}
                    value={selection[person.sourceId] ?? ""}
                    disabled={busy !== null}
                    onChange={(event) =>
                      setSelection((current) => ({
                        ...current,
                        [person.sourceId]: event.target.value,
                      }))
                    }
                  >
                    <option value="">{t("importedPeopleMapping:keepPlaceholder")}</option>
                    {members.map((member) => (
                      <option key={member.userId} value={member.userId}>
                        {(member.displayName || member.email || member.userId)
                          + (person.suggestedUserId === member.userId
                            ? ` (${t("importedPeopleMapping:suggested")})`
                            : "")}
                      </option>
                    ))}
                  </AppSelect>
                </span>
              </div>
            );
          })}
        </div>
      ) : null}
      {result && hasMore ? (
        <div className={chromeStyles.sectionActions}>
          <button
            type="button"
            className={chromeStyles.secondaryButton}
            disabled={busy !== null}
            onClick={() => void scan(nextCursor)}
          >
            {busy === "scan"
              ? t("importedPeopleMapping:scanning")
              : t("databaseView:loadMore")}
          </button>
        </div>
      ) : null}
      {people.length > 0 ? (
        <div className={chromeStyles.sectionActions}>
          <button
            type="button"
            className={chromeStyles.primaryButton}
            disabled={busy !== null || !selectable}
            onClick={() => void apply()}
          >
            {busy === "apply"
              ? t("importedPeopleMapping:applying")
              : t("importedPeopleMapping:apply")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
