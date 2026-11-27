import { useRef, useState, type ReactNode, type RefObject } from "react";
import { usePendingChoice } from "@/lib/pendingChoice";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles } from "@/components/shell/PhoneSheet";
import { CheckIcon, ChevronDown, DotsHorizontal, Trash } from "@/icons/leafen";
import { publicMutationRoles, type PublicMutationRole } from "@/publicMutationWire.generated";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { SwitchMark } from "@/components/Switch";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { styles } from "./topBarStyles";
import access from "./TopBarShareAccess.module.css";
import inherited from "./TopBarShareInherited.module.css";
import phoneStyles from "./TopBarSharePhone.module.css";
import roleStyles from "./TopBarSharePhoneRole.module.css";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";

/** A role in the share window's menus: one that gives access, or — for a
 * teamspace's owners or members — none, as Notion's "사용 권한 없음". */
export type ShareRoleChoice = PublicMutationRole | "none";

const shareRoleRanks: readonly ShareRoleChoice[] = ["none", publicMutationRoles.view, publicMutationRoles.comment,
  publicMutationRoles.edit, publicMutationRoles.fullAccess];

/** Where a role stands among the others, none lowest; a role not given is none. */
export const shareRoleRank = (role?: string) => Math.max(0, shareRoleRanks.indexOf((role || "none") as ShareRoleChoice));

export function sharePermissionLabel(role: ShareRoleChoice, t: ReturnType<typeof useTranslation>["t"]) {
  if (role === "none") return t("topBar:noAccess");
  if (role === publicMutationRoles.fullAccess) return t("topBar:fullAccess");
  if (role === publicMutationRoles.edit) return t("topBar:canEdit");
  if (role === publicMutationRoles.comment) return t("topBar:canComment");
  return t("topBar:canView");
}

/** The line under a role in its menu, as Notion's (seen 2026-10-06). */
export function sharePermissionDescription(role: ShareRoleChoice, t: ReturnType<typeof useTranslation>["t"]) {
  if (role === publicMutationRoles.fullAccess) return t("topBar:fullAccessDescription");
  if (role === publicMutationRoles.edit) return t("topBar:canEditDescription");
  if (role === publicMutationRoles.comment) return t("topBar:canCommentDescription");
  return "";
}

/**
 * The role an invite starts at, in the share window and in "초대와 멘션":
 * read, where Notion's start at full access — narrow first. As
 * Notion's, a window keeps the role picked for the next address typed in it,
 * and one opened anew starts here again (seen 2026-11-14, page and row page).
 */
export const inviteStartRole: PublicMutationRole = publicMutationRoles.view;

/** Native ACL values and exact permission IDs remain with the caller. */
export type ShareRoleToggle = { label: string; checked: boolean; onChange(checked: boolean): void };

type SharePermissionLayout = "inline" | "mention" | "full";

