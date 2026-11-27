import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown, DotsHorizontal, FileBlank, LockIcon, ReturnKeyIcon, TableIcon, X } from "@/icons/leafen";
import { canAddTeamspacePage, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement, type PopoverAnchor } from "@/lib/popoverPlacement";
import { holdTypingWhile, pressPageBeingMade, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import type { Page } from "@/types";
import { closeAddPageWindow, openAddPageWindow, useAddPageWindowRequest, type AddPagePlace, type AddPageRequest } from "./addPageWindowStore";
import { whenPageMadeHereRefused } from "@/lib/pageMadeHereRefused";
import styles from "./AddPageWindow.module.css";

import { DatabaseStarterGallery, type DatabaseStarterSourceLoader } from "@/components/database/DatabaseStarterGallery";
import { DatabaseStarterCustomizer } from "@/components/database/DatabaseStarterCustomizer";
import { databaseStarterLabel, databaseStarterMadeAtOnce, type DatabaseStarterAuthor, type DatabaseStarterTemplate } from "@/lib/databaseStarterCatalog";
import type { DatabaseStarterCreationChoice, AddRootPageDetails } from "@/lib/databaseStarterCreation";
import { keysMovedHighlight, pointerTakenRow } from "@/lib/pointerHover";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import { useClosedForLink } from "@/lib/productWindows";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type Kind = "page" | "database";

/**
 * The add window wherever something opened it: the page or database made at
 * the top of the place picked, then opened, a page at its title.
 */
export function AddPageWindowHost({ teamspaces, canAddPrivate, onCreateRootPage, onOpenPage, loadDatabaseSources, previewAuthor }: {
  teamspaces: readonly DialogTeamspace[];
  canAddPrivate: boolean;
  onCreateRootPage: (database: boolean, place: AddRootPageDetails) => Promise<Page | null>;
  /** Opens what the window made; `replace` puts it in the place of the page shown in the history. */
  onOpenPage: (page: Page, replace: boolean) => void;
  loadDatabaseSources?: DatabaseStarterSourceLoader;
  previewAuthor?: DatabaseStarterAuthor;
}) {
  const request = useAddPageWindowRequest();
  if (!request) return null;
  return (
    <AddPageWindow request={request} teamspaces={teamspaces} canAddPrivate={canAddPrivate} onClose={closeAddPageWindow}
      loadDatabaseSources={loadDatabaseSources} previewAuthor={previewAuthor} onCreate={(place, database, title, databaseChoice) => {
        // A page, named or not, opens at its title, as Notion's; a database,
        // named or blank, opens with nothing holding the keys, as Notion's
        // blank one from this window (measured 2026-11-16 in Chrome twice, in
        // fresh tabs with no script in the page, "새로 › 데이터베이스 › 빈
        // 데이터베이스": a letter typed right away and one typed 3 s later
        // went nowhere, the title still its placeholder).
        const opensAtTitle = !database && !databaseChoice;
        const make = async () => {
          const page = await onCreateRootPage(database, { teamspaceId: place.kind === "teamspace" ? place.teamspaceId : undefined, title, databaseChoice });
          if (!page) return false;
          // Refused by the server once open, the window shows again as it was, its name typed (pageMadeHereRefused.ts).
          whenPageMadeHereRefused(page.id, () => openAddPageWindow({ place, mode: request.mode, title }));
          if (opensAtTitle) requestPageTitleFocus(page.id);
          // A database it made takes the place of the page shown in the
          // history, as Notion's starter window opens what it made
          // (navigateToOnFinish "created_collection", redirect — its web
          // client, read 2026-11-13): back goes where that page came from.
          onOpenPage(page, database);
          return true;
        };
        // What is typed until a page opens at its title goes into that title.
        return opensAtTitle ? holdTypingWhile(make) : make();
      }} />
  );
}

/**
 * The window a teamspace's or the private pages' "+" opens, as Notion's
 * (seen 2026-10-07): "추가 대상" with the place to add to, which another
 * teamspace or the private pages can take, a field in the middle, and
 * "빈 페이지" and "빈 데이터베이스" (only the latter when a database was
 * asked for). Typing in the field turns them into "추가 옵션" — a new page
 * or database named what was typed, Enter making the one picked. Notion's
 * templates and existing sources use the shared starter gallery below.
 * "마켓플레이스에서 더 보기" is left out.
 */
export function AddPageWindow({ request, teamspaces, canAddPrivate, onCreate, onClose, loadDatabaseSources, previewAuthor }: {
  request: AddPageRequest;
  /** The teamspaces the reader joined, in their sidebar order. */
  teamspaces: readonly DialogTeamspace[];
  canAddPrivate: boolean;
  /** Makes the page or database there and opens it; false when it couldn't. */
  onCreate: (place: AddPagePlace, database: boolean, title: string, databaseChoice?: DatabaseStarterCreationChoice) => Promise<boolean>;
  onClose: () => void;
  loadDatabaseSources?: DatabaseStarterSourceLoader;
  previewAuthor?: DatabaseStarterAuthor;
}) {
  const { t } = useTranslation(["sidebar", "common"]);
  const labelId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const optionsRef = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState(request.place);
  const [query, setQuery] = useState(request.title ?? "");
  const [picked, setPicked] = useState(0);
  const [template, setTemplate] = useState<DatabaseStarterTemplate | null>(null);
  const createWork = useRef(false);
  // Where the window was pressed while it made something (pressPageBeingMade).
  const pressedWhileMaking = useRef<{ x: number; y: number } | null>(null);
  // A page made here opens with nothing focused, as Notion's (seen 2026-10-29); only a window closed
  // without one gives the focus back to what opened it.
  const made = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const [busy, setBusy] = useState(false);
  // Why it couldn't be added, in the words the server's refusal has
  // elsewhere (a data loss prevention term, no access), or the general line.
  const [failure, setFailure] = useState<string | null>(null);
  const failed = failure !== null;
  const [chooser, setChooser] = useState<PopoverAnchor | null>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], initialFocus: searchRef,
    shouldRestoreFocus: () => !made.current,
    onEscape: () => { if (!busy) onClose(); },
  });
  // A link going to a place here closes it as Escape does (closeWindowsForLink), unless it is making a page.
  useClosedForLink(() => { if (!createWork.current) onClose(); });
  const places = teamspaces.filter(canAddTeamspacePage);
  const title = query.trim();
  const kinds: Kind[] = request.mode === "database" ? ["database"] : ["page", "database"];
  const placeTeamspace = place.kind === "teamspace" ? teamspaces.find((teamspace) => teamspace.id === place.teamspaceId) : undefined;

  async function create(kind: Kind, named: string, choice?: DatabaseStarterCreationChoice) {
    if (createWork.current) return;
    createWork.current = true;
    setBusy(true);
    setFailure(null);
    let refusal = "";
    const created = await onCreate(place, kind === "database", named, choice).catch((error: unknown) => {
      refusal = authenticatedProductErrorMessage(error, "pageCreate");
      return false;
    });
    createWork.current = false;
    if (!mounted.current) return;
    setBusy(false);
    made.current = created;
    if (created) onClose();
    else setFailure(refusal || t("addWindow.failed"));
  }

  function onSearchKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (!title) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      // Round past the ends, the pointer taking the pick back only as it
      // moves into another row (`pointerHover`), as Notion's (seen 2026-11-14).
      keysMovedHighlight(optionsRef.current);
      setPicked((index) => (index + (event.key === "ArrowDown" ? 1 : kinds.length - 1)) % kinds.length);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      void create(kinds[picked] ?? kinds[0], title);
    }
  }

  const kindIcon = (kind: Kind) => (kind === "page"
    ? <FileBlank size="regular" aria-hidden="true" /> : <TableIcon size="regular" aria-hidden="true" />);

  const layer = (
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }}
      onPointerDownCapture={(event) => { pressedWhileMaking.current = createWork.current ? { x: event.clientX, y: event.clientY } : null; }}
      onMouseDownCapture={(event) => { if (createWork.current) event.preventDefault(); }}
      onPointerUpCapture={() => {
        const point = pressedWhileMaking.current;
        pressedWhileMaking.current = null;
        if (point && createWork.current) pressPageBeingMade(point);
      }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")}
        onClick={() => { if (!busy) onClose(); }} />
      <section ref={dialogRef} className={styles.window} role="dialog" aria-modal="true" aria-labelledby={template ? undefined : labelId} aria-label={template ? t("addWindow.customize", { title: databaseStarterLabel(template.name) }) : undefined}
        aria-busy={busy || undefined} data-add-page-window={request.mode}>
        {template ? <DatabaseStarterCustomizer template={template} busy={busy} failed={failed} previewAuthor={previewAuthor} onCancel={() => { setTemplate(null); setFailure(null); }}
          onApply={selection => void create("database", databaseStarterLabel(template.name), { kind: "template", selection })} /> : <>
        <header className={styles.head}>
          <div className={styles.headStart}>
            <button type="button" className={styles.close} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose}>
              <X size="micro" aria-hidden="true" />
            </button>
            <span id={labelId} className={styles.placeLabel}>{t("addWindow.addTo")}</span>
            <button type="button" className={styles.place} aria-haspopup="menu" aria-expanded={chooser !== null} data-add-page-place
              onClick={(event) => {
                const button = event.currentTarget;
                setChooser((value) => (value ? null : popoverAnchorOf(button)));
              }}>
              {placeTeamspace ? <span className={styles.placeGlyph} aria-hidden="true">{teamspaceGlyph(placeTeamspace)}</span>
                : <LockIcon size="small" aria-hidden="true" />}
              <span className={styles.placeName}>{placeTeamspace?.name ?? t("privatePages")}</span>
              <ChevronDown size="micro" aria-hidden="true" />
            </button>
          </div>
          <label className={styles.search}>
            <input ref={searchRef} value={query} placeholder={t("addWindow.search")} aria-label={t("addWindow.search")}
              onChange={(event) => { setQuery(event.target.value); setPicked(0); }} onKeyDown={onSearchKeyDown} />
          </label>
          <span className={styles.headEnd} />
        </header>
        <div className={styles.body}>
          {title ? (
            <div ref={optionsRef} className={styles.options} role="menu" aria-label={t("addWindow.moreOptions")}
              onPointerMove={(event) => { const kind = pointerTakenRow(event); if (kind !== null) setPicked(kinds.indexOf(kind as Kind)); }}>
              <div className={styles.optionsHead}><DotsHorizontal size="small" aria-hidden="true" />{t("addWindow.moreOptions")}</div>
              {kinds.map((kind, index) => (
                <button key={kind} type="button" role="menuitem" className={styles.option} data-picked={index === picked ? "true" : undefined}
                  data-key={kind} data-add-page-option={kind} disabled={busy} onClick={() => void create(kind, title)}>
                  {kindIcon(kind)}
                  <span className={styles.optionLabel}>{t(kind === "page" ? "addWindow.newPage" : "addWindow.newDatabase", { title })}</span>
                  {index === picked ? <ReturnKeyIcon size="regular" className={styles.optionEnter} aria-hidden="true" /> : null}
                </button>
              ))}
            </div>
          ) : (
            <div className={styles.cards} data-single={kinds.length === 1 ? "true" : undefined}>
              {kinds.map((kind) => (
                <button key={kind} type="button" className={styles.card} data-add-page-blank={kind} disabled={busy}
                  onClick={() => void create(kind, "")}>
                  {kindIcon(kind)}
                  <span>{t(kind === "page" ? "addWindow.blankPage" : "addWindow.blankDatabase")}</span>
                </button>
              ))}
            </div>
          )}
          <DatabaseStarterGallery query={query} busy={busy} loadSources={loadDatabaseSources} onTemplate={picked => {
            const selection = databaseStarterMadeAtOnce(picked);
            if (selection) void create("database", databaseStarterLabel(picked.name), { kind: "template", selection });
            else setTemplate(picked);
          }}
            onSource={source => void create("database", t("editor:starter.linkedTitle", { title: source.title }), { kind: "source", source })} />
          {failure ? <p className={styles.failed} role="status">{failure}</p> : null}
        </div>
        </>}
      </section>
      {chooser ? (
        <AddPagePlaceChooser anchor={chooser} place={place} places={places} canAddPrivate={canAddPrivate}
          onPick={(next) => { setPlace(next); setChooser(null); }} onClose={() => setChooser(null)} />
      ) : null}
    </div>
  );
  return createPortal(layer, document.body);
}

