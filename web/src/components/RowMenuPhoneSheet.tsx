import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ArchiveIcon, ArrowBendUpRightIcon, Bell, CheckIcon, ClipboardIcon, UpdatesClockIcon, ConnectionsIcon, Copy, Download, ExportIcon, EyeIcon,
  LinkIcon, LockIcon, PageLayoutIcon, PhoneShareIcon, RepeatIcon, SlidersIcon, Star, StarFilled, Trash, UnlockIcon,
  VersionHistoryIcon, LogOutIcon,
} from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, PhoneSheetSwitch } from "./shell/PhoneSheet";
import type { PagePresentationChange } from "./page/pagePresentation";
import type { usePageConnections } from "./RowMenuSubmenus";
import { RowMenuPhoneConnections } from "./RowMenuPhoneConnections";
import { lockedPageRows, phoneActionsUnderLock } from "./lockedPageMenu";
import { RowMenuPhoneCustomize, type RowMenuPhoneLockForMe } from "./RowMenuPhoneCustomize";
import type { GuestDuplicateToWorkspace } from "./useGuestDuplicateToWorkspace";
import type { GuestSidebarRows } from "./useGuestRowMenu";
import styles from "./RowMenuPhoneSheet.module.css";

const FONTS = ["default", "serif", "mono"] as const;

/** The style the sheet shows and changes: the fonts' tiles and 페이지 사용자 지정's rows. */
export type RowMenuPhoneStyle = {
  font?: string; smallText?: boolean; fullWidth?: boolean;
  tableOfContentsDisplay?: string; pageCommentsDisplay?: string; inlineCommentsDisplay?: string;
};

export type RowMenuPhoneActions = {
  /** What the menu is of: a page, a wiki's own page, a full-page database or a database's page. */
  kind: "page" | "wiki" | "database" | "row";
  canEdit: boolean;
  /** The page is in the trash, or under a page that is: its export, its updates and the foot alone. */
  trashed?: boolean;
  /** Whether its contents may be copied or exported: not by a guest, whatever they can edit. */
  canExport: boolean;
  busy: boolean;
  lockedForMe: boolean;
  style: RowMenuPhoneStyle;
  /** Changes the style; without it there are no font tiles. */
  onStyle?: (change: PagePresentationChange) => void;
  favorite?: { on: boolean; onToggle(): void };
  onShare?: () => void;
  onCopyLink(): void;
  onCopyContents?: () => void;
  onDuplicate?: () => void;
  onDuplicateWithoutContent?: () => void;
  onDuplicateToPrivate?: () => void;
  /** A guest's "페이지 복제 ›", its places in a half sheet as the desktop menu's beside it (useGuestDuplicateToWorkspace). */
  duplicateToWorkspace?: GuestDuplicateToWorkspace;
  /** "옮기기": opens the move where it is, or lists its places in a half sheet. */
  move?: { onPress(): void } | { sheet(close: () => void): ReactNode };
  /** "옮기기" shown dimmed: a guest without full access, as the desktop menu's (useRowMenuGroups' canMove). */
  moveDisabled?: boolean;
  onTrash?: () => void;
  /** "휴지통으로 이동" shown dimmed: a guest's page under a page they only read (useGuestEditsPageAbove). */
  trashDisabled?: boolean;
  onCustomizeLayout?: () => void;
  /** A database's page whose database can't change its layout now (its lock): that row faint, 페이지 사용자 지정 beside it. */
  layoutHeld?: boolean;
  /** A database's page: changes the comments its database's layout shows, which the database's lock leaves to this reader. */
  layoutComments?: (change: PagePresentationChange) => void;
  /** Its switch; a locked page's own unlock or lock again (`forMe`) heads 페이지 사용자 지정. */
  lock?: { on: boolean; label: string; onToggle(): void; forMe?: RowMenuPhoneLockForMe };
  archive?: { label: string; onToggle(): void };
  onExport?: () => void;
  /** Undoing a wiki asks over the closed sheet (confirmWikiUndo). */
  wiki?: { label: string; onToggle(): void };
  onAnalytics?: () => void;
  onHistory?: () => void;
  onReadLog?: () => void;
  notify?: {
    /** The level now, in gray at the row's end. */
    value: string;
    current: string | null;
    choices: { value: string; label: string }[];
    disabled?: boolean;
    onChoose(value: string): void;
    onRetry?: () => void;
  };
  /** "연결 ›": no "연결 관리" on a phone, as Notion's (seen 2026-11-13). */
  connections?: { value: string; state: ReturnType<typeof usePageConnections> };
  offline?: { on: boolean; onToggle(): void };
  /** Who edited last, at the sheet's foot. */
  foot?: ReactNode;
};

