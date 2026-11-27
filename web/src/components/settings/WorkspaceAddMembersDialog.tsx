import { withPersonDistinction } from "@/lib/peopleDirectory";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ChevronLeft, EnvelopeIcon, UserPlusIcon, XMarkIcon } from "@/icons/leafen";
import { organizationModule } from "@/lib/organizationModule";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { Session, Workspace } from "@/types";
import { workspaceMembershipBounds, type WorkspaceGuestDirectoryItem, type WorkspaceMemberCandidate } from "@/workspaceMembershipWire.generated";
import { accessRequestBounds } from "@/accessRequestWire.generated";
import { WorkspaceNameInvitations } from "./WorkspaceNameInvitations";
import styles from "./WorkspaceAddMembersDialog.module.css";
import requestStyles from "./WorkspaceMemberRequest.module.css";
import { WorkspaceAddMembersTouch } from "./WorkspaceAddMembersPhone";
import { AddMembersRoleCard, AddMembersRoleRow, type AddMembersRole } from "./WorkspaceAddMembersRole";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/u;
const SUGGESTIONS_MOST = 8;

/**
 * The addresses among what was typed or pasted — its words cut at commas, semicolons and spaces — in small letters, as
 * Notion's chips show them, and the text left without them, as typed (Notion's field keeps "Qx Z", "Qx, Z" and "Qx; Z",
 * twice 2026-11-22). With `all` false only a word a separator ends counts: the one still being typed stays whatever it
 * looks like, and the other words stay — Notion's chips an address-like word still being typed ("a@example.c") and drops
 * the rest, even a letter typed after "a@example.com " (seen 2026-11-22); Leafen keeps what was typed (principle 5).
 */
function addressesIn(text: string, all: boolean) {
  const addresses: string[] = [];
  const rest = text.replace(/([^\s,;]+)([\s,;]*)/gu, (whole: string, word: string, after: string) => {
    if ((!all && !after) || !EMAIL.test(word)) return whole;
    addresses.push(word.toLowerCase());
    return "";
  });
  return { addresses, rest };
}

type Suggestion = { key: string; label: string; email: string; userId?: string; avatar?: string | null; here: boolean;
  /** Someone with no address, added by their account (the server's member candidates). */
  person?: boolean;
  /** The typed address itself. */
  address?: boolean;
  /** Words that are no address and find no one (unfoundWords). */
  unfound?: boolean };
type PickedPerson = { userId: string; name: string };

export type WorkspaceAddMembersResult = { added: string[]; error: string };

/**
 * Settings › 사람's "멤버 추가하기", as Notion's dialog (seen 2026-10-27): the
 * field takes addresses typed or pasted apart by commas, and names find the
 * people already here ("초대됨") and the guests, whom a pick adds; the role
 * an invitation brings, chosen where there is more than one to give
 * (WorkspaceAddMembersRole), and a note the emailed invitation carries. Escape closes the
 * suggestions first, then the dialog. A member asks the owner instead, in
 * Notion's "멤버 초대 요청" (WorkspaceMemberRequest.module.css): the address and
 * why, which the owner reads with the request. Under a line, Leafen's own way
 * in: by name, without an email.
 */
