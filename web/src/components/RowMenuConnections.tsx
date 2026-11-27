import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft, ArrowUpRightIcon, CheckIcon, ChevronRight, DisconnectConnectionIcon, Plus, Settings, UnlinkSharingIcon, X,
} from "@/icons/leafen";
import { keysLeadNow } from "@/lib/menuInputLead";
import { isMenuMoveKey } from "@/lib/menuKeyboard";
import { usePageNames } from "@/lib/pageNames";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";
import { pageHref } from "@/lib/navigation";
import { developerToolsHref, pushRoute } from "@/lib/router";
import type { PublicMutationNotionConnection } from "@/publicMutationWire.generated";
import { confirmAction } from "./ConfirmActionDialog";
import { PageIconGlyph } from "./page/PageIcon";
import { BESIDE_KEEPS_KEYS, RowMenuBesideSubmenu, type usePageConnections } from "./RowMenuSubmenus";
import styles from "./RowMenu.module.css";
import own from "./RowMenuConnections.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";

type PageConnections = ReturnType<typeof usePageConnections>;
type ConnectableApp = NonNullable<PageConnections["apps"]>[number];

/** A connection as the "연결" lists show it: an app installed as a guest, or a Notion-compatible one. */
export type ListedConnection = { key: string; name: string; creator?: string; app?: ConnectableApp; notion?: PublicMutationNotionConnection };
type Listed = ListedConnection;

/** What a connection may do, in the order Notion's "기능" lists it (seen 2026-11-13). */
export const CONNECTION_CAPABILITIES = ["read_content", "insert_content", "update_content", "insert_comments", "read_comments"] as const;
const CAPABILITIES = CONNECTION_CAPABILITIES;

/**
 * A page's connections as both "연결" lists show them — the menu's box
 * beside it and a phone's sheets: those it has ("활성화된 연결") and, by
 * name as Notion's list (Latin before Hangul), those it may add that match
 * the search, "결과 없음" when none does. A change is the server's to make
 * first; true once made. Taking one off asks first, as Notion's "연결 해제"
 * (seen 2026-11-21): one the page has itself "〈이름〉 제거", one it has from
 * a page above whether to unlink the page's sharing from its parent's too,
 * which taking it off here does (page_notion_connections_unlink.go).
 */
export function usePageConnectionLists(connections: PageConnections, query: string) {
  const { t } = useTranslation("rowMenu");
  const listed: Listed[] = [
    ...(connections.apps ?? []).map((app) => ({ key: `app:${app.appId}`, name: t("connections.appName", { name: app.name }), app })),
    ...connections.notion.map((notion) => ({ key: `notion:${notion.grantId}`, name: notion.name, creator: notion.creatorName, notion })),
  ];
  const isOn = (item: Listed) => (item.app ? connections.connected(item.app) : item.notion?.reach !== "none");
  const needle = query.trim().toLowerCase();
  const addable = listed.filter((item) => !isOn(item) && item.name.toLowerCase().includes(needle))
    .sort((a, b) => (a.name.toLowerCase() < b.name.toLowerCase() ? -1 : a.name.toLowerCase() > b.name.toLowerCase() ? 1 : 0));
  const change = async (item: Listed) => Boolean(await (item.app ? connections.toggle(item.app) : connections.toggleNotion(item.notion!)));
  const inherited = (item: Listed) => item.notion?.reach === "parent";
  return {
    listed,
    active: listed.filter(isOn),
    addable,
    /** Nothing to add matches: "결과 없음" where the list would be. */
    noneFound: connections.loaded && listed.length > 0 && addable.length === 0,
    busy: connections.changing || !connections.loaded || !connections.manageable,
    change,
    /** Asked and answered: null when not taken off, else whether it was. */
    disconnect: async (item: Listed) => await confirmAction(inherited(item) ? {
      message: t("connections.unlinkInheritedTitle", { name: item.name }), detail: t("connections.unlinkInheritedDetail"),
      confirmLabel: t("connections.unlinkInheritedConfirm"), danger: "filled", compact: true, titleSm: true,
      icon: <UnlinkSharingIcon size="card" aria-hidden="true" />,
    } : {
      message: t("connections.removeTitle", { name: item.name }), detail: t("connections.removeDetail", { name: item.name }),
      confirmLabel: t("connections.removeConfirm"), danger: "filled", titleSm: true,
    }) ? change(item) : null,
  };
}