type Opened = "customize" | "notify" | "connections" | "duplicate" | "workspaces" | "move" | null;

/**
 * A page's "…" on a phone, as Notion's "작업" sheet (seen 2026-10-27 at
 * 375px): the fonts as three tiles, then rows in groups — favorite, share,
 * link, contents, duplicate, move, trash | 페이지 사용자 지정 (a database's
 * page: 레이아웃 사용자 지정, and both while its database is locked, seen
 * 2026-11-16 on the A90) | lock | export | wiki | updates, history |
 * notifications › | connections › — each of those two its own group (seen
 * 2026-11-25 on the A90 in Chrome at 411: a page's and a database's page's
 * sheet in two tabs, a database's once) — and who edited it last at its
 * foot. A row with › opens a half sheet over it. Leafen's own rows join
 * the groups they belong to: archive by the lock, the read log by the
 * history, offline last. Notion's "AI로 사용", "번역" and
 * "앱에서 열기" aren't here (no AI in the editor, the page's open-in-app
 * band), nor is "편집 제안": Notion's phone offers no suggesting (its code
 * leaves the row out on a phone; seen 2026-11-18 in iPhone Safari at 402, a
 * page's and a database's page's sheet).
 */
export function RowMenuPhoneSheet({ actions: given, onClose }: { actions: RowMenuPhoneActions; onClose(): void }) {
  const { t } = useTranslation(["rowMenu", "topBar", "common"]);
  const [opened, setOpened] = useState<Opened>(null);
  const actions = phoneActionsUnderLock(given);
  const { kind, canEdit, busy, lockedForMe, style, onStyle } = actions, layoutHeld = kind === "row" && actions.layoutHeld;
  const ownPage = kind === "page" || kind === "row";
  const styleLocked = busy || lockedForMe;
  const press = (run: () => void) => () => { if (!busy) run(); };
  const close = () => setOpened(null);
  const move = actions.move;

  // A page in the trash keeps its export and its updates, as Notion's phone
  // sheet (seen 2026-11-15 twice, iPhone Safari 402 dark: "번역 ›",
  // "내보내기", "페이지 업데이트", "iOS 앱에서 열기", then who edited it last,
  // when and its word count) and the desktop "…" (useRowMenuGroups).
  if (actions.trashed) return (
    <PhoneSheet title={t("rowMenu:phone.title")} titleLarge onClose={onClose} data={{ "data-page-phone-actions": kind, "data-page-trashed": "" }}
      onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); onClose(); } }}>
      {actions.onExport ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<ExportIcon size="regular" />} label={t("rowMenu:actions.export")} disabled={!actions.canExport} onPress={actions.onExport} />
        </PhoneSheetGroup>
      ) : null}
      {actions.onAnalytics ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<UpdatesClockIcon size="regular" />} label={t("rowMenu:phone.updates")} onPress={actions.onAnalytics} />
        </PhoneSheetGroup>
      ) : null}
      {actions.foot ? <PhoneSheetGroup className={styles.foot}>{actions.foot}</PhoneSheetGroup> : null}
    </PhoneSheet>
  );

  // "페이지 사용자 지정" takes the sheet's place, over the page, as Notion's (seen 2026-11-16 on the A90); closed, so is the menu.
  // A database's page keeps its comments in its database's layout, every page's, which its lock leaves to change here (Notion's, 2026-11-16).
  if (opened === "customize") return (
    <RowMenuPhoneCustomize kind={kind} style={style} onStyle={busy ? undefined : kind === "row" ? actions.layoutComments : onStyle} locked={lockedForMe} onClose={onClose}
      lockForMe={actions.lock?.on ? actions.lock.forMe : undefined} />
  );

  return (
    <PhoneSheet title={t("rowMenu:phone.title")} titleLarge onClose={onClose} data={{ "data-page-phone-actions": kind }}
      onKeyDown={(event) => { if (event.key === "Escape" && !opened) { event.stopPropagation(); onClose(); } }}>
      {ownPage && canEdit && onStyle && lockedPageRows(lockedForMe).fonts ? (
        <PhoneSheetGroup className={styles.fonts}>
          {FONTS.map((font) => (
            <button key={font} type="button" className={styles.font} data-font={font} data-active={(style.font ?? "default") === font ? "true" : undefined}
              aria-pressed={(style.font ?? "default") === font} disabled={styleLocked} onClick={() => onStyle({ font })}>
              <span className={styles.fontSample}>Ag</span>
              <span>{t(`rowMenu:fonts.${font}`)}</span>
            </button>
          ))}
        </PhoneSheetGroup>
      ) : null}
      <PhoneSheetGroup>
        {actions.favorite ? (
          <PhoneSheetRow more={false} icon={actions.favorite.on ? <StarFilled size="regular" /> : <Star size="regular" />}
            label={t(actions.favorite.on ? "rowMenu:actions.removeFromFavorites" : "rowMenu:actions.addToFavorites")} onPress={press(actions.favorite.onToggle)} />
        ) : null}
        {actions.onShare ? <PhoneSheetRow more={false} icon={<PhoneShareIcon size="regular" />} label={t("topBar:share")} onPress={actions.onShare} /> : null}
        <PhoneSheetRow more={false} icon={<LinkIcon size="regular" />} label={t("rowMenu:actions.copyLink")} onPress={press(actions.onCopyLink)} />
        {ownPage && actions.onCopyContents ? (
          <PhoneSheetRow more={false} icon={<ClipboardIcon size="regular" />} label={t("rowMenu:actions.copyContents")} disabled={!actions.canExport}
            onPress={press(actions.onCopyContents)} />
        ) : null}
        {canEdit && actions.duplicateToWorkspace ? (
          <PhoneSheetRow icon={actions.duplicateToWorkspace.icon} label={actions.duplicateToWorkspace.label} disabled={busy}
            onPress={() => setOpened("workspaces")} data={{ "data-page-duplicate-to-workspace": "" }} />
        ) : null}
        {canEdit && actions.onDuplicate ? (
          <PhoneSheetRow more={Boolean(actions.onDuplicateWithoutContent)} icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicate")}
            onPress={actions.onDuplicateWithoutContent ? () => setOpened("duplicate") : press(actions.onDuplicate)} />
        ) : null}
        {canEdit && actions.onDuplicateToPrivate ? (
          <PhoneSheetRow more={false} icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicateToPrivate")} onPress={press(actions.onDuplicateToPrivate)} />
        ) : null}
        {canEdit && move ? (
          <PhoneSheetRow more={"sheet" in move} icon={<ArrowBendUpRightIcon size="regular" />} label={t("rowMenu:actions.moveTo")}
            onPress={"sheet" in move ? () => setOpened("move") : press(move.onPress)} disabled={actions.moveDisabled} data={{ "data-page-move-action": "" }} />
        ) : null}
        {canEdit && actions.onTrash ? (
          <PhoneSheetRow more={false} danger icon={<Trash size="regular" />} label={t("rowMenu:actions.moveToTrash")} onPress={press(actions.onTrash)}
            disabled={actions.trashDisabled} data={{ "data-page-trash-action": "" }} />
        ) : null}
      </PhoneSheetGroup>
      {canEdit && (kind === "page" || kind === "wiki" || actions.onCustomizeLayout || layoutHeld) ? (
        <PhoneSheetGroup>
          {kind === "database" || kind === "row" ? (
            <PhoneSheetRow more={false} icon={<PageLayoutIcon size="regular" />} label={t("rowMenu:actions.customizeLayout")}
              disabled={layoutHeld || lockedForMe && kind === "database"} onPress={() => actions.onCustomizeLayout?.()} />
          ) : null}
          {kind === "page" || kind === "wiki" || layoutHeld ? (
            <PhoneSheetRow more={false} icon={<SlidersIcon size="regular" />} label={t("rowMenu:phone.customize")} onPress={() => setOpened("customize")} />
          ) : null}
        </PhoneSheetGroup>
      ) : null}
      {canEdit && (actions.lock || actions.archive) ? (
        <PhoneSheetGroup>
          {actions.lock ? (
            <PhoneSheetRow more={false} icon={actions.lock.on ? <LockIcon size="regular" /> : <UnlockIcon size="regular" />} label={actions.lock.label}
              value={<PhoneSheetSwitch on={actions.lock.on} />} onPress={press(actions.lock.onToggle)} />
          ) : null}
          {actions.archive ? <PhoneSheetRow more={false} icon={<ArchiveIcon size="regular" />} label={actions.archive.label} onPress={press(actions.archive.onToggle)} /> : null}
        </PhoneSheetGroup>
      ) : null}
      {actions.onExport ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<ExportIcon size="regular" />} label={t("rowMenu:actions.export")} disabled={!actions.canExport} onPress={actions.onExport} />
        </PhoneSheetGroup>
      ) : null}
      {canEdit && actions.wiki && (kind === "page" || kind === "wiki") ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<RepeatIcon size="regular" />} label={actions.wiki.label}
            onPress={press(actions.wiki.onToggle)} />
        </PhoneSheetGroup>
      ) : null}
      {canEdit && (actions.onAnalytics || actions.onHistory || actions.onReadLog) ? (
        <PhoneSheetGroup>
          {actions.onAnalytics ? <PhoneSheetRow more={false} icon={<UpdatesClockIcon size="regular" />} label={t("rowMenu:phone.updates")} onPress={actions.onAnalytics} /> : null}
          {actions.onHistory ? <PhoneSheetRow more={false} icon={<VersionHistoryIcon size="regular" />} label={t("rowMenu:actions.pageHistory")} onPress={actions.onHistory} /> : null}
          {actions.onReadLog ? <PhoneSheetRow more={false} icon={<EyeIcon size="regular" />} label={t("rowMenu:actions.readLog")} onPress={actions.onReadLog} /> : null}
        </PhoneSheetGroup>
      ) : null}
      {actions.notify ? (
        <PhoneSheetGroup>
          <PhoneSheetRow icon={<Bell size="regular" />} label={t("rowMenu:notify.label")} data={{ "data-page-notify-level": actions.notify.current ?? "" }}
            value={actions.notify.value} disabled={busy || actions.notify.disabled || (!actions.notify.current && !actions.notify.onRetry)}
            onPress={actions.notify.current ? () => setOpened("notify") : () => actions.notify?.onRetry?.()} />
        </PhoneSheetGroup>
      ) : null}
      {actions.connections && canEdit ? (
        <PhoneSheetGroup>
          <PhoneSheetRow icon={<ConnectionsIcon size="regular" />} label={t("rowMenu:actions.connections")} value={actions.connections.value}
            disabled={busy} onPress={() => setOpened("connections")} />
        </PhoneSheetGroup>
      ) : null}
      {actions.offline ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<Download size="regular" />} label={t("topBar:availableOffline")}
            value={<PhoneSheetSwitch on={actions.offline.on} />} onPress={actions.offline.onToggle} />
        </PhoneSheetGroup>
      ) : null}
      {actions.foot ? <PhoneSheetGroup className={styles.foot}>{actions.foot}</PhoneSheetGroup> : null}

      {opened === "duplicate" && actions.onDuplicate ? (
        <PhoneSheet size="half" title={t("rowMenu:actions.duplicate")} onClose={close} onBack={close}>
          <PhoneSheetGroup>
            <PhoneSheetRow more={false} icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicateWithContent")} onPress={press(actions.onDuplicate)} />
            {actions.onDuplicateWithoutContent ? (
              <PhoneSheetRow more={false} icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicateWithoutContent")} onPress={press(actions.onDuplicateWithoutContent)} />
            ) : null}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {opened === "workspaces" && actions.duplicateToWorkspace ? (
        <PhoneSheet size="half" title={actions.duplicateToWorkspace.label} onClose={close} onBack={close}>
          {actions.duplicateToWorkspace.groups.map((choices, at) => choices.length > 0 ? (
            <PhoneSheetGroup key={at}>
              {choices.map((choice) => <PhoneSheetRow key={choice.key} more={false} icon={choice.icon} label={choice.label} data={choice.data} onPress={press(choice.run)} />)}
            </PhoneSheetGroup>
          ) : null)}
        </PhoneSheet>
      ) : null}
      {opened === "move" && move && "sheet" in move ? (
        // The pages' "옮기기" sheet's kind, whole with "완료" closing it all, as Notion's database page's "작업 › 옮기기" (seen 2026-11-17 at 411).
        <PhoneSheet title={t("rowMenu:actions.moveTo")} onClose={onClose} action={{ label: t("common:actions.done"), onPress: onClose }}>{move.sheet(close)}</PhoneSheet>
      ) : null}
      {opened === "notify" && actions.notify ? (
        <PhoneSheet size="half" title={t("rowMenu:notify.label")} onClose={close} onBack={close}>
          <PhoneSheetGroup>
            {actions.notify.choices.map((choice) => (
              <PhoneSheetRow key={choice.value} more={false} data={{ "data-page-notify-choice": choice.value }} label={choice.label}
                value={actions.notify?.current === choice.value ? <CheckIcon size="regular" /> : undefined}
                onPress={() => { actions.notify?.onChoose(choice.value); close(); }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {opened === "connections" && actions.connections ? (
        <RowMenuPhoneConnections connections={actions.connections.state} onLeave={onClose} onBack={close} />
      ) : null}
    </PhoneSheet>
  );
}

export type RowMenuPhoneSidebarActions = {
  canEdit: boolean;
  /** A guest's sheet follows their access as the desktop row menu's (RowMenuSidebarActions, useGuestRowMenu). */
  guestRows?: GuestSidebarRows;
  /** A guest's leaving the page topping what is shared with them, beside favorite (RowMenuSidebarActions). */
  onLeave?: () => void;
  busy: boolean;
  /** The favorites' copy of the page, whose menu has no duplicate or trash. */
  favoritesList: boolean;
  favorite: { on: boolean; onToggle(): void };
  onCopyLink(): void;
  onDuplicate(): void;
  onMove(): void;
  onTrash(): void;
  archive?: { label: string; onToggle(): void };
  wiki?: { label: string; onToggle(): void };
  foot?: ReactNode;
};

/**
 * A sidebar row's "…" on a phone, as Notion's (seen 2026-10-27): the "작업"
 * sheet under "페이지" — favorite | link, duplicate, move, trash | wiki — with
 * who edited it last at its foot. Leafen's archive sits by the trash. A
 * guest's "복제 대상" opens their workspaces in a half sheet, as the page
 * sheet's "페이지 복제".
 */
export function RowMenuPhoneSidebarSheet({ actions, onClose }: { actions: RowMenuPhoneSidebarActions; onClose(): void }) {
  const { t } = useTranslation(["rowMenu", "common"]);
  const { canEdit, busy, favoritesList, guestRows } = actions;
  const press = (run: () => void) => () => { if (!busy) run(); };
  const [placesOpen, setPlacesOpen] = useState(false);
  const duplicateTo = canEdit && !favoritesList ? guestRows?.duplicateTo : undefined;
  return (
    <PhoneSheet title={t("rowMenu:phone.title")} titleLarge onClose={onClose} data={{ "data-sidebar-phone-actions": "" }}
      onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); onClose(); } }}>
      <PhoneSheetGroup label={t("rowMenu:phone.page")}>
        <PhoneSheetRow more={false} icon={actions.favorite.on ? <StarFilled size="regular" /> : <Star size="regular" />}
          label={t(actions.favorite.on ? "rowMenu:actions.removeFromFavorites" : "rowMenu:actions.addToFavorites")} onPress={press(actions.favorite.onToggle)} />
        {actions.onLeave ? <PhoneSheetRow more={false} icon={<LogOutIcon size="regular" />} label={t("rowMenu:actions.leave")} onPress={press(actions.onLeave)} /> : null}
      </PhoneSheetGroup>
      <PhoneSheetGroup>
        <PhoneSheetRow more={false} icon={<LinkIcon size="regular" />} label={t("rowMenu:actions.copyLink")} onPress={press(actions.onCopyLink)} />
        {canEdit && !favoritesList && (guestRows?.duplicate ?? true) ? (
          <PhoneSheetRow more={false} icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicate")} onPress={press(actions.onDuplicate)} />
        ) : duplicateTo ? (
          <PhoneSheetRow icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicateTo")} disabled={busy} onPress={() => setPlacesOpen(true)} />
        ) : null}
        {canEdit ? <PhoneSheetRow more={false} icon={<ArrowBendUpRightIcon size="regular" />} label={t("rowMenu:actions.moveTo")} disabled={guestRows ? !guestRows.move : false} onPress={press(actions.onMove)} /> : null}
        {canEdit && actions.archive && !guestRows ? <PhoneSheetRow more={false} icon={<ArchiveIcon size="regular" />} label={actions.archive.label} onPress={press(actions.archive.onToggle)} /> : null}
        {canEdit && !favoritesList ? (
          <PhoneSheetRow more={false} danger icon={<Trash size="regular" />} label={t("rowMenu:actions.moveToTrash")} disabled={guestRows ? !guestRows.trash : false} onPress={press(actions.onTrash)} />
        ) : null}
      </PhoneSheetGroup>
      {canEdit && actions.wiki && (guestRows?.wiki ?? true) ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<RepeatIcon size="regular" />} label={actions.wiki.label} onPress={press(actions.wiki.onToggle)} />
        </PhoneSheetGroup>
      ) : null}
      {actions.foot ? <PhoneSheetGroup className={styles.foot}>{actions.foot}</PhoneSheetGroup> : null}
      {placesOpen && duplicateTo ? (
        <PhoneSheet size="half" title={t("rowMenu:actions.duplicateTo")} onClose={() => setPlacesOpen(false)} onBack={() => setPlacesOpen(false)}>
          <PhoneSheetGroup>
            {duplicateTo.map((choice) => <PhoneSheetRow key={choice.key} more={false} icon={choice.icon} label={choice.label} data={choice.data} onPress={press(choice.run)} />)}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </PhoneSheet>
  );
}
