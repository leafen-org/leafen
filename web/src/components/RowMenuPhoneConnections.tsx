import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ArrowUpRightIcon, CheckIcon, DisconnectConnectionIcon, Plus, X } from "@/icons/leafen";
import { developerToolsHref, pushRoute } from "@/lib/router";
import { CONNECTION_CAPABILITIES, ConnectionSource, usePageConnectionLists, type ListedConnection } from "./RowMenuConnections";
import type { usePageConnections } from "./RowMenuSubmenus";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "./shell/PhoneSheet";
import styles from "./RowMenuPhoneSheet.module.css";
import { SearchClearButton } from "./PageActionSearch";

/**
 * "연결 ›" on a phone, as Notion's (seen 2026-11-13 at 402pt): a whole sheet
 * over the menu with "완료" at its head and no title — "활성화된 연결", each
 * the page has on two lines, its name over who made it, and "›" to a sheet
 * over it with where it comes from, what it may do and "연결 해제", then who
 * made it; then "+ 연결 추가하기", which turns the sheet to "← 연결
 * 추가하기", a search, the connections the page lacks and "개발자 포털".
 * The same lists as the desktop menu's (RowMenuConnections).
 */
export function RowMenuPhoneConnections({ connections, onLeave, onBack }: {
  connections: ReturnType<typeof usePageConnections>;
  /** Closes the menu, as going elsewhere does. */
  onLeave(): void;
  /** Back to the menu. */
  onBack(): void;
}) {
  const { t } = useTranslation(["rowMenu", "common"]);
  const [adding, setAdding] = useState(false);
  const [detail, setDetail] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const lists = usePageConnectionLists(connections, query);
  const { listed, active, addable, noneFound, busy } = lists;
  const shown = detail ? active.find((item) => item.key === detail) : undefined;
  const toList = () => { setAdding(false); setDetail(null); setQuery(""); };
  async function change(item: ListedConnection) {
    if (await lists.change(item)) toList();
  }
  // Answered, the sheet goes back to the list (the question's, RowMenuConnections).
  async function disconnect(item: ListedConnection) {
    if (await lists.disconnect(item) !== false) toList();
    else setDetail(null);
  }
  const glyph = (item: ListedConnection) => <span className={styles.connectionGlyph}>{item.name.slice(0, 1).toUpperCase()}</span>;
  const mark = (item: ListedConnection) => item.app ? { "data-page-app": item.app.appId } : { "data-page-notion-connection": item.notion!.grantId };
  const creator = (item: ListedConnection) => item.creator ? t("rowMenu:connections.creator", { name: item.creator }) : undefined;
  const done = t("common:actions.done");
  const notes = <>
    {connections.loaded && !connections.manageable ? <p className={styles.note} data-page-connections-locked>{t("rowMenu:connections.fullAccessOnly")}</p> : null}
    {connections.message ? <p className={styles.note} role="alert">{connections.message}</p> : null}
  </>;

  return <>
    <PhoneSheet title="" label={t("rowMenu:actions.connections")} onClose={onBack} action={{ label: done, onPress: onBack }}
      data={{ "data-page-connections": adding ? "adding" : "" }}>
      {adding ? <>
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<ArrowLeft size="regular" />} label={t("rowMenu:connections.add")} onPress={toList}
            data={{ "data-page-connections-back": "" }} />
        </PhoneSheetGroup>
        <PhoneSheetGroup>
          <label className={styles.connectionSearch}>
            <input ref={searchRef} value={query} placeholder={t("rowMenu:connections.search")}
              aria-label={t("rowMenu:connections.search")} onChange={(event) => setQuery(event.target.value)} />
            <SearchClearButton value={query} inputRef={searchRef} className={styles.connectionSearchClear} onClear={() => setQuery("")} />
          </label>
        </PhoneSheetGroup>
        {addable.length ? (
          <PhoneSheetGroup>
            {addable.map((item) => (
              <PhoneSheetRow key={item.key} more={false} icon={glyph(item)} label={item.name} description={creator(item)}
                disabled={busy} onPress={() => void change(item)} data={mark(item)} />
            ))}
          </PhoneSheetGroup>
        ) : null}
        {listed.length === 0 ? <p className={styles.note}>{t("rowMenu:connections.empty")}</p> : null}
        {noneFound ? <p className={styles.note} data-page-connections-none>{t("rowMenu:noResults")}</p> : null}
        {notes}
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<ArrowUpRightIcon size="regular" />} label={t("rowMenu:connections.developerPortal")}
            onPress={() => { onLeave(); pushRoute(developerToolsHref("start")); }} data={{ "data-page-connections-developers": "" }} />
        </PhoneSheetGroup>
      </> : <>
        {active.length ? (
          <PhoneSheetGroup label={t("rowMenu:connections.active")}>
            {active.map((item) => (
              <PhoneSheetRow key={item.key} icon={glyph(item)} label={item.name} description={creator(item)}
                onPress={() => setDetail(item.key)} data={{ ...mark(item), "data-connected": "true", "data-connection-row": "" }} />
            ))}
          </PhoneSheetGroup>
        ) : null}
        {notes}
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<Plus size="regular" />} label={t("rowMenu:connections.add")} disabled={!connections.loaded}
            onPress={() => setAdding(true)} data={{ "data-page-connections-add": "" }} />
        </PhoneSheetGroup>
      </>}
    </PhoneSheet>
    {shown ? <ConnectionSheet item={shown} busy={busy} done={done} onDone={() => setDetail(null)} onDisconnect={() => void disconnect(shown)} /> : null}
  </>;
}

/** A connection's sheet over the list: "연결 출처 📄 〈page〉" for one from a page above, "기능" ✓/✕, "연결 해제", then who made it. */
function ConnectionSheet({ item, busy, done, onDone, onDisconnect }: {
  item: ListedConnection; busy: boolean; done: string; onDone(): void; onDisconnect(): void;
}) {
  const { t } = useTranslation("rowMenu");
  const notion = item.notion;
  const can = new Set<string>(notion?.capabilities ?? []);
  return (
    <PhoneSheet title="" label={item.name} onClose={onDone} action={{ label: done, onPress: onDone }} data={{ "data-page-connection-detail": item.key }}>
      {notion ? (
        <PhoneSheetGroup>
          {notion.reach === "parent" ? <ConnectionSource notion={notion} className={styles.connectionSource} mark={16} /> : null}
          <div className={styles.connectionCapabilities}>
            <span>{t("connections.capabilities")}</span>
            {CONNECTION_CAPABILITIES.map((capability) => (
              <span key={capability} data-capability={capability} data-on={can.has(capability) ? "true" : "false"}>
                {can.has(capability) ? <CheckIcon size="small" aria-hidden="true" /> : <X size="small" aria-hidden="true" />}
                {t(`connections.capability.${capability}.${can.has(capability) ? "yes" : "no"}`)}
              </span>
            ))}
          </div>
        </PhoneSheetGroup>
      ) : null}
      <PhoneSheetGroup>
        <PhoneSheetRow more={false} icon={<DisconnectConnectionIcon size="regular" />} label={t("connections.disconnect")}
          disabled={busy} onPress={onDisconnect} data={{ "data-page-connection-disconnect": "" }} />
      </PhoneSheetGroup>
      {item.creator ? <p className={styles.connectionCreator}>{t("connections.creator", { name: item.creator })}</p> : null}
    </PhoneSheet>
  );
}
