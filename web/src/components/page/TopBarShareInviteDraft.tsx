import { useRef, useState, type MutableRefObject, type RefObject } from "react";
import type { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { accessRequestErrorCodes } from "@/accessRequestWire.generated";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { ProductStatusSink } from "@/lib/productStatus";
import type { useOptionalProductToast } from "@/lib/productToast";
import { userErrorText } from "@/lib/settingsErrors";
import { publicMutationErrorCodes, type PublicMutationInput, type PublicMutationPrincipalOption, type PublicMutationRole, type PublicMutationState } from "@/publicMutationWire.generated";
import type { Session } from "@/types";
import { inviteStartRole, sharePermissionLabel } from "./TopBarSharePermissionPicker";
import type { ShareAlreadyShared } from "./TopBarShareInviteField";
import {
  finishInviteDraft, inviteAddressOptions, isInviteAddress, matchingPrincipalOptions, principalForInvite,
  principalForTypedAddress, shareDoneNotice, shareIsYou, type SelectedPrincipal, type ShareMutationOutcome,
} from "./topBarShareModel";
import { styles } from "./topBarStyles";

type Grant = Omit<PublicMutationInput, "requestId" | "expectedRevision">;

/**
 * The share window's invite: what is typed, the people chosen as chips, the
 * role and message they go with, and sharing with each in turn. The window
 * owns the page's sharing and every change's fence (runMutation); this owns
 * only the invite being written, which a fresh window starts empty again.
 */
export function useShareInviteDraft({ session, pageId, pageTop, initialInvite, state, stateRef, busy, setBusy, setErrorCode, runMutation,
  onStatus, toast, phone, inputRef, t }: {
  session: Session; pageId: string; initialInvite: string;
  /** The page is known to sit under no page or database (its parent read as none; a row page's window doesn't say). */
  pageTop: boolean;
  state: PublicMutationState | null; stateRef: MutableRefObject<PublicMutationState | null>;
  busy: boolean; setBusy(next: boolean): void; setErrorCode(next: string): void;
  runMutation(input: Grant): Promise<ShareMutationOutcome>;
  onStatus: ProductStatusSink; toast: ReturnType<typeof useOptionalProductToast>; phone: boolean;
  inputRef: RefObject<HTMLInputElement | null>; t: ReturnType<typeof useTranslation>["t"];
}) {
  const [draft, setDraft] = useState(initialInvite);
  const [invalidInvites, setInvalidInvites] = useState<string[]>([]);
  const [message, setMessage] = useState("");
  // "공유하기" with nothing typed opens the invite step on its suggestions, as Notion's (seen 2026-10-27).
  const [opened, setOpened] = useState(false);
  const [chips, setChips] = useState<SelectedPrincipal[]>([]);
  const pending = useRef<SelectedPrincipal | null>(null);
  const refused = useRef<Promise<boolean> | null>(null);
  const [focusRequest, setFocusRequest] = useState(0);
  const [role, setRole] = useState<PublicMutationRole>(inviteStartRole);
  // A guest only an owner may invite, whom the person asks an owner to add.
  const [guestRequest, setGuestRequest] = useState<{ principal: SelectedPrincipal; role: PublicMutationRole } | null>(null);
  const focusField = () => setFocusRequest(value => value + 1);

  // A share an owner must approve offers to ask them (the window's runMutation tells every refusal here).
  function noteRefusal(error: unknown, input: object) {
    // A person's page refuses a restricted member: Notion's window says so in
    // a notice answered "확인", with no line of its own, the person still
    // offered (seen 2026-11-16 in Chrome at 1440, in two tabs at two times).
    if (error instanceof LeafenApiError && error.code === publicMutationErrorCodes.personPageRestricted) {
      setErrorCode("");
      refused.current = confirmAction({ message: t("topBar:inviteFailed"), confirmLabel: t("topBar:inviteFailedOk"), cancelLabel: null });
      return;
    }
    if (!(error instanceof LeafenApiError && error.code === accessRequestErrorCodes.guestInviteRequiresApproval) || !("action" in input)) return;
    const grant = input as Grant;
    if (grant.action === "grant_permission" && grant.principalType && grant.principalId && grant.role) {
      setGuestRequest({ principal: pending.current ?? principalForInvite(grant.principalId, null), role: grant.role });
    }
  }

  // Everyone chosen and every address typed (comma separated, as Notion's
  // field says) is shared with in turn; the first refusal stops the rest.
  async function submit() {
    const parts = draft.split(",").map(part => part.trim()).filter(Boolean);
    // Words that are no address go nowhere: the button does nothing for them, as Notion's (seen 2026-11-04).
    if (!parts.every(isInviteAddress)) return;
    // The person sharing, chosen on a phone, has the page already: inviting them alone just closes the screen, as Notion's (seen 2026-11-12).
    // A known person's address is that person (principalForTypedAddress).
    const offered = state?.principalOptions ?? [];
    const principals = [...chips, ...parts.map(part => principalForTypedAddress(part, offered))]
      .filter(principal => principal.principalId && !(principal.principalType === "user" && principal.principalId === session.user.id));
    if (!principals.length) { if (chips.length) { setChips([]); setMessage(""); setOpened(false); } return; }
    setGuestRequest(null);
    const shared: SelectedPrincipal[] = [];
    for (const principal of principals) {
      pending.current = principal;
      const outcome = await runMutation({
        action: "grant_permission",
        principalType: principal.principalType,
        principalId: principal.principalId,
        role, inviteMessage: message,
      });
      if (outcome !== "succeeded") break;
      shared.push(principal);
    }
    pending.current = null;
    // The field takes the keys again once a refusal's notice is answered.
    const notice = refused.current;
    refused.current = null;
    if (notice) await notice;
    if (shared.length) {
      // An invitation whose mail didn't go, or went without its link, says so under it (shareDoneNotice).
      const notice = shareDoneNotice(shared, stateRef.current, t);
      if (notice.detail && toast) toast.notify(notice.message, "default", undefined, undefined, notice.detail);
      else onStatus(notice.detail ? `${notice.message}. ${notice.detail}` : notice.message, "default");
      const left = principals.slice(shared.length);
      setChips(left);
      setDraft("");
      setInvalidInvites([]);
      if (!left.length) { setMessage(""); setOpened(false); }
    }
    if (inputRef.current?.isConnected) focusField();
  }

  function changeDraft(next: string) {
    setGuestRequest(null);
    if (!next.includes(",")) { setDraft(next); return; }
    const { typing, addresses, invalid } = finishInviteDraft(next);
    const offered = state?.principalOptions ?? [];
    setChips(current => [...current, ...addresses.map(address => principalForTypedAddress(address, offered))
      .filter(principal => !current.some(chip => chip.principalType === principal.principalType && chip.principalId === principal.principalId))]);
    setInvalidInvites(current => [...current, ...invalid]);
    setDraft(typing);
    setOpened(true);
  }

  function toggleChip(option: PublicMutationPrincipalOption) {
    const key = `${option.principalType}:${option.principalId}`;
    setGuestRequest(null);
    if (option.principalType === "email" && !isInviteAddress(option.principalId)) {
      setInvalidInvites(current => [...current, option.principalId]);
      setDraft("");
      setOpened(true);
      return;
    }
    setChips(current => current.some(chip => `${chip.principalType}:${chip.principalId}` === key)
      ? current.filter(chip => `${chip.principalType}:${chip.principalId}` !== key)
      : [...current, { label: option.label, principalType: option.principalType, principalId: option.principalId, guest: option.guest, restricted: option.restricted }]);
    setDraft("");
  }

  async function requestGuestInvite() {
    const request = guestRequest;
    if (!request || busy) return;
    const { principal, role: requested } = request;
    if (principal.principalType !== "user" && principal.principalType !== "email") return;
    setBusy(true);
    try {
      await api.requestGuestInvite(session, pageId, {
        principalType: principal.principalType, principalId: principal.principalId, role: requested,
      });
      setGuestRequest(null);
      setErrorCode("");
      setDraft("");
      setChips(current => current.filter(chip => chip.principalId !== principal.principalId));
      onStatus(t("topBar:guestInviteRequested", { name: principal.label }), "default");
    } catch (error: unknown) {
      setErrorCode(userErrorText(error, t("topBar:guestInviteRequestFailed")));
    } finally {
      setBusy(false);
    }
  }

  const inviting = state?.canManage !== false && Boolean(opened || draft.trim() || chips.length || message.trim());
  // Who the page isn't shared with yet. A computer never offers the person sharing; a phone's "사용자 선택"
  // leads with them unless the page is shared with them on itself, as Notion's (iPhone 402): on a page under
  // one of theirs they lead (seen 2026-11-12), on a page whose sharing is set on it — unlinked from its parent's,
  // or after inviting themselves — they are not offered (seen 2026-11-12 and 2026-11-16). A private page's owner
  // holds theirs on it at the top of their pages (Notion's private page carries its maker's own share) and on a
  // page unlinked from its parent's; under it they hold it from above.
  const holder = state?.accessOwner;
  const holdsHere = Boolean(holder?.privatePage && (pageTop || state?.shareUnlink));
  const holderOut = holder && (holder.principalId !== session.user.id || holdsHere) ? holder.principalId : "";
  const suggestions = matchingPrincipalOptions(state?.principalOptions ?? [], state?.permissions ?? [], draft,
    phone ? [holderOut] : [session.user.id, holder?.principalId ?? ""], phone ? session.user.id : "");
  const query = draft.trim().toLocaleLowerCase();
  const alreadyShared: ShareAlreadyShared[] = !query || !state ? [] : [
    { key: `owner:${state.accessOwner.principalId}`, label: state.accessOwner.label, email: state.accessOwner.email,
      roleLabel: t("topBar:fullAccess"), isYou: state.accessOwner.isCurrent, userId: state.accessOwner.principalId },
    ...state.permissions.map(permission => ({
      key: permission.id, label: permission.principalLabel, email: permission.principalEmail,
      roleLabel: sharePermissionLabel(permission.role, t),
      isYou: shareIsYou(permission, session.user),
      // An address someone joined by is that person, with their face, as the access list's row (principalUserId).
      userId: permission.principalUserId || (permission.principalType === "user" ? permission.principalId : undefined),
      guest: permission.guest, restricted: permission.restricted,
    })),
  ].filter(row => `${row.label} ${row.email}`.toLocaleLowerCase().includes(query));
  // Words that match no one, and no one here already, offer addresses, unless addresses can't be invited at all.
  const addressOptions = suggestions.length || alreadyShared.length || state?.guestsAllowed === false || state?.externalEmailAllowed === false
    ? [] : inviteAddressOptions(draft, phone ? [] : t("topBar:inviteEmailDomains").split(","));

  return {
    inviting, guestRequest: guestRequest !== null, noteRefusal, focusField,
    /** Chosen people, a message or a refused address: closing asks before dropping them. */
    unsent: Boolean(chips.length || message.trim() || invalidInvites.length),
    /** The invite's part of the share tab (TopBarShareAccessPanel). */
    panel: {
      focusRequest, inputRef, draft, onDraft: changeDraft, addressRows: addressOptions.length > 0, invalidInvites,
      chips, onRemoveChip: (chip: SelectedPrincipal) => { setOpened(true); setChips(current => current.filter(item => item !== chip)); },
      candidates: addressOptions.length ? addressOptions : suggestions, onToggle: toggleChip, alreadyShared, inviting,
      onBack: () => { setOpened(false); setDraft(""); setInvalidInvites([]); setChips([]); setMessage(""); setGuestRequest(null); focusField(); },
      onOpenInvite: () => { setOpened(true); focusField(); },
      role, onRole: setRole, message, onMessage: setMessage,
      onInvite: () => void submit(),
    },
    /** Asking an owner to add a guest only they may invite. */
    guestNote: guestRequest ? <div data-share-guest-request>
      <p className={styles.shareSecurityNote} role="alert">{t("topBar:guestInviteNeedsApproval")}</p>
      <div className={styles.copyLinks}>
        <button type="button" className={styles.copyLink} data-menu-item disabled={busy} onClick={() => void requestGuestInvite()}>
          {t("topBar:requestGuestInvite")}
        </button>
      </div>
    </div> : null,
  };
}