/**
 * "연결 ›" beside the menu, as Notion's (seen 2026-11-13): "활성화된 연결",
 * each the page has on two lines — its name over who made it — and "›" for
 * where it comes from, what it may do and "연결 해제"; then "+ 연결 추가하기",
 * which turns the box to "← 연결 추가하기" — a search and the connections the
 * page lacks, "연결 관리" and "개발자 포털" — and "연결 관리" (the settings'
 * connections). A page with none opens on the connections it may add: the
 * search with the caret in it, "새 연결 추가하기" over them, "연결 관리" and
 * "개발자 포털", with no way back to a list it doesn't have (Notion's, seen
 * 2026-11-16 in Chrome at 1440 on People's page and on a page's "더 보기 ›").
 */
export function RowMenuConnectionsSubmenu({ row, connections, onClose, onAnswered = onClose }: {
  row: HTMLElement;
  connections: PageConnections;
  onClose: () => void;
  /** Closes it and the lists it stands in once "연결 해제" is answered. */
  onAnswered?: () => void;
}) {
  const { t } = useTranslation("rowMenu");
  const [adding, setAdding] = useState(false);
  const [query, setQuery] = useState("");
  const [detail, setDetail] = useState<{ key: string; row: HTMLElement; byKeys?: boolean } | null>(null);
  const hover = useRef(0);
  const lists = usePageConnectionLists(connections, query);
  const { listed, active, addable, noneFound, busy } = lists;
  const shown = detail ? active.find((item) => item.key === detail.key) : undefined;
  // With none, the connections it may add are all the box has.
  const firstAdd = active.length === 0;
  const keepsKeys = !firstAdd && !adding;
  // Opened by the keys, its first connection is shaded until the pointer comes in.
  const [shade, setShade] = useState(keysLeadNow);
  const firstActive = useRef<HTMLButtonElement | null>(null);
  const first = active[0]?.key;
  const close = useRef(onClose);
  close.current = onClose;
  // With connections it opens with the keys left on its row, as Notion's (seen
  // 2026-11-18 at 1440 in Chrome in two tabs, judged by the screen with no
  // script before the action, a page with three connections: → on "연결 3 ›"
  // opened the list, the row still ringed and the first connection grey; ↓
  // went on to the next row, closing the list; → again opened that first
  // connection's "›" with the keys in it, ← closed it, the row ringed again,
  // and ← once more closed the list). Enter opens it the same way: Notion's
  // closed "더 보기 ›" (ringed then), which left the keys nowhere near the
  // list they asked for. With none it opens on its search with the caret in it.
  useEffect(() => {
    if (!keepsKeys) return undefined;
    row.setAttribute(BESIDE_KEEPS_KEYS, "");
    const menu = row.closest<HTMLElement>("[data-native-menu]");
    const key = (event: KeyboardEvent) => {
      if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      if (event.key === inlineStartKey(row)) {
        event.preventDefault(); event.stopPropagation();
        close.current();
      } else if (event.key === inlineEndKey(row) && first && firstActive.current) {
        event.preventDefault();
        setDetail({ key: first, row: firstActive.current, byKeys: true });
      }
    };
    // The keys moving on in the row's list close this one.
    const moved = (event: KeyboardEvent) => {
      if (isMenuMoveKey(event.key) && event.target instanceof Node && menu?.contains(event.target)) close.current();
    };
    row.addEventListener("keydown", key);
    document.addEventListener("keydown", moved, true);
    return () => {
      row.removeAttribute(BESIDE_KEEPS_KEYS);
      row.removeEventListener("keydown", key);
      document.removeEventListener("keydown", moved, true);
    };
  }, [keepsKeys, row, first]);

  async function change(item: Listed) {
    if (await lists.change(item)) {
      setDetail(null);
      setAdding(false);
      setQuery("");
    }
  }
  // Answered, the box and the lists it stands in close, leaving the page
  // menu, as Notion's under its question (seen 2026-11-21, yes or "취소");
  // a refusal stays in the box to be read.
  async function disconnect(item: Listed) {
    const done = await lists.disconnect(item);
    if (done === false) setDetail(null);
    else onAnswered();
  }
  function go(href: string) {
    onClose();
    pushRoute(href);
  }
  // The page a connection comes through opens in place of the page, the
  // box and the lists it stands in closed, as Notion's (seen 2026-11-21).
  function openSource(pageId: string) {
    onAnswered();
    pushRoute(pageHref(pageId));
  }
  // The pointer on another row closes a connection's "›", as moving off a row closes Notion's.
  const leave = () => {
    window.clearTimeout(hover.current);
    setDetail(null);
    setShade(false);
  };
  const manage = (
    <button type="button" role="menuitem" className={styles.submenuItem} data-page-connections-manage onPointerEnter={leave}
      onClick={() => go(`/settings?section=integrations&from=${encodeURIComponent(`${window.location.pathname}${window.location.search}`)}`)}>
      <Settings size="regular" aria-hidden="true" />
      <span>{t("connections.manage")}</span>
    </button>
  );
  const notes = <>
    {connections.loaded && !connections.manageable ? <p className={styles.submenuNote} data-page-connections-locked>{t("connections.fullAccessOnly")}</p> : null}
    {connections.message ? <p className={styles.submenuNote} role="alert" data-error>{connections.message}</p> : null}
  </>;
  const mark = (item: Listed) => item.app ? { "data-page-app": item.app.appId } : { "data-page-notion-connection": item.notion!.grantId };
  const glyph = (item: Listed) => <span className={styles.submenuGlyph} aria-hidden="true">{item.name.slice(0, 1).toUpperCase()}</span>;
  const named = (item: Listed) => (
    <span className={own.name}>
      <span>{item.name}</span>
      {item.creator ? <small>{t("connections.creator", { name: item.creator })}</small> : null}
    </span>
  );

  return (
    <RowMenuBesideSubmenu row={row} width={358} label={t("actions.connections")} onClose={onClose} focusFirst={!keepsKeys}>
      {adding || firstAdd ? <>
        {firstAdd ? null : (
          <div className={own.addHead}>
            <button type="button" aria-label={t("connections.back")} onClick={() => { setAdding(false); setQuery(""); }}>
              <ArrowLeft size="small" aria-hidden="true" />
            </button>
            <span>{t("connections.add")}</span>
          </div>
        )}
        <label className={styles.submenuSearch}>
          <input autoFocus={fieldFocusOnOpen()} value={query} placeholder={t("connections.search")} aria-label={t("connections.search")}
            onChange={(event) => setQuery(event.target.value)} />
        </label>
        {firstAdd && addable.length ? <div className={styles.sectionLabel} data-page-connections-new>{t("connections.addNew")}</div> : null}
        {connections.loaded && listed.length === 0 ? <p className={styles.submenuNote}>{t("connections.empty")}</p> : null}
        {noneFound ? <p className={`${styles.menuEmpty} ${own.none}`} data-page-connections-none>{t("noResults")}</p> : null}
        {addable.map((item) => (
          <button key={item.key} type="button" role="menuitem" className={`${styles.submenuItem} ${item.creator ? own.twoLines : ""}`}
            disabled={busy} title={item.creator ? t("connections.creator", { name: item.creator }) : undefined}
            onClick={() => void change(item)} {...mark(item)}>
            {glyph(item)}
            {named(item)}
          </button>
        ))}
        {notes}
        <div className={styles.divider} role="separator" />
        {manage}
        <button type="button" role="menuitem" className={styles.submenuItem} data-page-connections-developers
          onClick={() => go(developerToolsHref("start"))}>
          <ArrowUpRightIcon size="regular" aria-hidden="true" />
          <span>{t("connections.developerPortal")}</span>
        </button>
      </> : <>
        {active.length ? <div className={styles.sectionLabel}>{t("connections.active")}</div> : null}
        {active.map((item, index) => (
          <button key={item.key} ref={index === 0 ? firstActive : undefined} type="button" role="menuitem" aria-haspopup="menu"
            aria-expanded={detail?.key === item.key} className={`${styles.submenuItem} ${item.creator ? own.twoLines : ""} ${
              shade && index === 0 ? own.shaded : ""}`} data-connected="true" {...mark(item)}
            onClick={(event) => setDetail({ key: item.key, row: event.currentTarget })}
            onPointerEnter={(event) => {
              const target = event.currentTarget;
              setShade(false);
              window.clearTimeout(hover.current);
              hover.current = window.setTimeout(() => setDetail({ key: item.key, row: target }), 150);
            }}
            onPointerLeave={() => window.clearTimeout(hover.current)}>
            {glyph(item)}
            {named(item)}
            <ChevronRight className={styles.itemChevron} size="small" aria-hidden="true" />
          </button>
        ))}
        {notes}
        {active.length ? <div className={styles.divider} role="separator" /> : null}
        <button type="button" role="menuitem" className={styles.submenuItem} data-page-connections-add
          disabled={!connections.loaded} onPointerEnter={leave} onClick={() => { setDetail(null); setAdding(true); }}>
          <Plus size="regular" aria-hidden="true" />
          <span>{t("connections.add")}</span>
        </button>
        <div className={styles.divider} role="separator" />
        {manage}
      </>}
      {shown && detail && !adding ? (
        <RowMenuBesideSubmenu row={detail.row} width={184} label={shown.name} focusFirst={detail.byKeys === true}
          returnTo={detail.byKeys ? row : undefined} onClose={() => setDetail(null)}>
          <ConnectionDetail item={shown} disabled={busy} onDisconnect={() => void disconnect(shown)} onOpenSource={openSource} />
        </RowMenuBesideSubmenu>
      ) : null}
    </RowMenuBesideSubmenu>
  );
}

