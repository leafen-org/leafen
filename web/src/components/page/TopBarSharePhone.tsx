import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetHeadButton, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { CheckIcon, ChevronRight, EnvelopeLetterIcon, Search, XMarkIcon } from "@/icons/leafen";
import { InvalidInviteNote } from "./TopBarShareInviteField";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { publicMutationRoles, type PublicMutationPrincipalOption, type PublicMutationRole } from "@/publicMutationWire.generated";
import { sharePermissionDescription, sharePermissionLabel } from "./TopBarSharePermissionPicker";
import { isInviteAddress, type SelectedPrincipal } from "./topBarShareModel";
import css from "./TopBarSharePhone.module.css";
import inviteCss from "./TopBarSharePhoneInvite.module.css";
import { SharePersonMark } from "./SharePersonMark";
import { SelfMark } from "@/components/people/SelfMark";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// A phone's share sheet, as Notion's "공유 설정" (seen 2026-10-27 at 375px):
// a field that opens the invite screen, the people with their role under
// their name and "⋯", and the rest under a line (TopBarShareAccessPanel).

export const sharePhoneStyles = css;

const keyOf = (item: { principalType: string; principalId: string }) => `${item.principalType}:${item.principalId}`;

/** The grey field that opens the invite screen. */
export function SharePhoneSearch({ disabled, onOpen }: { disabled: boolean; onOpen(): void }) {
  const { t } = useTranslation("topBar");
  return <button type="button" className={css.search} disabled={disabled} data-share-phone-search onClick={onOpen}>
    <Search size="page" aria-hidden="true" /><span>{t("phoneInviteSearch")}</span>
  </button>;
}

/** A person's name with "(나)" and a guest's mark, and their role under it. */
export function SharePhonePersonText({ name, you, guest, role }: { name: string; you: boolean; guest?: ReactNode; role: ReactNode }) {
  const { t } = useTranslation("topBar");
  return <span className={css.personText}>
    <span className={css.personName}><span>{name}{you ? <SelfMark className={css.you} text={t("youMark")} /> : null}</span>{guest}</span>
    <span className={css.personRole}>{role}</span>
  </span>;
}

/** A person's row: their face, name and role, and what it ends with (its "⋯"). */
export function SharePhonePerson({ face, children, data }: { face: ReactNode; children: ReactNode; data?: Record<`data-${string}`, string | undefined> }) {
  return <div className={css.person} data-share-row {...data}>{face}{children}</div>;
}

/**
 * The invite screen, as Notion's "사용자 초대" (seen 2026-10-27): "취소" and
 * "초대" at its head; the field, the people chosen as chips in it; "사용 권한
 * 수준" with the role, its choices in a sheet over it; then "사용자 선택",
 * who it isn't shared with yet — the person inviting first as "〈이름〉(나)"
 * where theirs comes from above (useShareInviteDraft) — each chosen one
 * leaving the list for the field (seen 2026-11-12 at 402pt).
 * A workspace owner's, a guest's or a restricted member's tag ends their row.
 * Leaving with someone chosen or typed asks first.
 */
