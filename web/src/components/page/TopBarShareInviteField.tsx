import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createFocusManager } from "@react-aria/focus";
import { useTranslation } from "react-i18next";
import { ArrowUpLeftIcon, CheckIcon, GlobeIcon, PersonInviteIcon, XMarkIcon } from "@/icons/leafen";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import type { PublicMutationPrincipalOption, PublicMutationRole } from "@/publicMutationWire.generated";
import { TopBarSharePermissionPicker } from "./TopBarSharePermissionPicker";
import { SharePersonMark } from "./SharePersonMark";
import { sharePrincipalNote, type SelectedPrincipal } from "./topBarShareModel";
import { styles } from "./topBarStyles";
import access from "./TopBarShareAccess.module.css";
import inherited from "./TopBarShareInherited.module.css";
import address from "./TopBarShareInviteAddress.module.css";
import { SelfMark } from "@/components/people/SelfMark";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

export type ShareAlreadyShared = { key: string; label: string; email: string; roleLabel: string; isYou: boolean; userId?: string; guest?: boolean; restricted?: boolean };

const keyOf = (item: { principalType: string; principalId: string }) => `${item.principalType}:${item.principalId}`;

/** What a comma or a chosen row finished that is no address, as Notion's (seen 2026-11-04). */
export function InvalidInviteNote({ names }: { names: string[] }) {
  const { t } = useTranslation("topBar");
  if (!names.length) return null;
  return <p className={access.inviteInvalid} role="alert" data-share-invite-invalid>
    {names.length === 1 ? t("inviteInvalidEmail", { name: names[0] }) : t("inviteInvalidEmails", { name: names[0], count: names.length - 1 })}</p>;
}

/**
 * A chosen invitee in the field, as Notion's (seen 2026-11-12): 24px, 2px 4px
 * in, 14px words. Someone the share makes a guest — an address, or a guest of
 * the workspace — is in the guest colour (dark: rgb(80,68,37) under
 * rgb(249,243,220)) after a yellow globe; a member has their face, 16px.
 */
export function InviteChip({ chip, disabled, onRemove }: { chip: SelectedPrincipal; disabled: boolean; onRemove(): void }) {
  const { t } = useTranslation("topBar");
  const guest = chip.principalType === "email" || chip.guest === true;
  return <span className={access.chip} data-share-invite-chip data-guest={guest ? "true" : undefined}>
    {chip.principalType === "email" ? <GlobeIcon className={access.chipGlobe} size="small" aria-hidden="true" />
      : chip.principalType === "user" ? <PersonAvatar userId={chip.principalId} name={chip.label} size={16} /> : null}
    <span className={access.chipLabel}>{chip.label}</span>
    {/* Gone with its chip, the × takes the focus with it unless the field's owner gives it somewhere (data-focus-leaves). */}
    <button type="button" aria-label={t("removeInviteChip", { name: chip.label })} disabled={disabled} data-focus-leaves
      onClick={onRemove}><XMarkIcon size="small" aria-hidden="true" /></button>
  </span>;
}

/** The invite field, as Notion's (seen 2026-10-06): the people chosen are
 * chips in the field with the role beside them, and typing turns the tab
 * into the invite step listing who it isn't shared with yet and who already
 * has it. Choosing only prepares the invite; the caller's submit shares. */