export function TopBarSharePermissionPicker({ value, disabled, invite = false, layout, onChange, onRemove, onOpen, minimumRole,
  current, held = false, roles = [publicMutationRoles.fullAccess, publicMutationRoles.edit, publicMutationRoles.comment, publicMutationRoles.view],
  removeLabel, note, more, person, target, removeMark, sheetOpen, onSheetOpen, extra, removeHead, toggle, readOnly = false }: {
  value: ShareRoleChoice; disabled: boolean; invite?: boolean; minimumRole?: PublicMutationRole;
  /**
   * Where it stands, which its own look follows: "inline" inside the invite
   * field (small and plain), "mention" as inline but 20 tall with 4px corners
   * in a mention's invite field (InviteAndMentionPanel), "full" across a
   * form's width with its chevron at the end — set here, not by the form
   * reaching into it.
   */
  layout?: SharePermissionLayout;
  /**
   * Where the role comes from: the menu heads with "현재 사용 권한", the role
   * and this under it, as Notion's every person's menu (seen 2026-10-28:
   * "〈page〉 사용자 사용 권한 권한에 따라"; a page above's, 2026-10-11).
   */
  current?: ReactNode;
  /** No choice here changes it (the workspace owner's full access): the menu shows it, its choices and "제거" greyed. */
  held?: boolean;
  /** The roles offered, and what the last row that takes it away says. */
  roles?: ShareRoleChoice[]; removeLabel?: string;
  /** A line under the roles, as Notion's teamspace rows say changing one overrides the pages above. */
  note?: string;
  /** On a phone, a person's row opens it from its "⋯", the sheet headed by them (person), as Notion's "사용 권한 변경" (seen 2026-10-27). */
  more?: boolean; person?: ReactNode;
  /** A general access row's sheet heads with whom it opens the page to, on a band across it (TopBarSharePhoneRole). */
  target?: ReactNode;
  /** Its "제거" under a bin, as the workspace's row's in Notion's phone and iPad sheet (seen 2026-11-16). */
  removeMark?: boolean;
  /** On a phone, the sheet opened from outside too, as choosing the workspace opens its own (TopBarShareGeneralAccess). */
  sheetOpen?: boolean; onSheetOpen?(open: boolean): void;
  /** A phone sheet's own rows past the roles, such as copying an invite's link. */
  extra?: ReactNode;
  /** What heads "제거"'s group in a phone sheet, as the web link's expiry (TopBarShareWebExpiryPhone). */
  removeHead?: ReactNode;
  /** A switch past the roles, as Notion's "검색 결과에서 숨기기" (seen 2026-11-05). */
  toggle?: ShareRoleToggle;
  /** Someone who can't change sharing reads the role as words, as Notion's (seen 2026-11-04); a phone's row has it under the name already. */
  readOnly?: boolean;
  onChange(role: ShareRoleChoice): void; onRemove?(): void; onOpen?(): void;
}) {
  const { t } = useTranslation("topBar"), trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { phone } = useSidebarLayout(true);
  if (readOnly) return more ? null : <span className={`${styles.sharePermission} ${styles.sharePermissionFixed}`} data-share-role-read>{sharePermissionLabel(value, t)}</span>;
  if (phone) return <PhoneSharePermissionSheet value={value} disabled={disabled} invite={invite} onChange={onChange} onRemove={onRemove} onOpen={onOpen}
    minimumRole={minimumRole} current={current} held={held} toggle={toggle} roles={roles} removeLabel={removeLabel} note={note}
    more={more} person={person} target={target} removeMark={removeMark} sheetOpen={sheetOpen} onSheetOpen={onSheetOpen} extra={extra} removeHead={removeHead} />;
  return <DesktopSharePermissionPicker value={value} disabled={disabled} invite={invite} layout={layout} onChange={onChange} onRemove={onRemove}
    onOpen={onOpen} minimumRole={minimumRole} current={current} held={held} toggle={toggle} roles={roles} removeLabel={removeLabel} note={note}
    trigger={trigger} menu={menu} />;
}

/** The menu's roles, the ones below the least it may give greyed. */
function roleDisabled(role: ShareRoleChoice, disabled: boolean, minimumRole?: PublicMutationRole) {
  return disabled || Boolean(minimumRole && shareRoleRanks.indexOf(role) < shareRoleRanks.indexOf(minimumRole));
}