/**
 * A connection's "›", as Notion's (seen 2026-11-13): "연결 출처" and the page
 * it comes through when that is a page above (none for the page's own, seen
 * 2026-11-21), its name a button that opens that page, "기능" with what it
 * may do (✓) and may not (✕), "연결 해제" and who made it. An app has only
 * "연결 해제".
 */
function ConnectionDetail({ item, disabled, onDisconnect, onOpenSource }: {
  item: Listed; disabled: boolean; onDisconnect: () => void; onOpenSource: (pageId: string) => void;
}) {
  const { t } = useTranslation("rowMenu");
  const notion = item.notion;
  const can = new Set<string>(notion?.capabilities ?? []);
  return <>
    {notion ? <>
      {notion.reach === "parent" ? <ConnectionSource notion={notion} className={own.source} onOpen={onOpenSource} /> : null}
      <div className={own.capsLabel}>{t("connections.capabilities")}</div>
      {CAPABILITIES.map((capability) => (
        <div key={capability} className={own.capability} data-capability={capability} data-on={can.has(capability) ? "true" : "false"}>
          {can.has(capability) ? <CheckIcon size="small" aria-hidden="true" /> : <X size="small" aria-hidden="true" />}
          <span>{t(`connections.capability.${capability}.${can.has(capability) ? "yes" : "no"}`)}</span>
        </div>
      ))}
      <div className={`${styles.divider} ${own.capsEnd}`} role="separator" />
    </> : null}
    <button type="button" role="menuitem" className={styles.submenuItem} data-page-connection-disconnect disabled={disabled}
      onClick={onDisconnect}>
      <DisconnectConnectionIcon size="regular" aria-hidden="true" />
      <span>{t("connections.disconnect")}</span>
    </button>
    {item.creator ? <>
      <div className={styles.divider} role="separator" />
      <p className={`${styles.submenuNote} ${own.creator}`}>{t("connections.creator", { name: item.creator })}</p>
    </> : null}
  </>;
}

