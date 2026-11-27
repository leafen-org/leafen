import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { FocusScope, useDialog } from "react-aria";
import { api } from "@/api";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { i18next } from "@/i18n";
import { ArrowLeft } from "@/icons/leafen";
import { newId } from "@/lib/ids";
import { listNames } from "@/lib/listNames";
import { placeAround, popoverPlacementStyle, popoverRectAt, usePopoverPlacement, type PopoverRect, type PopoverSize,
  type PopoverViewport } from "@/lib/popoverPlacement";
import { appToastStack, useOptionalProductToast } from "@/lib/productToast";
import { inlineSign } from "@/lib/readingDirection";
import { publicMutationService } from "@/lib/publicMutationService";
import { userErrorText } from "@/lib/settingsErrors";
import type { PublicMutationRole } from "@/publicMutationWire.generated";
import { workspaceMemberRoles } from "@/workspaceMembershipWire.generated";
import type { Session } from "@/types";
import { useProductPagesSelect } from "../product/productPagesStore";
import { InviteChip } from "./TopBarShareInviteField";
import { inviteStartRole, TopBarSharePermissionPicker } from "./TopBarSharePermissionPicker";
import { finishInviteDraft, isInviteAddress, principalForInvite, type SelectedPrincipal } from "./topBarShareModel";
import access from "./TopBarShareAccess.module.css";
import css from "./InviteAndMentionPanel.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

type Target = "member" | "guest";

// How far before its "@" the "@" list starts (MentionMenu's mention mode).
const MENTION_LIST_INSET = 22;

/**
 * By an "@" typed in a line it stands where the "@" list stood, as Notion's
 * (seen 2026-11-16 in a 723 window, 332 high: 12 from the "@", its start
 * with the list's): under the line where the room under it, down to the
 * window's foot, holds its own height and that gap, else over it where the
 * room over it does, else under it — the "@" list's rule (popoverPlacement
 * `lineSide`), by the panel's height. Notion's "@" at 350.5..369.5 had it
 * under at 382..714, one at 390.5..409.5 over at 47..379. Grown past the
 * room on its side (an address made a chip), it is never cut: it slides
 * into the window whole (`whole`), as Notion's grew to 354 under that "@"
 * at 369.5..723.5 (seen 2026-11-16 in two tabs).
 */
function placeByLine(at: PopoverRect, size: PopoverSize, viewport: PopoverViewport, panel: Element | null) {
  return placeAround(popoverRectAt({ top: at.top, bottom: at.bottom, left: at.left - MENTION_LIST_INSET * inlineSign(panel) }),
    size, viewport, { gap: 12, lineSide: size.height, marginY: 0, whole: true });
}

/**
 * What its caller says once the panel's invite went, as Notion's snackbar
 * "〈주소〉님을 초대했습니다." / "Successfully invited 〈…〉", several named in
 * its list form (listNames).
 */
export function inviteMentionDoneText(emails: readonly string[]) {
  return i18next.t("topBar:inviteMention.invited", { address: listNames(emails) });
}

/**
 * Notion's "초대와 멘션" (seen 2026-11-12, 1440 dark): a person picker's
 * "사용자 초대 ›" and a text's "@" invite open it by the row or the "@", the
 * picker staying open under it. Its head is "←" back to the picker and its
 * title; then the addresses as chips with the role (an invite's start,
 * inviteStartRole — Notion's "전체 허용", Leafen's read) at their right, a message, and where they go — "워크스페이스에 추가 · 멤버로
 * 초대" (Leafen bills no one), chosen as it opens for the owner, or
 * "페이지에만 추가 · 게스트로 초대", chosen for anyone else — and "초대"
 * across the bottom. A restricted member can't add or ask for
 * members, so only the page is theirs to offer. Inviting shares the page with
 * each address at the role (as the share window does), a member being added
 * to the workspace first, or asked for where only the owner adds; then the
 * caller mentions whoever now has an account (onInvited). Where the caller
 * takes only people with accounts (`admit`), an address it doesn't take is
 * refused before anything is sent, with a notice that the block allows them
 * no access and "확인", as Notion's to-do assignee invite (seen 2026-11-12).
 */
