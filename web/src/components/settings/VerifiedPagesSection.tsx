import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useProductPages } from "@/components/product/productPagesContext";
import { SidebarPagePicker } from "@/components/sidebar/SidebarPagePicker";
import { EmptyPageFilterIcon, TeamspaceIcon } from "@/icons/leafen";
import { SettingsSearchToggle } from "./SettingsSearchToggle";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { usePageNames } from "@/lib/pageNames";
import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  listWorkspaceVerifiedPages,
  type VerifiedPageRef,
  type WorkspaceVerifiedPages,
} from "@/lib/workspaceVerifiedPages";
import type { Session, Workspace } from "@/types";
import table from "./DeveloperSettings.module.css";
import { SettingsChipIcon, SettingsFilterChip, SettingsPersonFilterIcon } from "./SettingsFilterChip";
import chipStyles from "./SettingsFilterChip.module.css";
import { SettingsChevron } from "./SettingsChevron";
import styles from "./SettingsItemRows.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { activeDateLocale } from "@/lib/i18n";

/** The teamspace filter's choice for pages outside a teamspace the reader sees. */
const NO_TEAMSPACE = "\u0000none";

/**
 * The features' "인증된 페이지" item, as Notion's (seen 2026-10-12): the
 * workspace's verified pages the reader can open, filtered by owner — the
 * reader at first — by the page they sit in ("다음에 포함") and by
 * teamspace, with a search behind an icon; each row its page, owners and
 * the date its verification ends. Notion's "모든 페이지
 * 검증" row is its plan's prompt; Leafen lets anyone who can edit a page verify
 * it.
 */