/**
 * "연결 출처" and the page a connection comes through, as Notion's (seen
 * 2026-11-13): that page's own icon — its empty or lined document without
 * one, a database's table — and its name, or what one without a name is
 * called anywhere else, "새 데이터베이스" for a database, "A 및 B" for one of
 * several sources by their titles (pageTitle.ts). Given onOpen, the icon and
 * name are a button that opens that page, as Notion's beside the menu (seen
 * 2026-11-21 in Chrome at 1440 in two tabs: pressed, the menus closed and
 * the tab went to that page).
 */
export function ConnectionSource({ notion, className, mark = 12, onOpen }: {
  notion: PublicMutationNotionConnection; className: string; mark?: number; onOpen?: (pageId: string) => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("rowMenu");
  const source = {
    title: notion.sourceTitle ?? "", kind: notion.sourceKind ?? "page", viewBlock: notion.sourceViewBlock === true, sourceTitles: notion.sourceTitles,
    icon: notion.sourceIcon ?? "", iconType: notion.sourceIconType ?? "none", hasContent: notion.sourceHasContent === true,
  } as const;
  const named = <>
    <PageIconGlyph page={source} size={mark} />
    <strong>{pageNames.title(source)}</strong>
  </>;
  const pageId = notion.sourceId;
  return (
    <div className={className} data-connection-source={pageId}>
      <span>{t("connections.source")}</span>
      {onOpen && pageId ? <button type="button" data-connection-source-open onClick={() => onOpen(pageId)}>{named}</button> : named}
    </div>
  );
}
