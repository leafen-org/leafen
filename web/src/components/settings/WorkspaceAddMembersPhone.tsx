import type { CSSProperties, ReactNode, RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ChevronLeft, ChevronRight, EnvelopeLetterIcon } from "@/icons/leafen";
import styles from "./WorkspaceAddMembersDialog.module.css";
import phoneCss from "./WorkspaceAddMembersPhone.module.css";

/** One person or address the field found (WorkspaceAddMembersDialog). */
export type AddMembersTouchSuggestion = {
  key: string; label: string; email: string; userId?: string; avatar?: string | null; here: boolean; address?: boolean;
};

/**
 * "멤버 추가" on a touch screen, as Notion's phone (seen 2026-11-15 and
 * 2026-11-16 at 402, iPhone Safari): a whole screen with "취소" and "초대" in
 * its head, the search, the role an invitation brings under "사용 권한 수준",
 * then what the search found under "사용자 선택" in the screen's own rows —
 * an address typed by a ring with its first letter, a person by their face —
 * pushing what follows down, no list floating over it (seen 2026-11-16 07:29,
 * "a@b.co") — someone here already with Notion's grey "초대됨" tag, words
 * that are no address and find no one asking for one over a "✉" row of what
 * was typed ("z", "jun", seen 2026-11-16); and Leafen's "이름으로 초대" under that, opening a screen of its
 * own with the same head — a dark "‹" back at its start and its name in the
 * middle — and the same 16 of side room (WorkspaceNameInvitations). On a
 * touch screen wider than a phone, as Notion's iPad (seen 2026-11-16 at 834,
 * the settings open): the same, in the window in the screen's middle
 * (PhoneSheetWindow) over the settings' shade, its rows the window's 28px
 * ones (WorkspaceAddMembersPhone.module.css). Notion's carries no note with
 * the invitation.
 */
export function WorkspaceAddMembersTouch({
  wide, dialogRef, titleId, listId, zIndex, request, busy, loading, view, onView, field, suggestions, activeKey, onPick, unfound,
  error, errorId, nameInvitations, role, onCancel, onSend,
}: {
  /** A touch screen wider than a phone: the window in the screen's middle. */
  wide: boolean;
  dialogRef: RefObject<HTMLDivElement | null>;
  titleId: string;
  listId: string;
  zIndex: CSSProperties["zIndex"];
  request: boolean;
  busy: boolean;
  loading: boolean;
  view: "email" | "name";
  onView(view: "email" | "name"): void;
  /** The search with its chips. */
  field: ReactNode;
  /** What the search found, shown while it is typed. */
  suggestions: readonly AddMembersTouchSuggestion[];
  activeKey?: string;
  onPick(suggestion: AddMembersTouchSuggestion): void;
  /** Words typed that are no address and find no one, asked to go on to an address: the rule the window's
   *  list shows as its "✉ 〈typed〉" row (WorkspaceAddMembersDialog's unfoundWords). */
  unfound: string;
  error: string;
  errorId: string;
  /** "이름으로 초대"'s body, under its head. */
  nameInvitations: ReactNode;
  /** "사용 권한 수준"'s row: the role the invitation brings (WorkspaceAddMembersRole). */
  role: ReactNode;
  onCancel(): void;
  onSend(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common", "topBar"]);
  // The window in a wide touch screen's middle, or the phone's whole screen: one look or the other.
  const frame = wide ? phoneCss.window : `${styles.dialog} ${phoneCss.screen}`;
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      {wide ? <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")}
        onClick={() => { if (!busy) onCancel(); }} /> : null}
      <div ref={dialogRef} className={frame} role="dialog" aria-modal="true"
        aria-labelledby={titleId} data-workspace-add-members data-phone-add-members data-touch-window={wide ? "true" : undefined}>
        {view === "name" ? <>
          <div className={phoneCss.head}>
            <button type="button" className={`${phoneCss.headButton} ${phoneCss.headBack}`} aria-label={t("common:actions.back")}
              onClick={() => onView("email")}><ChevronLeft size="regular" aria-hidden="true" /></button>
            <h2 id={titleId} className={phoneCss.title} tabIndex={-1}>{t("nameInviteTitle")}</h2>
          </div>
          {nameInvitations}
        </> : <>
          <div className={phoneCss.head}>
            <button type="button" className={phoneCss.headButton} disabled={busy} onClick={onCancel}>{t("common:actions.cancel")}</button>
            <h2 id={titleId} className={phoneCss.title}>{t(request ? "memberRequestTitle" : "memberPhoneAdd")}</h2>
            <button type="button" className={`${phoneCss.headButton} ${phoneCss.headEnd}`} disabled={busy || loading} data-add-members-send
              onClick={onSend}>{t(request ? "memberPhoneRequestSend" : "memberPhoneInvite")}</button>
          </div>
          <div className={phoneCss.search}>{field}</div>
          {request ? null : <div className={phoneCss.part}>
            <span className={phoneCss.label}>{t("memberPhoneLevel")}</span>
            {role}
          </div>}
          {/* Words that find no one and are no address yet ask for one over what was typed, as Notion's phone ("z", seen 2026-11-16 at 402). */}
          {unfound ? <div className={phoneCss.part} id={listId} role="listbox" aria-label={t("topBar:inviteTypeEmail")}>
            <span className={phoneCss.label} aria-hidden="true">{t("topBar:inviteTypeEmail")}</span>
            <div role="option" aria-disabled="true" aria-selected={false} className={`${phoneCss.row} ${phoneCss.person}`} data-add-members-unfound>
              <EnvelopeLetterIcon size="regular" aria-hidden="true" />
              <span className={phoneCss.personName}>{unfound}</span>
            </div>
          </div> : null}
          {suggestions.length ? <div className={phoneCss.part} id={listId} role="listbox" aria-label={t("topBar:invitePick")}>
            <span className={phoneCss.label} aria-hidden="true">{t("topBar:invitePick")}</span>
            {suggestions.map((suggestion) => (
              <div key={suggestion.key} role="option" className={`${phoneCss.row} ${phoneCss.person}`} aria-disabled={suggestion.here || undefined}
                aria-selected={!suggestion.here && activeKey === suggestion.key} data-add-members-suggestion={suggestion.key}
                onMouseDown={(event) => event.preventDefault()} onClick={() => onPick(suggestion)}>
                {suggestion.address ? <span className={phoneCss.letter} aria-hidden="true">{suggestion.label.slice(0, 1).toUpperCase()}</span>
                  : <PersonAvatar userId={suggestion.userId ?? suggestion.email} avatar={suggestion.avatar} name={suggestion.label} size={wide ? 20 : 28} />}
                <span className={phoneCss.personName}>{suggestion.label}</span>
                {suggestion.here ? <span className={phoneCss.personHere}>{t("addMembersHere")}</span> : null}
              </div>
            ))}
          </div> : null}
          {error ? <p id={errorId} className={`${styles.error} ${phoneCss.error}`} role="alert">{error}</p> : null}
          {request ? null : <div className={phoneCss.part}>
            <button type="button" className={phoneCss.row} data-add-members-by-name onClick={() => onView("name")}>
              {t("nameInviteTitle")}<ChevronRight size="page" aria-hidden="true" />
            </button>
          </div>}
        </>}
      </div>
    </div>,
    document.body,
  );
}