export function WorkspaceAddMembersDialog({
  session, workspace, request, members, invitations, busy, loading = false, initialEmails, onAdd, onClose,
}: {
  session: Session;
  workspace: Workspace;
  /** A member asks the owner to add someone. */
  request: boolean;
  members: readonly { userId: string; displayName: string; email: string; avatar?: string | null }[];
  invitations: readonly { email: string }[];
  busy: boolean;
  /** The people already here are still on their way: typing goes on, sending waits. */
  loading?: boolean;
  /** Addresses already chosen as it opens: the guests being made members (settings › 사람 › 게스트). */
  initialEmails?: readonly string[];
  onAdd(emails: string[], message: string, people: readonly PickedPerson[], restricted: boolean): Promise<WorkspaceAddMembersResult>;
  onClose(): void;
}) {
  const { t, i18n } = useTranslation(["workspaceSettingsDialog", "common"]);
  // A touch screen's is Notion's phone and iPad "멤버 추가" wherever it opens from — settings › 사람 or the
  // workspace menu's "멤버 초대" (WorkspaceAddMembersPhone.tsx): a phone's whole screen, a wider one's window.
  const layout = useSidebarLayout(true);
  const phone = layout.phone, wide = layout.touch && !layout.phone;
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const titleId = useId(), listId = useId(), messageId = useId(), errorId = useId();
  const [view, setView] = useState<"email" | "name">("email");
  const [chosen, setChosen] = useState<string[]>(() => [...(initialEmails ?? [])]);
  // A person picked from the suggestions stands in their chip by the row's name, as Notion's ("june lee", 2026-11-22).
  const [chipNames, setChipNames] = useState<Readonly<Record<string, string>>>({});
  // People picked by name who have no address: a name invitation's account removed earlier.
  const [chosenPeople, setChosenPeople] = useState<PickedPerson[]>([]);
  const [candidates, setCandidates] = useState<WorkspaceMemberCandidate[]>([]);
  const [query, setQuery] = useState("");
  const [listOpen, setListOpen] = useState(false);
  const [active, setActive] = useState(0);
  // 멤버, or 제한된 멤버 where the organization module gives it (user decision 2026-10-30).
  const [role, setRole] = useState<AddMembersRole>("member");
  const roles: AddMembersRole[] = organizationModule()?.restrictMember ? ["member", "restricted"] : ["member"];
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  // The field a refusal here is about, marked with its line (rejected-entry focus).
  const refused = () => error === t("memberEmailInvalid") || error === t("memberEmailRequired")
    ? { "aria-invalid": true as const, "aria-describedby": errorId } : {};
  const [guests, setGuests] = useState<WorkspaceGuestDirectoryItem[]>([]);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], restoreFocus: true,
    // On a phone it is a whole screen whose field takes the keyboard at once, as Notion's phone "멤버 추가" (seen 2026-11-12).
    initialFocus: inputRef, typesOnPhone: true, onEscape: () => { if (!busy) onClose(); },
  });

  // A view the dialog turns into takes the focus in — its first field, else
  // its first control past "뒤로", else its title — and back on the first
  // view the field has it (2026-10-28: "이름으로 초대" left it on the page).
  const viewShown = useRef(view);
  useEffect(() => {
    if (viewShown.current === view) return undefined;
    viewShown.current = view;
    // What is typed from the press goes in there (focusOpenedField).
    return focusOpenedField(() => {
      const root = dialogRef.current;
      if (!root || view === "email") return root ? inputRef.current : null;
      return root.querySelector<HTMLElement>("input:not([disabled]):not([type=hidden]), textarea:not([disabled])")
        ?? [...root.querySelectorAll<HTMLElement>("button:not([disabled])")].find((button) => !button.closest(`.${styles.subhead}`))
        ?? root.querySelector<HTMLElement>(`#${CSS.escape(titleId)}`);
    });
  }, [titleId, view]);

  // The guests are found by name too, as Notion's field finds them.
  useEffect(() => {
    if (request) return undefined;
    const controller = new AbortController();
    api.listWorkspaceGuests(session, workspace.id, "", controller.signal)
      .then((directory) => setGuests(directory.guests), () => undefined);
    return () => controller.abort();
  }, [request, session, workspace.id]);

  const typed = query.trim().toLowerCase();

  // A name the owner types also finds the organization's people with no
  // address, whom no email reaches (Leafen's own: its name invitations).
  useEffect(() => {
    setCandidates([]);
    if (request || !typed || typed.includes("@") || Array.from(typed).length > workspaceMembershipBounds.memberCandidateQueryRunes) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      api.listWorkspaceMemberCandidates(session, workspace.id, typed, controller.signal)
        .then((found) => setCandidates(found.candidates), () => undefined);
    }, 200);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [request, session, typed, workspace.id]);
  const suggestions = useMemo<Suggestion[]>(() => {
    if (!typed) return [];
    const here = new Set([...members.map((member) => member.email.toLowerCase()), ...invitations.map((invitation) => invitation.email)]);
    // The one inviting is not offered, as Notion's (twice 2026-11-22: the owner's name and address found no one).
    const people: Suggestion[] = [
      ...members.filter((member) => member.userId !== session.user.id).map((member) => ({ key: `member:${member.userId}`, label: withPersonDistinction(member.userId, member.displayName || member.email), email: member.email.toLowerCase(),
        userId: member.userId, avatar: member.avatar, here: true })),
      ...invitations.map((invitation) => ({ key: `invitation:${invitation.email}`, label: invitation.email, email: invitation.email, here: true })),
      ...guests.filter((guest) => guest.email && !here.has(guest.email.toLowerCase())).map((guest) => ({
        key: `guest:${guest.userId}`, label: guest.displayName || guest.email, email: guest.email.toLowerCase(),
        userId: guest.userId, avatar: guest.avatar, here: false })),
    ];
    // Two of one name (or one like a member's) are told apart by when they joined.
    const named = (name: string) => [...members.map((member) => member.displayName), ...candidates.map((other) => other.displayName)]
      .filter((other) => other.trim().toLowerCase() === name.trim().toLowerCase()).length > 1;
    const joined = (at: string) => {
      const date = new Date(at);
      return Number.isNaN(date.getTime()) ? "" : date.toLocaleDateString(i18n.language);
    };
    const byName: Suggestion[] = candidates.filter((candidate) => !chosenPeople.some((picked) => picked.userId === candidate.userId))
      .map((candidate) => ({
        key: `person:${candidate.userId}`, email: "", userId: candidate.userId, avatar: candidate.avatar, here: false, person: true,
        label: named(candidate.displayName) && joined(candidate.joinedAt)
          ? t("addMembersJoined", { name: candidate.displayName, date: joined(candidate.joinedAt) }) : candidate.displayName,
      }));
    const found = people.filter((person) => !chosen.includes(person.email)
      && !chosenPeople.some((picked) => picked.userId === person.userId)
      && `${person.label} ${person.email}`.toLowerCase().includes(typed));
    // A whole address typed is offered as itself, "✉ 주소", picked to become a
    // chip, as Notion's (seen 2026-11-07).
    const address: Suggestion[] = EMAIL.test(typed) && !chosen.includes(typed) && !found.some((person) => person.email === typed)
      ? [{ key: `address:${typed}`, label: typed, email: typed, here: false, address: true }] : [];
    return [...address, ...found, ...byName].slice(0, SUGGESTIONS_MOST);
  }, [candidates, chosen, chosenPeople, guests, i18n.language, invitations, members, session.user.id, t, typed]);
  // Words that are no address and find no one, one rule for every screen: the window's list offers them as Notion's
  // desktop "✉ 〈typed〉" row (2026-11-22: "q" in three tabs, a name already a chip and the inviter's own in two,
  // "a@b" once), as typed, like the address row; a touch screen asks for an address over them (WorkspaceAddMembersPhone).
  const unfoundWords = typed && !EMAIL.test(typed) && !suggestions.length ? query.trim() : "";
  const rows: Suggestion[] = unfoundWords && !phone && !wide
    ? [{ key: `unfound:${unfoundWords}`, label: unfoundWords, email: "", here: false, unfound: true }] : suggestions;
  // In the window's list someone here already ("초대됨") is a row like the others, as Notion's menu item (twice
  // 2026-11-22): the first row's highlight, the arrows (round both ways), the pointer, and a pick making their chip.
  // A touch screen's rows and a member's request keep them out (Notion's not seen).
  const hereTakes = !request && !phone && !wide;
  const pickable = hereTakes ? rows : rows.filter((suggestion) => !suggestion.here);
  const shownList = listOpen && rows.length > 0;
  // "초대 보내기" waits for someone picked, as Notion's does until an address is a chip; a member's
  // "초대 요청" does not, and asks for the address when pressed without one (Notion's stands ready, seen 2026-11-21).
  const ready = request || chosen.length > 0 || chosenPeople.length > 0;

  /** The text's addresses become chips (addressesIn); what is left, as typed, is returned with the chips now chosen. */
  function take(text: string, all = true) {
    const { addresses, rest } = addressesIn(text, all);
    const fresh = addresses.filter((address, index) => !chosen.includes(address) && addresses.indexOf(address) === index);
    if (fresh.length) setChosen((current) => [...current, ...fresh]);
    return { rest, emails: [...chosen, ...fresh] };
  }

  /** A row picked: Enter leaves the caret in the field; a press, the focus with no field — the next key going
   *  nowhere, as Notion's (twice 2026-11-22, a letter typed after it). A touch screen's field keeps it. */
  function pick(suggestion: Suggestion, byKey = false) {
    if (suggestion.here && !hereTakes) return;
    // The "✉ 〈typed〉" row makes no chip, as Notion's (twice each 2026-11-22, a letter typed after): Enter leaves all
    // as it is; a press closes the list, the words staying in the field and the focus with no field.
    if (suggestion.unfound) {
      if (!byKey) { setListOpen(false); inputRef.current?.blur(); }
      return;
    }
    if ((suggestion.person || (suggestion.here && !suggestion.email)) && suggestion.userId) {
      const candidate = candidates.find((other) => other.userId === suggestion.userId);
      setChosenPeople((current) => [...current, { userId: suggestion.userId!, name: candidate?.displayName ?? suggestion.label }]);
    } else {
      setChosen((current) => [...current, suggestion.email]);
      if (!suggestion.address) setChipNames((current) => ({ ...current, [suggestion.email]: suggestion.label }));
    }
    setQuery("");
    setActive(0);
    if (byKey || phone || wide) inputRef.current?.focus();
    else inputRef.current?.blur();
  }

  async function send() {
    if (busy || loading) return;
    const { rest, emails } = take(query);
    // What became a chip leaves the field, whatever is refused next (2026-11-07: "주소 × 주소"); other words stay as typed.
    const left = /[^\s,;]/u.test(rest);
    setQuery(left ? rest : "");
    if (left) {
      setError(t("memberEmailInvalid"));
      return;
    }
    if (!emails.length && !chosenPeople.length) {
      setError(t("memberEmailRequired"));
      return;
    }
    setQuery("");
    setError("");
    const result = await onAdd(emails, message.trim(), chosenPeople, role === "restricted");
    setChosen(emails.filter((email) => !result.added.includes(email)));
    setChosenPeople((current) => current.filter((person) => !result.added.includes(`user:${person.userId}`)));
    if (result.error) setError(result.error);
    else onClose();
  }

  const head = (title: string) => (
    <div className={styles.subhead}>
      <button type="button" aria-label={t("common:actions.back")} onClick={() => setView("email")}><ChevronLeft size="regular" aria-hidden="true" /></button>
      <h2 id={titleId} className={styles.title} tabIndex={-1}>{title}</h2>
    </div>
  );

  // A chip's × leaves the focus with no field, the next key going nowhere, as Notion's "멤버 추가하기" (seen 2026-11-16
  // at 1440 after the press only: "q" typed after it reached nothing, the focus on BODY). A touch screen's field keeps it,
  // its keyboard up (Notion's phone and iPad not seen).
  const chipRemoved = () => { if (phone || wide) inputRef.current?.focus(); };

  // A press in the field brings back the list a press on a row or Escape closed, as Notion's (twice 2026-11-22); a
  // press anywhere else in the window closes it (the message box twice, the title once), a row's press keeping the
  // field's focus until it is picked. A touch screen's rows stay as they were (Notion's not seen).
  const listFollowsPresses = !phone && !wide;
  function fieldPressed(target: Element) {
    if (target.closest("button")) return;
    inputRef.current?.focus();
    if (listFollowsPresses && typed) setListOpen(true);
  }

  const fieldBox = (
    <div className={`${styles.field} ${request ? requestStyles.field : ""}`} data-add-members-field
      onClick={(event) => fieldPressed(event.target as Element)}>
      {chosenPeople.map((person) => (
        <span key={person.userId} className={styles.chip} data-add-members-chip={`user:${person.userId}`}>
          <span>{person.name}</span>
          <button type="button" aria-label={t("addMembersRemove", { email: person.name })} disabled={busy} data-focus-leaves
            onClick={() => { setChosenPeople((current) => current.filter((other) => other.userId !== person.userId)); chipRemoved(); }}>
            <XMarkIcon size="small" aria-hidden="true" />
          </button>
        </span>
      ))}
      {chosen.map((email) => (
        <span key={email} className={styles.chip} data-add-members-chip={email}>
          <span>{chipNames[email] ?? email}</span>
          <button type="button" aria-label={t("addMembersRemove", { email: chipNames[email] ?? email })} disabled={busy} data-focus-leaves
            onClick={() => { setChosen((current) => current.filter((other) => other !== email)); chipRemoved(); }}>
            <XMarkIcon size="small" aria-hidden="true" />
          </button>
        </span>
      ))}
      <input ref={inputRef} value={query} placeholder={chosen.length || chosenPeople.length ? "" : t(phone || wide ? "memberPhoneSearch" : request ? "memberRequestSearch" : "addMembersSearch")} aria-label={t("addMembersSearch")}
        // An address field, as Notion's (type email, seen 2026-11-16): a phone's email keyboard, small letters.
        inputMode="email" autoCapitalize="none" autoCorrect="off" spellCheck={false}
        autoComplete="off" disabled={busy} role="combobox" aria-expanded={shownList} aria-controls={listId} {...refused()}
        onBlur={() => { if (listFollowsPresses) setListOpen(false); }}
        onChange={(event) => {
          const value = event.target.value;
          setError("");
          setListOpen(true);
          setActive(0);
          // An address a comma, semicolon or space ends, or a pasted list's, turns into a chip; the other words stay as
          // typed, as Notion's ("Qx Z" stays "Qx Z", twice 2026-11-22).
          setQuery(take(value, false).rest);
        }}
        onKeyDown={(event) => {
          if (imeComposing(event)) return;
          if (event.key === "Escape" && shownList) { event.preventDefault(); event.stopPropagation(); setListOpen(false); return; }
          if (event.key === "Backspace" && !query && chosen.length) { setChosen((current) => current.slice(0, -1)); return; }
          if (event.key === "Backspace" && !query && chosenPeople.length) { setChosenPeople((current) => current.slice(0, -1)); return; }
          if (event.key === "ArrowDown" && shownList && pickable.length) { event.preventDefault(); setActive((index) => (index + 1) % pickable.length); return; }
          if (event.key === "ArrowUp" && shownList && pickable.length) { event.preventDefault(); setActive((index) => (index - 1 + pickable.length) % pickable.length); return; }
          if (fieldEnter(event)) {
            event.preventDefault();
            if (shownList && pickable[active]) pick(pickable[active], true);
            else if (typed && EMAIL.test(typed)) { take(query); setQuery(""); }
            else void send();
          }
        }} />
    </div>
  );
  // The suggestions float under the field, over what follows, so typing leaves the dialog still, as Notion's (seen 2026-11-12).
  const field = (
    <div className={`${styles.fieldWrap} ${request ? "" : styles.fieldInset}`}>
      {fieldBox}
      {shownList ? (
        <div className={styles.suggestions} id={listId} role="listbox" aria-label={t("addMembersSearch")}>
          {rows.map((suggestion) => (
            <div key={suggestion.key} role="option" className={styles.suggestion}
              aria-disabled={suggestion.here && !hereTakes ? true : undefined}
              aria-selected={pickable[active]?.key === suggestion.key}
              data-active={pickable[active]?.key === suggestion.key ? "true" : undefined}
              data-add-members-suggestion={suggestion.key} data-add-members-unfound={suggestion.unfound ? "" : undefined}
              // The pointer moves the highlight, which stays where it last was when the pointer leaves (Notion's).
              onPointerMove={() => { const at = pickable.indexOf(suggestion); if (at >= 0 && at !== active) setActive(at); }}
              onMouseDown={(event) => event.preventDefault()} onClick={() => pick(suggestion)}>
              {suggestion.address || suggestion.unfound ? <EnvelopeIcon size="regular" aria-hidden="true" />
                : <PersonAvatar userId={suggestion.userId ?? suggestion.email} avatar={suggestion.avatar} name={suggestion.label} size={20} />}
              <span className={styles.suggestionName}>{suggestion.label}</span>
              {suggestion.here ? <span className={styles.badge}>{t("addMembersHere")}</span> : null}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );

  // A member's request, as Notion's "멤버 초대 요청": the title, the address, why, and the note beside "초대 요청".
  const requestView = <>
    <h2 id={titleId} className={requestStyles.title}>{t("memberRequestTitle")}</h2>
    {field}
    <label className={requestStyles.reason} htmlFor={messageId}>{t("memberRequestReason")}</label>
    <span className={requestStyles.reasonBox}>
      <textarea id={messageId} className={requestStyles.reasonText} value={message}
        placeholder={t("memberRequestReasonPlaceholder")} maxLength={accessRequestBounds.messageRunes} disabled={busy}
        data-member-request-reason onChange={(event) => setMessage(event.target.value)} />
    </span>
    {error ? <p id={errorId} className={styles.error} role="alert">{error}</p> : null}
    <div className={requestStyles.foot}>
      <p className={requestStyles.note}>{t("memberRequestNote")}</p>
      <button type="button" className={requestStyles.send} disabled={busy || loading} data-add-members-send onClick={() => void send()}>
        {t("memberRequestSend")}
      </button>
    </div>
  </>;

  const memberNames = members.map((member) => member.displayName);
  const nameView = <>{head(t("nameInviteTitle"))}<WorkspaceNameInvitations session={session} workspaceId={workspace.id} memberNames={memberNames} /></>;

  // A touch screen's, as Notion's phone and iPad "멤버 추가": what is found stands in the screen's rows under the role (WorkspaceAddMembersPhone).
  if (phone || wide) return <WorkspaceAddMembersTouch wide={!phone} dialogRef={dialogRef} titleId={titleId} listId={listId} zIndex={zIndex} request={request}
    busy={busy} loading={loading} view={view} onView={setView} field={fieldBox} suggestions={listOpen ? suggestions : []}
    activeKey={pickable[active]?.key} onPick={pick} unfound={listOpen ? unfoundWords : ""}
    error={error} errorId={errorId}
    role={<AddMembersRoleRow value={role} roles={roles} wide={!phone} disabled={busy} onChange={setRole} />}
    nameInvitations={<WorkspaceNameInvitations session={session} workspaceId={workspace.id} memberNames={memberNames} phone />}
    onCancel={onClose} onSend={() => void send()} />;

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={() => { if (!busy) onClose(); }} />
      <div ref={dialogRef} className={`${styles.dialog} ${request ? requestStyles.dialog : ""}`} role="dialog" aria-modal="true"
        aria-labelledby={titleId} data-workspace-add-members data-member-request={request ? "" : undefined}>
        {view === "name" ? nameView
          : request ? requestView
          : <>
            <div className={styles.head}>
              <UserPlusIcon className={styles.icon} aria-hidden="true" />
              <h2 id={titleId} className={styles.title}>{t("addMembers")}</h2>
              <p className={styles.hint}>{t("addMembersHint")}</p>
            </div>
            {field}
            <span className={`${styles.label} ${styles.roleLabel}`}>{t("memberRoleMenuTitle")}</span>
            <AddMembersRoleCard value={role} roles={roles} disabled={busy} onChange={setRole} />
            <label className={styles.label} htmlFor={messageId}>{t("addMembersMessage")}</label>
            <span className={styles.messageBox}>
              <textarea id={messageId} className={styles.message} value={message} placeholder={t("addMembersMessagePlaceholder")}
                maxLength={workspaceMembershipBounds.invitationMessageCharacters} disabled={busy} data-add-members-message
                onChange={(event) => setMessage(event.target.value)} />
            </span>
            {error ? <p id={errorId} className={styles.error} role="alert">{error}</p> : null}
            <button type="button" className={styles.send} disabled={!ready || busy || loading} data-add-members-send onClick={() => void send()}>
              {t("addMembersSend")}
            </button>
            <button type="button" className={styles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
            <div className={styles.more}>
              <button type="button" data-add-members-by-name onClick={() => setView("name")}>{t("nameInviteTitle")}</button>
            </div>
          </>}
      </div>
    </div>,
    document.body,
  );
}