/** "추가 대상 ⌄": the teamspaces one may add to and the private pages, under a search field (252px). */
function AddPagePlaceChooser({ anchor, place, places, canAddPrivate, onPick, onClose }: {
  anchor: PopoverAnchor;
  place: AddPagePlace;
  places: readonly DialogTeamspace[];
  canAddPrivate: boolean;
  onPick: (place: AddPagePlace) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("sidebar");
  const ref = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  // The chooser opens to be searched, as Notion's: its new page's "추가 대상 ⌄"
  // pressed, "z" went into the search (measured 2026-11-16 in Chrome at 1440
  // with no script before the press, Leafen the same).
  useEffect(() => search.current?.focus({ preventScroll: true }), []);
  const position = usePopoverPlacement(ref, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0 }), [anchor], undefined,
    { kind: "picker", inside: () => [anchor.contextElement], onEscape: onClose, onOutsidePress: onClose });
  const needle = query.trim().toLowerCase();
  const privateName = t("privatePages");
  const shown = places.filter((teamspace) => teamspace.name.toLowerCase().includes(needle));
  const showPrivate = canAddPrivate && privateName.toLowerCase().includes(needle);
  return (
    <div ref={ref} className={styles.chooser} role="dialog" aria-label={t("addWindow.addTo")} data-add-page-chooser
      style={{ position: "fixed", ...popoverPlacementStyle(position) }}>
      <label className={styles.chooserSearch}>
        <input ref={search} value={query} placeholder={t("addWindow.searchPlaces")} aria-label={t("addWindow.searchPlaces")}
          onChange={(event) => setQuery(event.target.value)} />
      </label>
      <div role="menu" aria-label={t("addWindow.addTo")}>
        {shown.map((teamspace) => {
          const current = place.kind === "teamspace" && place.teamspaceId === teamspace.id;
          return (
            <button key={teamspace.id} type="button" role="menuitemradio" aria-checked={current} className={styles.chooserItem}
              data-add-page-place-choice={teamspace.id} onClick={() => onPick({ kind: "teamspace", teamspaceId: teamspace.id })}>
              <span className={styles.placeGlyph} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
              <span className={styles.placeName}>{teamspace.name}</span>
              {current ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </button>
          );
        })}
        {showPrivate ? (
          <button type="button" role="menuitemradio" aria-checked={place.kind === "private"} className={styles.chooserItem}
            data-add-page-place-choice="private" onClick={() => onPick({ kind: "private" })}>
            <LockIcon size="page" aria-hidden="true" />
            <span className={styles.placeName}>{privateName}</span>
            {place.kind === "private" ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </button>
        ) : null}
      </div>
    </div>
  );
}