export function SharePhoneInvite({ query, onQuery, chips, onRemoveChip, candidates, onToggle, addressRows, invalidInvites, role, onRole, disabled, busy, hasMore, onMore,
  selfId, onSubmit, onCancel }: {
  query: string; onQuery(next: string): void; chips: SelectedPrincipal[]; onRemoveChip(chip: SelectedPrincipal): void;
  candidates: PublicMutationPrincipalOption[]; onToggle(option: PublicMutationPrincipalOption): void;
  addressRows: boolean; invalidInvites: string[];
  role: PublicMutationRole; onRole(next: PublicMutationRole): void; disabled: boolean; busy: boolean; hasMore: boolean; onMore(): void;
  selfId: string; onSubmit(): void; onCancel(): void;
}) {
  const { t } = useTranslation(["topBar", "common", "siteBuilder"]);
  const input = useRef<HTMLInputElement>(null);
  const [roles, setRoles] = useState(false);
  // The field takes the keys as the screen opens, a frame after the tap, as Notion's. Measured 2026-11-16 after the
  // press only (no script in the page before it): on a Galaxy A90's Chrome (two tabs 20 minutes apart, and again half an
  // hour later) the grey field tapped, the keyboard up at once and "z" typed went into the field; in iPhone Safari at
  // 402 (three openings, 03:38–03:39) the keyboard stayed down until the field was tapped — focused outside the tap,
  // iOS leaves it down. One earlier look (03:0x) saw the iPhone's up at once; what decides that is not found.
  // A touch screen's focus comes a frame on as before, nothing held (focusOpenedField, loaded apart: the
  // top bar is on the first screen's path, the caret's rules with the editor).
  useEffect(() => {
    let stop: (() => void) | null = null, gone = false;
    void import("@/components/editor/openedFieldCaret").then(({ focusOpenedField }) => {
      if (!gone) stop = focusOpenedField(() => input.current, { scroll: false });
    });
    return () => { gone = true; stop?.(); };
  }, []);
  const chosen = new Set(chips.map(keyOf));
  // Only people chosen ask before leaving; words typed alone go, as Notion's (seen 2026-10-28).
  const cancel = () => {
    if (!chips.length) { onCancel(); return; }
    void confirmAction({ message: t("topBar:discardInviteTitle"), confirmLabel: t("topBar:discardInvite"), cancelLabel: t("topBar:keepInvite") })
      .then(discard => { if (discard) onCancel(); else input.current?.focus(); });
  };
  const listed = candidates.filter(option => !chosen.has(keyOf(option)));
  // Words that aren't an address yet ask for one over what was typed; a whole address is offered under
  // "사용자 선택", as Notion's phone ("z" and "guest@op.st", seen 2026-11-16 in iPhone Safari at 402).
  const askAddress = addressRows && !isInviteAddress(query.trim());
  return <PhoneSheet size="screen" headEven title={t("topBar:invitePeople")} onClose={cancel}
    start={<PhoneSheetHeadButton label={t("common:actions.cancel")} onPress={cancel} />}
    action={{ label: t("topBar:inviteSend"), onPress: onSubmit, disabled: disabled || busy || (!query.trim() && !chips.length) }}
    data={{ "data-share-invite-step": "", "data-share-phone-invite": "" }}>
    <div className={inviteCss.screen}>
      <div className={inviteCss.inviteField} data-chips={chips.length ? "true" : undefined}>
        {chips.map(chip => <span key={keyOf(chip)} className={inviteCss.chip} data-share-invite-chip>
          <span>{chip.label}</span>
          <button type="button" aria-label={t("topBar:removeInviteChip", { name: chip.label })} disabled={disabled}
            onClick={() => { onRemoveChip(chip); input.current?.focus(); }}><XMarkIcon size="tiny" aria-hidden="true" /></button>
        </span>)}
        {/* An email field, as Notion's (type email, autocomplete off, seen 2026-11-16 on a Galaxy A90's Chrome): the
            email keyboard, small letters with "@", "." and ".com" (iPhone Safari's with "@" and "."). */}
        <input ref={input} type="email" value={query} placeholder={chips.length ? "" : t("topBar:phoneInvitePlaceholder")} disabled={disabled}
          autoComplete="off"
          aria-label={t("topBar:invitePeople")} onChange={event => onQuery(event.currentTarget.value)}
          onKeyDown={event => {
            if (event.key === "Backspace" && !query && chips.length) { event.preventDefault(); onRemoveChip(chips[chips.length - 1]); }
            if (fieldEnter(event)) { event.preventDefault(); onSubmit(); }
          }} />
      </div>
      <InvalidInviteNote names={invalidInvites} />
      <p className={inviteCss.screenLabel}>{t("topBar:inviteAccessLevel")}</p>
      <button type="button" className={inviteCss.screenRow} disabled={disabled} data-share-invite-role={role} onClick={() => setRoles(true)}>
        <span className={inviteCss.screenLabelText}>{sharePermissionLabel(role, t)}</span>
        <ChevronRight className={inviteCss.screenMore} size="small" aria-hidden="true" />
      </button>
      {listed.length ? <>
        <p className={inviteCss.screenLabel}>{t(askAddress ? "topBar:inviteTypeEmail" : "topBar:invitePick")}</p>
        {listed.map(option => <button key={keyOf(option)} type="button" className={inviteCss.screenRow} data-person="" disabled={disabled}
          data-share-principal-option={option.principalType} onClick={() => onToggle(option)}>
          {/* What was typed, after a letter, as Notion's phone (seen 2026-11-16: "✉ z"). */}
          {addressRows ? <EnvelopeLetterIcon size="regular" aria-hidden="true" /> : option.principalType === "user"
            ? <PersonAvatar className={inviteCss.screenAvatar} userId={option.principalId} name={option.label} size={28} />
            : <span className={inviteCss.screenAvatar} aria-hidden="true">{option.label.trim().slice(0, 1).toUpperCase() || "?"}</span>}
          <span className={inviteCss.screenLabelText}>{option.label}
            {option.principalType === "user" && option.principalId === selfId ? <SelfMark className={inviteCss.screenYou} text={t("topBar:youMark")} /> : null}</span>
          <SharePersonMark owner={option.workspaceOwner} guest={option.guest} restricted={option.restricted} />
        </button>)}
        {hasMore ? <button type="button" className={inviteCss.screenRow} disabled={busy} onClick={onMore}>
          <span className={inviteCss.screenLabelText}>{t("siteBuilder:navigationLoadMore")}</span>
        </button> : null}
      </> : null}
    </div>
    {/* Its row names the level, its sheet changes it: "사용 권한 변경" as the people's and the web link's sheets (Notion's phone, seen 2026-11-18). */}
    {roles ? <PhoneSheet size="half" headEven closeButton title={t("topBar:changeAccess")} onClose={() => setRoles(false)}
      data={{ "data-share-invite-roles": "" }}>
      <PhoneSheetGroup>
        {[publicMutationRoles.fullAccess, publicMutationRoles.edit, publicMutationRoles.comment, publicMutationRoles.view].map(choice =>
          <PhoneSheetRow key={choice} label={sharePermissionLabel(choice, t)} description={sharePermissionDescription(choice, t) || undefined} more={false}
            value={role === choice ? <CheckIcon size="small" aria-hidden="true" /> : undefined}
            data={{ "data-share-role": choice, "data-checked": role === choice ? "true" : undefined }}
            onPress={() => { setRoles(false); onRole(choice); }} />)}
      </PhoneSheetGroup>
    </PhoneSheet> : null}
  </PhoneSheet>;
}
