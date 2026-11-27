import { Plus, Search } from "@/icons/leafen";
import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { PageIcon } from "@/components/page/PageIcon";
import { newId } from "@/lib/ids";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { createNativePage } from "@/lib/pageCreationService";
import { pagePlace, type Place } from "@/lib/pagePath";
import { PlaceLine } from "@/components/PlaceLine";
import { requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { publicMutationService } from "@/lib/publicMutationService";
import { useProductPages } from "@/components/product/productPagesContext";
import { publicMutationActions, publicMutationPrincipalTypes, publicMutationRoles } from "@/publicMutationWire.generated";
import type { Page, Session, Workspace } from "@/types";
import { SettingsChevron } from "./SettingsChevron";
import styles from "./WorkspaceLandingPage.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { useShownChanges } from "@/lib/shownChanges";
import { plainTitleOf } from "@/lib/closedMentions";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup } from "@/components/shell/PhoneSheet";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, usePopoverPlacement, type PopoverRect, type PopoverViewport } from "@/lib/popoverPlacement";
import { pagesByIdOf } from "@/lib/pageIndex";
import { ReadFailed } from "@/components/ReadFailed";

type Choice = Pick<Page, "id" | "title" | "icon" | "iconType" | "kind"> & Partial<Pick<Page, "parentId">>;
/** The list's rows: a page to choose, or "+ 새 페이지 생성하기" at its foot. */
type Row = Choice | "create";
// No page id looks like it.
const CREATE_KEY = "landing-page:create";
function rowKey(row: Row) {
  return row === "create" ? CREATE_KEY : row.id;
}

/** What the server answered a choice: taken, refused as a page some members couldn't open, or failed. */
export type LandingPageAnswer = "saved" | "hidden" | "failed";

class LandingPageRefused extends Error {
  constructor(readonly answer: LandingPageAnswer) {
    super("landing page refused");
  }
}

/**
 * The page people who join the workspace open first, as Notion's
 * "사용자 지정 랜딩 페이지" under 일반 (seen 2026-10-05): a button naming it
 * (or "페이지 선택하기") opens a list of recently edited pages to filter by
 * title, each with the pages above it under its title, and "+ 새 페이지
 * 생성하기"; "초기화" beside the button lets the chosen one go (seen
 * 2026-11-09). A member who can't read the chosen page starts as usual.
 * A page chosen or let go shows at once and goes to the server behind it; a
 * refused one puts back the page before, its row saying why
 * (lib/shownChanges.ts). The list stays open until the answer: a page some
 * people who join couldn't open is refused, the list kept for another and
 * Notion's red words under the button (seen 2026-11-14: a private page,
 * shared with one person or none, and a page of a teamspace the rest of the
 * workspace has no access to); a taken one closes it. Making a new page
 * waits for the server: it is shared with the whole workspace first.
 */