export function TopBarShareInviteField({ inputRef, dialogRef, query, onQuery, chips, onRemoveChip, candidates, onToggle, addressRows, invalidInvites,
  alreadyShared, alreadySharedRole, inviting, disabled, busy, visible, focusRequest, role, onRole, onSubmit, onOpen, hasMore, onMore, message, onMessage }: {
  inputRef: RefObject<HTMLInputElement | null>; dialogRef: RefObject<HTMLDivElement | null>; query: string; onQuery(next: string): void;
  chips: SelectedPrincipal[]; onRemoveChip(chip: SelectedPrincipal): void;
  candidates: PublicMutationPrincipalOption[]; onToggle(option: PublicMutationPrincipalOption): void;
  addressRows: boolean; invalidInvites: string[];
  alreadyShared: ShareAlreadyShared[]; alreadySharedRole(row: ShareAlreadyShared): ReactNode; inviting: boolean;
  disabled: boolean; busy: boolean; visible: boolean; focusRequest: number; role: PublicMutationRole; onRole(next: PublicMutationRole): void;
  message: string; onMessage(next: string): void;
  onSubmit(): void; onOpen(): void; hasMore: boolean; onMore(): void;
}) {
  const { t } = useTranslation(["topBar", "siteBuilder"]);
  const listRef = useRef<HTMLDivElement>(null), ready = useRef(false);
  const focusedRequest = useRef(-1);
  // Someone chosen with the pointer leaves the field without its ring until a
  // key goes to it, as Notion's (seen 2026-11-12); chosen with Enter it keeps it.
  const pointerPick = useRef(false), [quiet, setQuiet] = useState(false);
  const chosen = new Set(chips.map(keyOf));
  const open = inviting && !disabled && candidates.length > 0;
  // The message box shows for any chip. Notion's invite step asks no "페이지에 액세스하면 알림 받기" for
  // any chip — a member, a restricted member, a guest or an address, alone or together (seen 2026-11-11
  // in the free workspace, 2026-11-21 in two tabs of the business trial and the free workspace).
  const askMessage = inviting && !addressRows && (chips.length > 0 || Boolean(message.trim()));
  const picker = useNativeAutocomplete({ items: candidates, query, onQuery,
    inputRef, listRef, popoverRef: dialogRef, label: t("topBar:invitePeople"), open, commitTab: false,
    // The people suggested or found are lit by the arrows alone, ↓ the first
    // and ↑ the last, past either end the field's own step with none lit, as
    // Notion's (seen 2026-11-14); a typed address's row is lit at once. Enter
    // with none lit does nothing while words typed list people — a known
    // person's whole address too, which "공유하기" shares with them (seen
    // 2026-11-16 in two tabs) — and is the field's own (onSubmit) otherwise.
    highlightFirst: addressRows, wrap: "throughNone", onEnterNone: event => { if (query.trim()) event.preventDefault(); },
    keyOf: item => keyOf(item), textOf: item => item.label,
    disabledKeys: disabled ? candidates.map(keyOf) : [],
    onPick: item => { setQuiet(pointerPick.current); pointerPick.current = false; onToggle(item); } });
  useLayoutEffect(() => {
    if (!visible || disabled || ready.current && focusedRequest.current === focusRequest || !inputRef.current?.parentElement) return;
    const focused = createFocusManager({ current: inputRef.current.parentElement }, { accept: element => element === inputRef.current }).focusFirst();
    if (focused) { ready.current = true; focusedRequest.current = focusRequest; }
  }, [disabled, inputRef, visible, focusRequest]);
  return <>
    <div className={access.inviteRow}>
      {/* The chips sit in one wrapper before the field, so the field keeps
          its place, and its focus, as people are chosen; with any chosen the
          field takes a line of its own under them, the role at the top right
          (Notion's, seen 2026-11-12). */}
      <div className={access.inviteBox} data-disabled={disabled ? "true" : undefined} data-has-chips={chips.length ? "true" : undefined}
        data-quiet={quiet ? "true" : undefined}>
        <span className={access.inviteEntries}>
          {/* A chip's × leaves the focus with no field, the next key going nowhere, and the invite step
              stays though the last chip went (TopBarShare), as Notion's share window's (seen 2026-11-15
              and 11-16, one letter typed after the press in fresh tabs). */}
          <span className={access.chips}>{chips.map(chip => <InviteChip key={keyOf(chip)} chip={chip} disabled={disabled}
            onRemove={() => onRemoveChip(chip)} />)}</span>
          <input {...picker.inputProps} ref={inputRef} placeholder={chips.length ? "" : t("topBar:invitePlaceholder")} disabled={disabled}
            // A click, not the focus it opens with, turns to the suggestions, as Notion's (seen 2026-11-05).
            onClick={event => { picker.inputProps.onClick?.(event); if (!inviting) onOpen(); }}
            onKeyDown={event => {
              setQuiet(false);
              if (imeComposing(event)) return;
              if (event.key === "Backspace" && !query && chips.length) { event.preventDefault(); onRemoveChip(chips[chips.length - 1]); return; }
              if (open) picker.inputProps.onKeyDown?.(event);
              if (fieldEnter(event) && !event.defaultPrevented) { event.preventDefault(); onSubmit(); }
            }} />
        </span>
        {inviting ? <span className={access.inviteRole}>
          <TopBarSharePermissionPicker invite layout="inline" disabled={disabled} value={role} onChange={onRole} />
        </span> : null}
      </div>
      {/* Nothing chosen yet, "공유하기" opens the invite step on its suggestions, as Notion's (seen 2026-10-27). */}
      <button type="button" className={access.inviteSubmit} data-menu-item
        disabled={disabled || (inviting && !query.trim() && !chips.length)}
        onClick={() => { if (!inviting && !query.trim() && !chips.length) { onOpen(); inputRef.current?.focus(); } else onSubmit(); }}>
        {t("topBar:invite")}</button>
    </div>
    <InvalidInviteNote names={invalidInvites} />
    {inviting ? <div data-share-invite-candidates>
      {alreadyShared.length ? <>
        <div className={access.sectionLabel}>{t("topBar:alreadyShared")}</div>
        {alreadyShared.map(row => <div key={row.key} className={access.candidate} data-share-already-shared>
          {row.userId
            ? <PersonAvatar className={styles.shareSuggestionAvatar} userId={row.userId} name={row.label} size={32} />
            : <span className={styles.shareSuggestionAvatar} aria-hidden="true">{row.label.trim().slice(0, 1).toUpperCase() || "?"}</span>}
          <span className={styles.shareSuggestionText}><span className={inherited.name}><span>{row.label}{row.isYou ? <SelfMark className={styles.shareYou} text={t("topBar:youMark")} /> : null}</span>
            <SharePersonMark guest={row.guest} restricted={row.restricted} /></span>
            {row.email ? <span>{row.email}</span> : null}</span>
          <span className={access.candidateRole}>{alreadySharedRole(row) ?? row.roleLabel}</span>
        </div>)}
      </> : null}
      {candidates.length ? <>
        <div className={addressRows ? `${access.sectionLabel} ${address.hint}` : access.sectionLabel}>
          {t(addressRows ? "topBar:inviteTypeEmail" : query.trim() ? "topBar:notSharedYet" : "topBar:suggested")}</div>
        <div {...picker.listBoxProps} ref={listRef} aria-label={t("topBar:organizationPeople")}
          onPointerDownCapture={() => { pointerPick.current = true; }}>
          {candidates.map(option => <NativePickerOption key={keyOf(option)} state={picker.state} itemKey={keyOf(option)} element="div"
            className={addressRows ? address.row : access.candidate} data-share-principal-option={option.principalType}>
            {({ labelProps, descriptionProps }) => addressRows ? <>
              <PersonInviteIcon size="regular" aria-hidden="true" />
              <span {...labelProps}>“{option.label}”</span>
              <ArrowUpLeftIcon size="small" aria-hidden="true" />
            </> : <>
              {option.principalType === "user"
                ? <PersonAvatar className={styles.shareSuggestionAvatar} userId={option.principalId} name={option.label} size={32} />
                : <span className={styles.shareSuggestionAvatar} aria-hidden="true">
                  {option.label.trim().slice(0, 1).toUpperCase() || "?"}</span>}
              <span className={styles.shareSuggestionText}><span className={inherited.name}><span {...labelProps}>{option.label}</span>
                <SharePersonMark guest={option.guest} restricted={option.restricted} /></span>
                <span {...descriptionProps}>{sharePrincipalNote({ principalType: option.principalType, label: option.label, email: option.email, principalId: option.principalId }, t)}</span></span>
              <span className={access.check} data-checked={chosen.has(keyOf(option)) ? "true" : undefined} aria-hidden="true">
                {chosen.has(keyOf(option)) ? <CheckIcon size="micro" /> : null}</span>
            </>}
          </NativePickerOption>)}
        </div>
      </> : null}
      {hasMore ? <button type="button" className={styles.shareAccessLoadMore} disabled={busy} onClick={onMore}>
        {t("siteBuilder:navigationLoadMore")}</button> : null}
    </div> : null}
    {askMessage ? <textarea className={access.inviteMessage} aria-label={t("topBar:inviteMessage")} placeholder={t("topBar:inviteMessage")}
      value={message} onChange={event => onMessage(event.currentTarget.value)} maxLength={2000} rows={2} disabled={disabled} data-share-invite-message /> : null}
  </>;
}