function DesktopSharePermissionPicker({ value, disabled, invite, layout, onChange, onRemove, onOpen, minimumRole, current, held, toggle, roles,
  removeLabel, note, trigger, menu }: {
  value: ShareRoleChoice; disabled: boolean; invite: boolean; layout?: SharePermissionLayout; minimumRole?: PublicMutationRole;
  current?: ReactNode; held: boolean; roles: ShareRoleChoice[]; removeLabel?: string; note?: string; toggle?: ShareRoleToggle;
  onChange(role: ShareRoleChoice): void; onRemove?(): void; onOpen?(): void;
  trigger: RefObject<HTMLButtonElement | null>; menu: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation("topBar");
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled, { onOpenChange: next => { if (next) onOpen?.(); } });
  // The role picked shows at once while its change is out (lib/pendingChoice.ts).
  const pending = usePendingChoice(value, disabled);
  // Shown whole: short of room under its button it goes up over it, as
  // Notion's role menu does (seen 2026-11-07 at 1440×583).
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 4, whole: true },
  ) : null, [state.isOpen], undefined, { kind: "picker", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  return <span className={`${invite ? styles.shareInvitePermissionWrap : styles.sharePermissionWrap} ${layout === "full" ? styles.sharePermissionFull : ""}`}
    data-share-permission-root>
    <button {...buttonProps} ref={trigger} data-menu-item className={`${invite ? styles.shareInvitePermissionButton : styles.sharePermissionButton} ${
      layout === "inline" ? styles.sharePermissionInline : layout === "mention" ? `${styles.sharePermissionInline} ${styles.sharePermissionMention}`
        : layout === "full" ? styles.sharePermissionFullButton : ""}`}
      data-waiting-note={pending.note}>
      {sharePermissionLabel(pending.shown, t)} <ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {/* Placed under its button by the measured size alone: the menu's own
        stylesheet anchors it inside the row, which stretched it across the
        window once it moved to the page's top layer. */}
    {state.isOpen ? createPortal(<div ref={menu} className={`${styles.sharePermissionMenu} ${access.roleMenu}`}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto" }}>
      {placement && current !== undefined ? <div className={inherited.current} data-share-current-access>
        <span className={inherited.heading}>{t("currentAccess")}</span>
        <span className={inherited.currentRole}><span>{sharePermissionLabel(value, t)}</span>
          <span>{current}</span></span>
        <span className={inherited.heading}>{t("personAccess")}</span>
      </div> : null}
      <NativeActionMenu {...menuProps} aria-label={t(invite ? "newInvitePermission" : "whoHasAccess")} autoFocus={autoFocus}>
        {roles.map(role =>
          <NativeActionMenuItem id={role} key={role} disabled={roleDisabled(role, disabled || held, minimumRole)}
            role="menuitemradio" aria-label={sharePermissionLabel(role, t)} aria-checked={value === role}
            onClick={() => { state.close(); if (role !== value) pending.pick(role); onChange(role); }}>
            <span className={access.roleText}><span>{sharePermissionLabel(role, t)}</span>
              {sharePermissionDescription(role, t) ? <span>{sharePermissionDescription(role, t)}</span> : null}</span>
            {value === role ? <CheckIcon size="small" className={access.roleCheck} aria-hidden="true" /> : null}
          </NativeActionMenuItem>)}
        {/* A person's "제거" is plain under a bin, as Notion's (seen 2026-10-28); a rule's own word stays red. */}
        {toggle ? <NativeActionMenuItem id="toggle" role="menuitemcheckbox" aria-label={toggle.label} aria-checked={toggle.checked} disabled={disabled}
          className={access.roleToggle} data-share-role-toggle onClick={() => toggle.onChange(!toggle.checked)}>
          <span>{toggle.label}</span><SwitchMark on={toggle.checked} />
        </NativeActionMenuItem> : null}
        {onRemove || held ? <NativeActionMenuItem id="remove" disabled={disabled || held}
          className={removeLabel ? `${styles.sharePermissionDanger} ${access.roleRemove}` : access.roleRemove}
          onClick={() => { state.close(); onRemove?.(); }}>
          {removeLabel ?? <><Trash size="regular" aria-hidden="true" />{t("removeAccess")}</>}
        </NativeActionMenuItem> : null}
      </NativeActionMenu>
      {placement && note ? <p className={inherited.roleNote} data-share-role-note>{note}</p> : null}
    </div>, document.body) : null}
  </span>;
}

/**
 * A phone's role picker, as Notion's "사용 권한 변경" half sheet (seen
 * 2026-10-27): a person's row opens it from its "⋯" (more), headed by them;
 * where the role comes from (current), "현재 사용 권한" first; then the
 * roles, 17px with a grey line under each and ✓ at the chosen one, and a red
 * row that takes the access away — its rows sized by their words (fit).
 */
