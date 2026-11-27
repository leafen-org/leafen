import { SwitchMark } from "@/components/Switch";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { CustomEmojiAddDialog } from "@/components/page/CustomEmojiAddDialog";
import { CustomEmojiImage } from "@/components/page/CustomEmojiImage";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { ArrowDown, DotsHorizontal, Pencil, Search, SmileyIcon, Trash } from "@/icons/leafen";
import {
  deleteWorkspaceCustomEmoji,
  listWorkspaceCustomEmojis,
  type WorkspaceCustomEmoji,
} from "@/lib/customEmoji";
import { invalidateWorkspaceCustomEmojiIcon } from "@/lib/customEmojiIcon";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  changeWorkspaceConnectionPolicies,
  readWorkspaceConnectionPolicies,
  type WorkspaceConnectionPolicies,
} from "@/lib/workspaceConnectionPolicies";
import type { Session, Workspace } from "@/types";
import styles from "./EmojiSettingsSection.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";

/**
 * The features' "이모지" item, as Notion's (seen 2026-10-02, and its help):
 * the workspace's custom emoji with who added each, "이모지 추가" with the
 * same window as the emoji picker's, and editing or deleting what the
 * reader added (the owner, any), and the owner's "워크스페이스 소유자의 커스텀
 * 이모지", which keeps new emoji to owners while those added stay their
 * adders' to change.
 *
 * Its list, as Notion's (seen 2026-11-02): the count, a search by name or
 * adder and the blue "이모지 추가", then a table of picture, name, adder and
 * date added, newest first, each row's "⋯" opening "편집" (the add window,
 * for its picture and name) and "삭제" (asked in the middle of the window).
 */