export function WorkspaceLandingPage({ session, workspace, canManage, onChoose, onOpenPage }: {
  session: Session;
  workspace: Workspace;
  canManage: boolean;
  onChoose: (pageId: string) => Promise<LandingPageAnswer>;
  onOpenPage?: (pageId: string) => void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  // On a touch screen its list rises on a sheet, its filter waiting for a
  // tap, as the settings' other choices there (SettingsTouchChoice).
  const { touch, phone } = useSidebarLayout(true);
  const labelId = useId();
  const rowRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const [chosen, setChosen] = useState<Choice | null>(null);
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [results, setResults] = useState<Choice[] | null>(null);
  // A search that failed says so in the list, with a way to search again — not "none".
  const [searchFailed, setSearchFailed] = useState(false);
  const [searchTurn, setSearchTurn] = useState(0);
  const [busy, setBusy] = useState(false);
  // The last page pressed was refused as one some people who join couldn't open.
  const [hidden, setHidden] = useState(false);
  const openRef = useRef(open);
  openRef.current = open;
  const chooseTurn = useRef(0);
  const pending = useShownChanges<{ landing: Choice | null }>();
  const savedLandingPageId = workspace.landingPageId ?? "";
  const landingPageId = pending.shown.landing === undefined ? savedLandingPageId : pending.shown.landing?.id ?? "";
  const productPages = useProductPages();
  const pagesById = pagesByIdOf(productPages?.pages ?? []);
  // The row picked here stands for the saved page until its own read, so the
  // answer never shows the page before for a frame.
  const picked = useRef<Choice | null>(null);
  const savedChoice = !savedLandingPageId ? null : chosen?.id === savedLandingPageId ? chosen
    : picked.current?.id === savedLandingPageId ? picked.current : chosen;
  const shownChoice = pending.shown.landing === undefined ? savedChoice : pending.shown.landing;
  usePathsAboveShown([...(results ?? []), ...(shownChoice ? [shownChoice] : [])].flatMap((choice) => pagesById[choice.id] ? [pagesById[choice.id]] : []), pagesById);

  // The chosen page's title and icon: the row picked here, or read.
  useEffect(() => {
    if (!savedLandingPageId) {
      setChosen(null);
      return;
    }
    if (picked.current?.id === savedLandingPageId) {
      setChosen(picked.current);
      return;
    }
    const controller = new AbortController();
    api.readPagePreview(session, savedLandingPageId, { signal: controller.signal })
      .then((preview) => setChosen({ id: savedLandingPageId, title: preview.title, icon: preview.icon, iconType: preview.iconType as Page["iconType"], kind: "page" }))
      .catch(() => { if (!controller.signal.aborted) setChosen({ id: savedLandingPageId, title: "", icon: "", iconType: "none", kind: "page" }); });
    return () => controller.abort();
  }, [savedLandingPageId, session]);

  // Recently edited pages, or the pages whose titles hold the filter.
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const query = filter.trim();
    const timer = window.setTimeout(() => {
      api.searchWorkspace(session, workspace.id, query, {
        limit: 8, signal: controller.signal,
        filterParams: query ? { titleOnly: "true" } : { sort: "edited_desc" },
      }).then((found) => { setResults(found.results.flatMap((result) => result.row ? [] : [result.page])); setSearchFailed(false); })
        .catch(() => { if (!controller.signal.aborted) { setResults([]); setSearchFailed(true); } });
    }, query ? 150 : 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [filter, open, searchTurn, session, workspace.id]);

  function closeList() {
    setOpen(false);
    window.requestAnimationFrame(() => buttonRef.current?.focus());
  }

  // Its list floats over the settings window, 250px from the button's start
  // 4px under it, turned over it where the room under it is short — Notion's
  // (seen 2026-11-14 in a 723 window: 332..529 under a button at 300..328,
  // 315..512 over one at 516..544). Inside the panel, it was cut off at the
  // window's foot and scrolled with the rows (2026-11-12). It keeps the place
  // it opened at in its row as the button moves in it — a page shown chosen
  // changes the button's width, a refused one's red words move it to the
  // side's start — as Notion's stays (seen 2026-11-14: at 1111 while the
  // button went to 1035.5); the row moving, as the window scrolls, takes it
  // along, and a window of another size places it again.
  const opened = useRef<{ top: number; left: number; width: number; height: number; viewport: PopoverViewport } | null>(null);
  function openedAt(viewport: PopoverViewport): PopoverRect {
    const button = buttonRef.current, row = rowRef.current;
    if (!button || !row) return { top: 0, bottom: 0, left: 0, right: 0 };
    const kept = opened.current;
    if (!kept || kept.viewport.width !== viewport.width || kept.viewport.height !== viewport.height) {
      const at = button.getBoundingClientRect(), from = row.getBoundingClientRect();
      opened.current = { top: at.top - from.top, left: at.left - from.left, width: at.width, height: at.height, viewport };
    }
    const { top, left, width, height } = opened.current!;
    return {
      get top() { return row.getBoundingClientRect().top + top; },
      get bottom() { return row.getBoundingClientRect().top + top + height; },
      get left() { return row.getBoundingClientRect().left + left; },
      get right() { return row.getBoundingClientRect().left + left + width; },
      contextElement: button,
    };
  }
  const place = usePopoverPlacement(panelRef, open && !touch ? (size, viewport) => placeAround(
    openedAt(viewport), size, viewport, { gap: 4, align: "start" },
  ) : null, [open, touch], undefined, { kind: "menu", restoreFocus: false, onEscape: closeList, onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current] });

  function choose(page: Choice | null) {
    picked.current = page;
    setHidden(false);
    const turn = chooseTurn.current += 1;
    const answered = () => {
      if (turn === chooseTurn.current && openRef.current) closeList();
    };
    if (!page) answered();
    void pending.change({ landing: page }, async () => {
      const answer = await onChoose(page?.id ?? "");
      if (answer !== "saved") throw new LandingPageRefused(answer);
    }).then(answered, (error: unknown) => {
      if (error instanceof LandingPageRefused && error.answer === "hidden") {
        if (turn === chooseTurn.current) setHidden(true);
        return;
      }
      answered();
    });
  }

  // A new page is Notion's (seen 2026-11-09): "〈workspace〉에 오신 것을
  // 환영합니다!" among the maker's private pages, everyone in the workspace
  // reading it, chosen, and opened in place of the settings with the caret at
  // its title's end.
  async function createAndChoose() {
    setBusy(true);
    try {
      const title = t("landingPageWelcomeTitle", { name: workspace.name });
      const page = await createNativePage(session, { parentId: "", title, workspaceId: workspace.id });
      const signal = new AbortController().signal;
      const { state } = await publicMutationService.state(session, page.id, signal);
      await publicMutationService.mutate(session, page.id, {
        action: publicMutationActions.grantPermission, principalType: publicMutationPrincipalTypes.workspace,
        principalId: workspace.id, role: publicMutationRoles.view, requestId: newId(), expectedRevision: state.revision,
      }, signal).catch(() => undefined);
      void productPages?.refreshPages?.();
      setHidden(false);
      const answer = await onChoose(page.id);
      setBusy(false);
      if (answer === "hidden") setHidden(true);
      if (answer !== "saved" || !onOpenPage) return;
      setOpen(false);
      requestPageTitleFocus(page.id);
      onOpenPage(page.id);
    } catch {
      setBusy(false);
    }
  }

  const title = (page: Choice) => plainTitleOf(page) || untitledPageDisplayTitle(page);
  const location = (page: Choice): Place => {
    const known = pagesById[page.id];
    return known ? pagePlace(known, pagesById) : [];
  };
  // The list is a menu with its filter, as Notion's: ↑↓ move its highlight
  // from the filter and Enter takes it, none lit as it opens or as the
  // filter is typed (seen 2026-11-14).
  const rows: Row[] = [...(results ?? []), "create"];
  const menu = useNativeMenu<Row>({ items: rows, open, menuRef, label: t("landingPageField"), focusOwnerRef: filterRef,
    focusOnOpen: !touch, ownerSearches: false, disabledKeys: busy ? rows.map(rowKey) : [],
    keyOf: rowKey, textOf: (row) => row === "create" ? t("landingPageCreate") : title(row),
    onAction: (row) => { if (row === "create") void createAndChoose(); else choose(row); } });
  const panel = open ? (
    <div ref={panelRef} className={styles.panel} style={touch ? undefined : { position: "fixed", ...popoverPlacementStyle(place) }}
      data-workspace-landing-page-list data-in-sheet={touch ? "true" : undefined}>
      <div className={styles.filterRow}>
        <label className={styles.filter}>
          <Search size="small" aria-hidden="true" />
          <input {...menu.ownerProps} ref={filterRef} value={filter} placeholder={t("landingPageFilter")}
            aria-label={t("landingPageFilter")} onChange={(event) => setFilter(event.target.value)} />
        </label>
      </div>
      <div {...menu.menuProps} ref={menuRef} aria-labelledby={labelId} className={styles.menu}>
        <div className={styles.list}>
          {results?.map((page) => {
            const above = location(page);
            return (
              <NativeMenuItem key={page.id} itemKey={page.id} state={menu.state} className={styles.option}
                data-two-line={above.length ? "true" : undefined}>
                {() => <>
                  <span className={styles.icon}><PageIcon page={page} size={18} /></span>
                  <span className={styles.optionText}>
                    <span className={styles.value}>{title(page)}</span>
                    {above.length ? <PlaceLine place={above} className={styles.location} /> : null}
                  </span>
                </>}
              </NativeMenuItem>
            );
          })}
          {searchFailed ? <div className={styles.empty}><ReadFailed message={t("landingPageSearchFailed")} onRetry={() => { setSearchFailed(false); setResults(null); setSearchTurn((turn) => turn + 1); }} /></div>
            : results && results.length === 0 ? <div className={styles.empty}>{t("landingPageEmpty")}</div> : null}
        </div>
        <div className={styles.foot}>
          <NativeMenuItem itemKey={CREATE_KEY} state={menu.state} className={styles.option} data-landing-page-create>
            {() => <><span className={styles.icon}><Plus size="small" aria-hidden="true" /></span>{t("landingPageCreate")}</>}
          </NativeMenuItem>
        </div>
      </div>
    </div>
  ) : null;
  return (
    <div ref={rowRef} className={settingsStyles.securityRow} data-workspace-landing-page>
      <div className={settingsStyles.securityInstruction}>
        <strong id={labelId}>{t("landingPageField")}</strong>
        <span>{t("landingPageDescription")}</span>
      </div>
      {/* Notion's control side (seen 2026-11-14): "초기화" and the button,
          and under them, 8px down, the red words of a refused page, the
          side then a quarter of the row less 3px (its 1:3 share beside the
          words, 12px apart) with the button at its start. */}
      <div className={styles.end} data-row-end data-landing-page-hidden={hidden ? "true" : undefined}>
        <div className={styles.wrap}>
          {landingPageId && canManage ? (
            <button type="button" className={styles.reset} disabled={busy} onClick={() => choose(null)}>
              {t("landingPageClear")}
            </button>
          ) : null}
          <button
            ref={buttonRef}
            type="button"
            className={styles.trigger} data-phone={phone ? "" : undefined}
            aria-haspopup="menu"
            aria-expanded={open}
            aria-labelledby={labelId}
            aria-describedby={`${labelId}-value`}
            disabled={!canManage || busy}
            onClick={() => { opened.current = null; setFilter(""); setResults(null); setOpen((current) => !current); }}
          >
            {shownChoice ? <span className={styles.triggerIcon}><PageIcon page={shownChoice} size={16} /></span> : null}
            <span id={`${labelId}-value`} className={styles.value}>{shownChoice ? title(shownChoice) : t("landingPageChoose")}</span>
            <SettingsChevron className={styles.chevron} />
          </button>
        </div>
        {hidden ? <div className={styles.hiddenNote} role="alert" data-landing-page-hidden-note>{t("landingPageHidden")}</div> : null}
        {panel && touch ? (
          <PhoneSheet title={t("landingPageField")} tone="settings" onClose={closeList} data={{ "data-landing-page-sheet": "" }}
            action={{ label: t("common:actions.done"), onPress: closeList }}>
            <PhoneSheetGroup>{panel}</PhoneSheetGroup>
          </PhoneSheet>
        ) : panel ? createPortal(panel, document.body) : null}
      </div>
    </div>
  );
}