function PhoneSharePermissionSheet({ value, disabled, invite, onChange, onRemove, onOpen, minimumRole, current, held, toggle, roles, removeLabel,
  note, more, person, target, removeMark, sheetOpen, onSheetOpen, extra, removeHead }: {
  value: ShareRoleChoice; disabled: boolean; invite: boolean; minimumRole?: PublicMutationRole;
  current?: ReactNode; held: boolean; roles: ShareRoleChoice[]; removeLabel?: string; note?: string; more?: boolean; person?: ReactNode; extra?: ReactNode; removeHead?: ReactNode; toggle?: ShareRoleToggle;
  target?: ReactNode; removeMark?: boolean; sheetOpen?: boolean; onSheetOpen?(open: boolean): void;
  onChange(role: ShareRoleChoice): void; onRemove?(): void; onOpen?(): void;
}) {
  const { t } = useTranslation(["topBar", "common"]);
  const [ownOpen, setOwnOpen] = useState(false);
  const open = sheetOpen ?? ownOpen;
  const setOpen = (next: boolean) => { setOwnOpen(next); onSheetOpen?.(next); };
  const show = () => { onOpen?.(); setOpen(true); };
  const pick = (role: ShareRoleChoice) => { setOpen(false); if (role !== value) onChange(role); };
  const remove = onRemove || held ? <PhoneSheetGroup fit className={removeMark ? roleStyles.removeMark : undefined}>
    <PhoneSheetRow icon={removeMark ? <Trash size="regular" /> : undefined} label={removeLabel ?? t("topBar:removeAccess")} danger more={false}
      disabled={disabled || held} data={{ "data-share-remove": "" }}
      onPress={() => { setOpen(false); onRemove?.(); }} />
  </PhoneSheetGroup> : null;
  return <>
    {more ? <button type="button" className={phoneStyles.more} aria-label={t("topBar:changeAccess")} aria-haspopup="dialog" aria-expanded={open}
      data-share-permission-more onClick={show}><DotsHorizontal size="regular" aria-hidden="true" /></button>
      : <button type="button" className={invite ? styles.shareInvitePermissionButton : styles.sharePermissionButton} data-menu-item disabled={disabled}
        aria-haspopup="dialog" aria-expanded={open} onClick={show}>
        {sharePermissionLabel(value, t)} <ChevronDown size="tiny" aria-hidden="true" />
      </button>}
    {open ? <PhoneSheet size="half" headEven closeButton title={t("topBar:changeAccess")} onClose={() => setOpen(false)}
      data={{ "data-share-permission-sheet": "" }}>
      {target ? <div className={roleStyles.target} data-share-permission-target>{target}</div> : null}
      {person ? <div className={phoneStyles.sheetPerson}>{person}</div> : null}
      {current !== undefined ? <PhoneSheetGroup label={t("topBar:currentAccess")}>
        <div className={phoneStyles.current} data-share-current-access>
          <span>{sharePermissionLabel(value, t)}</span><span>{current}</span>
        </div>
      </PhoneSheetGroup> : null}
      <PhoneSheetGroup fit label={current !== undefined || person ? t("topBar:personAccess") : undefined}>
        {roles.map(role => <PhoneSheetRow key={role} label={sharePermissionLabel(role, t)} description={sharePermissionDescription(role, t) || undefined}
          disabled={roleDisabled(role, disabled || held, minimumRole)} more={false} value={value === role ? <CheckIcon size="small" aria-hidden="true" /> : undefined}
          data={{ "data-share-role": role, "data-checked": value === role ? "true" : undefined }} onPress={() => pick(role)} />)}
      </PhoneSheetGroup>
      {toggle ? <PhoneSheetGroup>
        <PhoneSheetRow label={toggle.label} disabled={disabled} more={false} value={<SwitchMark on={toggle.checked} />}
          data={{ "data-share-role-toggle": "" }} onPress={() => toggle.onChange(!toggle.checked)} />
      </PhoneSheetGroup> : null}
      {extra}
      {/* What stands with "제거" (the web link's expiry) heads its group (TopBarSharePhoneRole .removeHeaded). */}
      {removeHead ? <div className={roleStyles.removeHeaded}>{removeHead}{remove}</div> : remove}
      {note ? <p className={phoneSheetStyles.groupNote} data-share-role-note>{note}</p> : null}
    </PhoneSheet> : null}
  </>;
}