export function EmojiSettingsSection({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t, i18n } = useTranslation("workspaceSettingsDialog");
  const [items, setItems] = useState<WorkspaceCustomEmoji[] | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  // The button the add or edit window hangs from, as Notion's (seen 2026-11-01).
  const [adding, setAdding] = useState<HTMLElement | null>(null);
  const [editing, setEditing] = useState<{ emoji: WorkspaceCustomEmoji; anchor: HTMLElement } | null>(null);
  const [menuFor, setMenuFor] = useState<WorkspaceCustomEmoji | null>(null);
  const menuButton = useRef<HTMLButtonElement | null>(null);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // A failed read, in the list's place (or after what was read) with a way to
  // read the same window again; a change that failed is `error`.
  const [readFailed, setReadFailed] = useState<{ text: string; after: string | null } | null>(null);
  const [canAdd, setCanAdd] = useState(true);
  const [policies, setPolicies] = useState<WorkspaceConnectionPolicies | null>(null);

  const load = useCallback(async (after: string | null, signal?: AbortSignal) => {
    try {
      const page = await listWorkspaceCustomEmojis(session, workspace.id, { cursor: after ?? undefined, limit: 100, signal });
      setItems((current) => after ? [...(current ?? []), ...page.items] : page.items);
      setCursor(page.hasMore ? page.nextCursor : null);
      setCanAdd(page.canAdd !== false);
      setReadFailed(null);
    } catch (failure) {
      if (!signal?.aborted) setReadFailed({ text: settingsErrorMessage(failure, t("emojiLoadFailed")), after });
    }
  }, [session, t, workspace.id]);

  useEffect(() => {
    setItems(null);
    setError("");
    setReadFailed(null);
    const controller = new AbortController();
    void load(null, controller.signal);
    readWorkspaceConnectionPolicies(session, workspace.id, controller.signal).then(setPolicies).catch(() => undefined);
    return () => controller.abort();
  }, [load, session, workspace.id]);

  const dateFormat = useMemo(() => new Intl.DateTimeFormat(i18n.language, { year: "numeric", month: "long", day: "numeric" }), [i18n.language]);
  const shown = useMemo(() => {
    const folded = query.trim().toLocaleLowerCase();
    return [...(items ?? [])]
      .filter((emoji) => !folded || emoji.name.toLocaleLowerCase().includes(folded)
        || (emoji.creatorName ?? "").toLocaleLowerCase().includes(folded))
      .sort((a, b) => b.createdAt - a.createdAt);
  }, [items, query]);

  async function changeOwnersOnly() {
    if (!policies) return;
    setBusy("policy");
    setError("");
    try {
      setPolicies(await changeWorkspaceConnectionPolicies(session, workspace.id, { customEmojiOwnersOnly: !policies.customEmojiOwnersOnly }));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("emojiChangeFailed")));
    } finally {
      setBusy("");
    }
  }

  async function remove(emoji: WorkspaceCustomEmoji) {
    const confirmed = await confirmAction({ message: t("emojiDeleteTitle"), detail: t("emojiDeleteDetail"),
      confirmLabel: t("emojiDelete"), cancelLabel: t("emojiCancel"), danger: "filled", wide: true });
    if (!confirmed) return;
    setBusy(emoji.id);
    setError("");
    try {
      await deleteWorkspaceCustomEmoji(session, workspace.id, emoji.id, { baseRevision: emoji.revision });
      invalidateWorkspaceCustomEmojiIcon(workspace.id, emoji.id);
      setItems((current) => current?.filter((item) => item.id !== emoji.id) ?? current);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("emojiChangeFailed")));
    } finally {
      setBusy("");
    }
  }

  const empty = items !== null && items.length === 0;
  return (
    <div data-emoji-settings>
      {policies?.canManage ? (
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("emojiOwnersOnlyField")}</strong>
            <span>{t("emojiOwnersOnlyDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={policies.customEmojiOwnersOnly}
            aria-label={t("emojiOwnersOnlyField")}
            disabled={busy === "policy"}
            data-emoji-owners-only
            onClick={() => void changeOwnersOnly()}
          >
            <SwitchMark on={policies.customEmojiOwnersOnly} />
          </button>
        </div>
      ) : null}
      <hr className={styles.divider} />
      {empty ? (
        <div className={styles.empty}>
          <SmileyIcon pixels={64} className={styles.emptyMark} aria-hidden="true" />
          <p className={styles.emptyText}>{t("emojiEmpty")}</p>
          {canAdd ? (
            <button type="button" className={styles.emptyAdd} onClick={(event) => setAdding(event.currentTarget)}>{t("emojiAdd")}</button>
          ) : null}
        </div>
      ) : items ? (
        <section role="region" aria-label={t("navEmoji")}>
          <div className={styles.bar}>
            <span className={styles.count}>{t("emojiCount", { count: items.length })}</span>
            {searching ? (
              <input className={styles.searchField} value={query} autoFocus placeholder={t("emojiSearch")} aria-label={t("emojiSearch")}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== "Escape") return;
                  event.stopPropagation();
                  setQuery("");
                  setSearching(false);
                }} />
            ) : null}
            <button type="button" className={styles.searchButton} aria-label={t("emojiSearch")} title={t("emojiSearch")}
              aria-pressed={searching} onClick={() => { setSearching((open) => !open); setQuery(""); }}>
              <Search size="regular" aria-hidden="true" />
            </button>
            {canAdd ? (
              <button type="button" className={styles.add} onClick={(event) => setAdding(event.currentTarget)}>{t("emojiAdd")}</button>
            ) : null}
          </div>
          <div className={styles.table} role="table" aria-label={t("navEmoji")}>
            <div className={styles.head} role="row">
              <span role="columnheader">{t("emojiColumnPicture")}</span>
              <span role="columnheader">{t("emojiColumnName")}</span>
              <span role="columnheader">{t("emojiColumnAddedBy")}</span>
              <span role="columnheader" aria-sort="descending" className={styles.sorted}>
                {t("emojiColumnAdded")}<ArrowDown pixels={10} aria-hidden="true" />
              </span>
              <span role="columnheader" aria-hidden="true" />
            </div>
            {shown.map((emoji) => (
              <div key={emoji.id} className={styles.row} role="row" data-custom-emoji={emoji.id}>
                <span role="cell" className={styles.picture} aria-hidden="true"><CustomEmojiImage emoji={emoji} /></span>
                <span role="cell" className={styles.text}>{emoji.name}</span>
                <span role="cell" className={styles.text}>{emoji.creatorName ?? ""}</span>
                <span role="cell" className={styles.text}>{dateFormat.format(emoji.createdAt)}</span>
                <span role="cell">
                  {emoji.canChange ? (
                    <button type="button" className={styles.more} aria-label={t("emojiEditNamed", { name: emoji.name })}
                      aria-haspopup="menu" aria-expanded={menuFor?.id === emoji.id} disabled={busy === emoji.id}
                      onClick={(event) => { menuButton.current = event.currentTarget; setMenuFor(emoji); }}>
                      <DotsHorizontal size="regular" aria-hidden="true" />
                    </button>
                  ) : null}
                </span>
              </div>
            ))}
          </div>
          {cursor ? (
            <button type="button" className={styles.loadMore} onClick={() => void load(cursor)}>{t("emojiMore")}</button>
          ) : null}
        </section>
      ) : null}
      {readFailed ? (
        <ListNotice role="alert" onRetry={() => { setReadFailed(null); void load(readFailed.after); }} data-emoji-read-failed>{readFailed.text}</ListNotice>
      ) : null}
      {error ? <div className={styles.error} role="alert">{error}</div> : null}
      {menuFor ? (
        <SidebarActionMenu button={menuButton} label={t("emojiEditNamed", { name: menuFor.name })} width={180} gap={0} shift={156}
          backdropLabel={t("emojiCancel")} close={() => setMenuFor(null)}
          rows={[
            { key: "edit", label: t("emojiEdit"), icon: <Pencil size="regular" aria-hidden="true" />,
              run: () => { const anchor = menuButton.current; setMenuFor(null); if (anchor) setEditing({ emoji: menuFor, anchor }); } },
            { key: "delete", label: t("emojiDelete"), icon: <Trash size="regular" aria-hidden="true" />,
              run: () => { setMenuFor(null); void remove(menuFor); } },
          ]} />
      ) : null}
      {adding ? (
        <CustomEmojiAddDialog anchor={adding} uploadTarget={{ workspaceId: workspace.id }}
          onClose={() => setAdding(null)}
          onAdded={() => { setAdding(null); void load(null); }} />
      ) : null}
      {editing ? (
        <CustomEmojiAddDialog anchor={editing.anchor} uploadTarget={{ workspaceId: workspace.id }} editing={editing.emoji}
          onClose={() => setEditing(null)}
          onAdded={(updated) => {
            setEditing(null);
            invalidateWorkspaceCustomEmojiIcon(workspace.id, updated.id);
            setItems((current) => current?.map((item) => item.id === updated.id
              ? { ...item, ...updated, creatorName: item.creatorName, canChange: item.canChange } : item) ?? current);
          }} />
      ) : null}
    </div>
  );
}