export function VerifiedPagesSection({
  session,
  workspace,
  onOpenPage,
}: {
  session: Session;
  workspace: Workspace;
  onOpenPage: (pageId: string) => void;
}) {
  const pageNames = usePageNames();
  const { t, i18n } = useTranslation("workspaceSettingsDialog");
  const [list, setList] = useState<WorkspaceVerifiedPages | null>(null);
  const [query, setQuery] = useState("");
  const [owner, setOwner] = useState(session.user.id);
  const [within, setWithin] = useState("");
  const [picking, setPicking] = useState<DOMRect | null>(null);
  const withinRef = useRef<HTMLButtonElement>(null);
  const product = useProductPages();
  const [teamspace, setTeamspace] = useState("");
  // A failed read of the list, in its loading line's place with a way to read again.
  const [error, setError] = useState("");
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    setList(null);
    setError("");
    const controller = new AbortController();
    listWorkspaceVerifiedPages(session, workspace.id, controller.signal)
      .then(setList)
      .catch((failure) => {
        if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("verifiedPagesLoadFailed")));
      });
    return () => controller.abort();
  }, [readTurn, session, t, workspace.id]);

  const choices = useMemo(() => {
    const people = new Map<string, string>();
    const places = new Map<string, string>();
    let outside = false;
    people.set(session.user.id, session.user.name || session.user.email);
    for (const item of list?.items ?? []) {
      for (const person of item.owners) people.set(person.id, person.name);
      if (item.teamspace) places.set(item.teamspace.id, item.teamspace.name);
      else outside = true;
    }
    const sorted = (entries: Map<string, string>): VerifiedPageRef[] => [...entries]
      .map(([id, name]) => ({ id, name }))
      .sort((left, right) => left.name.localeCompare(right.name, i18n.language));
    return { owners: sorted(people), teamspaces: sorted(places), outside };
  }, [i18n.language, list, session.user.email, session.user.id, session.user.name]);

  const rows = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase(i18n.language);
    return (list?.items ?? []).filter((item) =>
      (!needle || item.title.toLocaleLowerCase(i18n.language).includes(needle))
      && (!owner || item.owners.some((person) => person.id === owner))
      && (!within || item.pageId === within || (item.ancestorIds ?? []).includes(within))
      && (!teamspace || (teamspace === NO_TEAMSPACE ? item.teamspace === null : item.teamspace?.id === teamspace)));
  }, [i18n.language, list, owner, query, teamspace, within]);

  const withinPage = within ? (product?.pages ?? []).find((page) => page.id === within) : undefined;
  const withinTitle = withinPage ? pageNames.title(withinPage) : t("verifiedPagesWithinPage");
  const date = (value: string) => {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? "" : new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(parsed));
  };
  const person = (ref: VerifiedPageRef) => ref.name || t("connectionUnknownPerson");

  return (
    <div data-verified-pages>
      <section className={settingsStyles.section} role="region" aria-label={t("navVerifiedPages")}>
        <div className={chipStyles.bar} data-verified-filters>
          <SettingsFilterChip marker="owner" icon={<SettingsPersonFilterIcon />} label={t("verifiedPagesOwnerFilter")}
            active={Boolean(owner)} value={owner} searchPlaceholder={t("verifiedPagesOwnerSearch")} emptyLabel={t("noSearchResults")}
            choices={[{ value: "", label: t("verifiedPagesAnyOwner") },
              ...choices.owners.map((entry) => ({ value: entry.id, label: person(entry) }))]}
            onChoose={setOwner} />
          <button ref={withinRef} type="button" className={chipStyles.chip} data-active={within ? true : undefined}
            data-filter-within="" onClick={() => setPicking(popoverAnchorOf(withinRef.current!))}>
            <SettingsChipIcon icon={<EmptyPageFilterIcon size="small" />} />
            <span>{within ? t("verifiedPagesWithinChosen", { title: withinTitle }) : t("verifiedPagesWithin")}</span>
            <SettingsChevron />
          </button>
          <SettingsFilterChip marker="teamspace" icon={<TeamspaceIcon size="small" trim />} label={t("verifiedPagesTeamspaceFilter")}
            active={Boolean(teamspace)} value={teamspace} searchPlaceholder={t("verifiedPagesTeamspaceSearch")} emptyLabel={t("noSearchResults")}
            choices={[{ value: "", label: t("verifiedPagesAnyTeamspace") },
              ...choices.teamspaces.map((entry) => ({ value: entry.id, label: entry.name })),
              ...(choices.outside ? [{ value: NO_TEAMSPACE, label: t("verifiedPagesNoTeamspace") }] : [])]}
            onChoose={setTeamspace} />
          <span className={chipStyles.spacer} />
          <SettingsSearchToggle query={query} onQueryChange={setQuery} placeholder={t("verifiedPagesSearch")} />
        </div>
        {picking ? (
          <SidebarPagePicker anchor={picking} placeholder={t("verifiedPagesWithinSearch")} rootLabel={t("verifiedPagesAnyPage")}
            pages={product?.pages ?? []} session={session} workspaceId={workspace.id}
            onChoose={(pageId) => { setWithin(pageId); setPicking(null); }} onClose={() => setPicking(null)} />
        ) : null}
        {list ? null : error ? (
          <ListNotice role="alert" onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }} data-verified-read-failed>{error}</ListNotice>
        ) : <div className={styles.empty}>{t("verifiedPagesLoading")}</div>}
        {list ? (
          <table className={table.tokens} data-verified-page-list>
            <thead>
              <tr>
                <th scope="col">{t("verifiedColumnPage")}</th>
                <th scope="col">{t("verifiedColumnOwners")}</th>
                <th scope="col">{t("verifiedColumnExpires")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr><td colSpan={3} className={chipStyles.emptyCell} data-verified-empty>{t("verifiedPagesNone")}</td></tr>
              ) : null}
              {rows.map((item) => (
                <tr key={`${item.pageId}:${item.blockId ?? ""}`} data-verified-page={item.pageId}>
                  <td className={table.name}>
                    <button type="button" className={table.secondary} onClick={() => onOpenPage(item.pageId)}>
                      {titleWithClosedMentions(item.title, item.titleSpans) || t("publicUntitled")}
                    </button>
                  </td>
                  <td>{item.owners.map(person).join(", ")}</td>
                  <td title={t("verifiedPagesVerifiedBy", { name: person(item.verifiedBy), date: date(item.verifiedAt) })}>
                    {item.expiresAt ? date(item.expiresAt) : t("verifiedPagesIndefinitely")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
        {list?.hasMore ? <div className={styles.empty}>{t("verifiedPagesMore")}</div> : null}
      </section>
    </div>
  );
}
