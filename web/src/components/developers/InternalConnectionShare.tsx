import { type RefObject, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ChevronDown, LinkIcon, LockIcon, SharePeopleIcon, Trash } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import {
  addInternalConnectionOwner, listInternalConnectionOwners, removeInternalConnectionOwner, type InternalConnectionOwner,
} from "@/lib/internalConnections";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { directoryPeople, subscribePeopleDirectory, type PersonDirectoryEntry } from "@/lib/peopleDirectory";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useProductToast } from "@/lib/productToast";
import { developerConnectionHref } from "@/lib/router";
import { serverUrl } from "@/lib/serverOrigin";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session, Workspace } from "@/types";
import styles from "./InternalConnectionShare.module.css";

/**
 * Who manages an internal connection, as Notion's connection page keeps it
 * (seen 2026-11-21): no "소유자" tab, but "🔒 공유 ⌄" on the top bar, whose
 * 456px window has "공유" over a line; a field to find someone by name or
 * email beside a blue "공유" that adds whoever was picked; each owner — the
 * maker marked "생성자", the person looking "(나)", their email under — with
 * "모두 허용 ⌄", whose menu has "제거하기"; the workspace's owners, who
 * always manage it; and "링크 복사" under a line.
 */
export function InternalConnectionShare({ session, workspace, connectionId }: { session: Session; workspace: Workspace; connectionId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button ref={buttonRef} type="button" className={styles.trigger} aria-expanded={open} aria-haspopup="dialog"
        data-connection-share onClick={() => setOpen((value) => !value)}>
        <LockIcon size="tiny" aria-hidden="true" />{t("developerTools.internal.share.title")}<ChevronDown size="micro" aria-hidden="true" />
      </button>
      {open ? <SharePopup session={session} workspace={workspace} connectionId={connectionId} anchor={buttonRef}
        onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function SharePopup({ session, workspace, connectionId, anchor, onClose }: {
  session: Session; workspace: Workspace; connectionId: string; anchor: RefObject<HTMLButtonElement | null>; onClose(): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { notify } = useProductToast();
  const popupRef = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const [owners, setOwners] = useState<InternalConnectionOwner[]>([]);
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<PersonDirectoryEntry | null>(null);
  const [menu, setMenu] = useState<{ owner: InternalConnectionOwner; anchor: HTMLElement } | null>(null);
  const [error, setError] = useState("");
  const [, setRevision] = useState(0);
  useEffect(() => subscribePeopleDirectory(() => setRevision((value) => value + 1)), []);
  useEffect(() => {
    const controller = new AbortController();
    void listInternalConnectionOwners(session, workspace.id, connectionId, controller.signal).then(setOwners)
      .catch((failure) => { if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("appLoadFailed"))); });
    return () => controller.abort();
  }, [connectionId, session, t, workspace.id]);
  const { zIndex } = useOverlayLayer(true, {
    kind: "picker", ref: popupRef, inside: () => [anchor.current], onOutsidePress: () => { if (!menu) onClose(); }, onEscape: () => { if (!menu) onClose(); },
    initialFocus: searchRef,
  });
  const place = usePopoverPlacement(popupRef, (size, viewport) => placeAround(popoverRectOf(anchor.current)!, size, viewport, { gap: 8, align: "end" }), [], undefined, false);
  const needle = query.trim().toLocaleLowerCase();
  const taken = useMemo(() => new Set(owners.map((owner) => owner.id)), [owners]);
  const people = picked || !needle ? [] : directoryPeople().filter((person) => !taken.has(person.userId)
    && `${person.displayName ?? ""} ${person.email ?? ""}`.toLocaleLowerCase().includes(needle)).slice(0, 8);
  async function change(run: () => Promise<InternalConnectionOwner[]>) {
    setError("");
    try {
      setOwners(await run());
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  function share() {
    if (!picked) return;
    const userId = picked.userId;
    setPicked(null);
    setQuery("");
    void change(() => addInternalConnectionOwner(session, workspace.id, connectionId, userId));
  }
  async function copyLink() {
    const ok = await copyText(serverUrl(developerConnectionHref(connectionId)));
    notify(t(ok ? "developerTools.internal.share.linkCopied" : "developerTools.internal.share.linkCopyFailed"), ok ? "success" : "error");
  }
  const label = (key: string, values?: Record<string, unknown>) => t(`developerTools.internal.share.${key}`, values);
  return createPortal(
    <div ref={popupRef} className={styles.popup} role="dialog" aria-label={label("title")}
      style={{ ...popoverPlacementStyle(place), zIndex }} data-connection-share-popup>
      <div className={styles.head}>{label("title")}</div>
      <div className={styles.body}>
        <div className={styles.inviteRow}>
          <div className={styles.fieldBox}>
            <label className={styles.field}>
              <input ref={searchRef} value={picked ? picked.displayName || picked.email || "" : query} placeholder={label("placeholder")}
                aria-label={label("placeholder")} onChange={(event) => { setPicked(null); setQuery(event.currentTarget.value); }}
                onKeyDown={(event) => { if (fieldEnter(event) && picked) { event.preventDefault(); share(); } }} />
            </label>
            {people.length ? (
              <div className={styles.suggestions} role="listbox" aria-label={label("placeholder")}>
                {people.map((person) => (
                  <button key={person.userId} type="button" role="option" aria-selected={false} className={styles.suggestion}
                    data-connection-owner-pick={person.userId} onClick={() => { setPicked(person); setQuery(""); searchRef.current?.focus(); }}>
                    <PersonAvatar userId={person.userId} size={22} />
                    <span>{person.displayName || person.email}</span>
                  </button>
                ))}
              </div>
            ) : null}
          </div>
          <button type="button" className={styles.invite} disabled={!picked} data-connection-share-submit onClick={share}>{label("submit")}</button>
        </div>
        <div className={styles.people}>
          {owners.map((owner) => (
            <div key={owner.id} className={styles.row} data-connection-owner={owner.id}>
              <PersonAvatar userId={owner.id} size={32} />
              <span className={styles.text}>
                <span className={styles.name}>{owner.name}
                  {owner.id === session.user.id ? <span className={styles.me}> {t("developerTools.me")}</span> : null}
                  {owner.creator ? <span className={styles.badge}>{t("developerTools.internal.creator")}</span> : null}</span>
                {owner.email ? <span className={styles.note}>{owner.email}</span> : null}
              </span>
              <button type="button" className={styles.role} aria-haspopup="menu" aria-expanded={menu?.owner.id === owner.id}
                data-connection-owner-role onClick={(event) => {
                  const target = event.currentTarget;
                  setMenu((current) => current?.owner.id === owner.id ? null : { owner, anchor: target });
                }}>
                {label("fullAccess")}<ChevronDown size="micro" aria-hidden="true" />
              </button>
            </div>
          ))}
          <div className={styles.row}>
            <span className={styles.mark} aria-hidden="true"><SharePeopleIcon size="small" /></span>
            <span className={styles.text}>
              <span className={styles.name}>{t("developerTools.internal.workspaceOwners", { workspace: workspace.name })}</span>
              <span className={styles.note}>{t("developerTools.internal.workspaceOwnersNote")}</span>
            </span>
          </div>
        </div>
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
      </div>
      <div className={styles.foot}>
        <button type="button" className={styles.copy} data-connection-share-copy onClick={() => void copyLink()}>
          <LinkIcon size="small" aria-hidden="true" />{label("copyLink")}</button>
      </div>
      {menu ? <RoleMenu anchor={menu.anchor} onClose={() => setMenu(null)} onRemove={() => {
        const ownerId = menu.owner.id;
        setMenu(null);
        void change(() => removeInternalConnectionOwner(session, workspace.id, connectionId, ownerId));
      }} /> : null}
    </div>,
    document.body,
  );
}

/** "모두 허용 ⌄"'s menu: "제거하기" alone, as Notion's (seen 2026-11-21). */
function RoleMenu({ anchor, onClose, onRemove }: { anchor: HTMLElement; onClose(): void; onRemove(): void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const menuRef = useRef<HTMLDivElement>(null), firstRef = useRef<HTMLButtonElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "menu", ref: menuRef, inside: () => [anchor], onOutsidePress: onClose, onEscape: onClose, initialFocus: firstRef });
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(popoverRectOf(anchor)!, size, viewport, { gap: 4, align: "end" }), [], undefined, false);
  return createPortal(
    <div ref={menuRef} className={styles.menu} role="menu" style={{ ...popoverPlacementStyle(place), zIndex }} data-connection-owner-menu>
      <button ref={firstRef} type="button" role="menuitem" className={styles.menuItem} data-connection-owner-remove onClick={onRemove}>
        <Trash size="regular" aria-hidden="true" />{t("developerTools.internal.removeOwner")}</button>
    </div>,
    document.body,
  );
}