export function InviteAndMentionPanel({ session, pageId, anchor, byLine = false, invite = "", admit, onBack, onClose, onInvited }: {
  session: Session;
  /** The page shared with them: the picker's page, or the page an "@" is typed on. */
  pageId: string;
  /** What it opens by: the invite row, or the "@" typed. */
  anchor: () => PopoverRect | null;
  /** Opened by an "@" typed in a line: it takes the "@" list's place (placeByLine). */
  byLine?: boolean;
  /** What was typed before: after an "@", text in the field (byLine); an address by a picker's row, a chip. */
  invite?: string;
  /** Whether the caller takes these addresses; none takes any. */
  admit?(emails: string[], signal: AbortSignal): Promise<boolean>;
  /** "←": back to what opened it; Escape too. */
  onBack(): void;
  /** Done: the invite went. */
  onClose(): void;
  onInvited?(emails: string[]): void;
}) {
  const { t } = useTranslation("topBar");
  const ref = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null);
  // What an "@" was followed by waits in the field as text, as Notion's (seen
  // 2026-11-16 in two tabs, Chrome 1440: the panel 332 high, its field one
  // line); it turns into a chip at a comma, as an address typed there does (a
  // space, Tab or leaving the field kept it text, seen once). An address by a
  // person picker's '"〈주소〉" 초대' comes as a chip over an empty line (Notion's
  // 354 high, seen in two tabs).
  const chipped = !byLine && isInviteAddress(invite);
  const [chips, setChips] = useState<SelectedPrincipal[]>(() => chipped ? [principalForInvite(invite, null)] : []);
  const [draft, setDraft] = useState(chipped ? "" : invite.trim());
  const [role, setRole] = useState<PublicMutationRole>(inviteStartRole);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useOptionalProductToast() ?? appToastStack();
  // The reader's workspace, where a member is added.
  const workspace = useProductPagesSelect((pages) => pages?.workspaces?.find((candidate) => candidate.id === pages.workspaceId) ?? null);
  const workspaceId = workspace?.id ?? "";
  // An owner adds members, a member asks the owner; a restricted member does neither.
  const memberAdds = Boolean(workspace) && !workspace?.restricted && workspace?.guest !== true;
  // It starts at "워크스페이스에 추가" for the owner, who adds them, and at
  // "페이지에만 추가" for anyone else, a member who may ask for them too, as
  // Notion's, a guest's "워크스페이스에 추가" dimmed (seen 2026-11-24 in Chrome,
  // the person picker's and the "@" menu's windows alike).
  const [target, setTarget] = useState<Target>(memberAdds && workspace?.role !== workspaceMemberRoles.member ? "member" : "guest");
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);

  const placement = usePopoverPlacement(ref, (size, viewport) => {
    const at = anchor() ?? { top: 0, bottom: 0, left: 0, right: 0 };
    // By a picker's row it stands under the row, else over it, and is never
    // cut: with room on neither side it slides up into the window whole, over
    // the row — Notion's (seen 2026-11-16: a row at 596.9..624.9 had its 354
    // at 242.9..596.9 over it, in two tabs; one at 354.9..382.9 at
    // 356.9..710.9, once).
    return byLine ? placeByLine(at, size, viewport, ref.current) : placeAround(at, size, viewport, { gap: 0, whole: true });
  }, [chips.length], undefined, { kind: "picker", onEscape: onBack, onOutsidePress: onBack });
  const { dialogProps } = useDialog({ "aria-label": t("inviteMention.title") }, ref);
  // The address field takes the keys once the panel shows where it goes (hidden till then, it can't).
  const placed = placement !== null, focused = useRef(false);
  useEffect(() => {
    if (!placed || focused.current) return;
    focused.current = true;
    inputRef.current?.focus();
  }, [placed]);

  function takeDraft(next: string) {
    if (!next.includes(",")) { setDraft(next); return; }
    const { typing, addresses } = finishInviteDraft(next);
    setChips(current => [...current, ...addresses.filter(address => !current.some(chip => chip.principalId === address))
      .map(address => principalForInvite(address, null))]);
    setDraft(typing);
  }

  async function submit() {
    const typed = draft.trim();
    const emails = [...chips.map(chip => chip.principalId), ...(isInviteAddress(typed) ? [typed] : [])];
    if (!emails.length || busy) { inputRef.current?.focus(); return; }
    controller.current?.abort();
    const signal = (controller.current = new AbortController()).signal;
    setBusy(true);
    try {
      if (admit && !(await admit(emails, signal))) {
        if (signal.aborted) return;
        // Answered, the notice gives the focus back where it was as it asked:
        // the field after Enter in it, "초대" after a press of it, each locked
        // while the addresses were checked (confirmAction's opener).
        await confirmAction({ message: t("inviteMention.blockRefused"), confirmLabel: t("inviteMention.blockRefusedOk"), cancelLabel: null });
        return;
      }
      if (target === "member") {
        for (const email of emails) {
          // An owner adds (a workspace's role names a member only, as the "멤버 초대" window reads it).
          if (workspace && workspace.role !== workspaceMemberRoles.member) {
            await api.addWorkspaceMemberByEmail(session, workspaceId, { email, role: workspaceMemberRoles.member, ...(message.trim() ? { message } : {}) }, signal);
          } else await api.requestMemberAdd(session, workspaceId, email, signal);
        }
      }
      let { state } = await publicMutationService.state(session, pageId, signal);
      for (const email of emails) {
        ({ state } = await publicMutationService.mutate(session, pageId, {
          requestId: newId(), expectedRevision: state.revision, action: "grant_permission", principalType: "email", principalId: email, role,
          // A member's invitation carries the message already.
          inviteMessage: target === "guest" ? message : "", notifyOnAccess: false,
        }, signal));
      }
      if (signal.aborted) return;
      onInvited?.(emails);
      onClose();
    } catch (failure) {
      if (signal.aborted) return;
      // A failed invite is said as Notion's panel says it (seen 2026-11-16,
      // three failed invites): the plain snackbar "〈주소〉님을 초대하는 데
      // 실패했습니다." naming the addresses, or the reason where one is known;
      // the panel stays open with its addresses taken away, the message and
      // where they go kept, to try again.
      toast?.notify(userErrorText(failure, t("inviteMention.failed", { users: listNames(emails) })));
      setChips([]);
      setDraft("");
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  }

  // Notion names them in its list form ("a외 2개 사용자의 메시지 추가", seen 2026-11-16).
  const named = listNames(chips.map(chip => chip.label));
  const card = (value: Target, title: string, line: string) => (
    <button type="button" className={css.card} aria-pressed={target === value} disabled={busy || (value === "member" && !memberAdds)}
      title={value === "member" && !memberAdds ? t("inviteMention.noMemberAdds") : undefined}
      onClick={() => setTarget(value)}>
      <span className={css.cardTitle}>{title}</span><span className={css.cardLine}>{line}</span>
    </button>
  );
  return createPortal(
    <div {...dialogProps} ref={ref} className={css.panel} data-invite-mention
      style={{ ...popoverPlacementStyle(placement), position: "fixed", visibility: placement ? "visible" : "hidden" }}
      onKeyDown={event => {
        // Escape goes back to the picker, which stays open under it, as Notion's;
        // in the role menu (its own portal) it only keeps from reaching the picker.
        if (event.key !== "Escape") return;
        event.stopPropagation();
        if (!event.defaultPrevented && ref.current?.contains(event.target as Node)) { event.preventDefault(); onBack(); }
      }}>
      {/* A scope of its own inside the picker's, so the keys come here while it is open. */}
      <FocusScope contain>
      <div className={css.head}>
        <button type="button" className={css.back} aria-label={t("inviteMention.back")} onClick={onBack}>
          <ArrowLeft size="small" aria-hidden="true" />
        </button>
        <span className={css.title}>{t("inviteMention.title")}</span>
      </div>
      <hr className={css.line} />
      <div className={css.field}>
        <div className={`${access.inviteBox} ${css.box}`} data-has-chips={chips.length ? "true" : undefined}>
          <span className={access.inviteEntries}>
            <span className={access.chips}>{chips.map(chip => <InviteChip key={chip.principalId} chip={chip} disabled={busy}
              onRemove={() => { setChips(current => current.filter(other => other !== chip)); inputRef.current?.focus(); }} />)}</span>
            <input ref={inputRef} value={draft} placeholder={chips.length ? "" : t("inviteMention.emails")} disabled={busy}
              aria-label={t("inviteMention.emails")} onChange={event => takeDraft(event.currentTarget.value)}
              onKeyDown={event => {
                if (imeComposing(event)) return;
                if (event.key === "Backspace" && !draft && chips.length) { event.preventDefault(); setChips(current => current.slice(0, -1)); }
                else if (fieldEnter(event)) { event.preventDefault(); void submit(); }
              }} />
          </span>
          <span className={access.inviteRole}>
            <TopBarSharePermissionPicker invite layout="mention" disabled={busy} value={role} onChange={next => { if (next !== "none") setRole(next); }} />
          </span>
        </div>
      </div>
      <div className={css.message}>
        <textarea value={message} rows={3} maxLength={2000} disabled={busy} aria-label={t("inviteMessage")}
          placeholder={named ? t("inviteMention.messageFor", { users: named }) : t("inviteMention.message")}
          onChange={event => setMessage(event.currentTarget.value)} />
      </div>
      <div className={css.cards}>
        {card("member", t("inviteMention.addToWorkspace"), t("inviteMention.asMember"))}
        {card("guest", t("inviteMention.addToPage"), t("inviteMention.asGuest"))}
      </div>
      <div className={css.foot}>
        <button type="button" className={css.invite} disabled={busy} onClick={() => void submit()}>{t("inviteMention.invite")}</button>
      </div>
      </FocusScope>
    </div>, document.body);
}
