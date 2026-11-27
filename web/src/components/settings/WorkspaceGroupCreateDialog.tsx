import { type FormEvent, useId, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { MakeOnce } from "@/lib/uncertainWrites";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { GroupPeopleIcon, X } from "@/icons/leafen";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { PageIconPicker } from "@/components/page/PageIconPicker";
import { WorkspaceGroupIcon } from "./WorkspaceGroupIcon";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import type { Session, Workspace } from "@/types";
import type { WorkspacePermissionGroup } from "@/workspaceMembershipApi";
import type { WorkspaceMemberDirectoryItem } from "@/workspaceMembershipWire.generated";
import groupStyles from "./WorkspaceGroupCreate.module.css";
import { keptIconPicture, keptIconUploadTarget } from "@/components/teamspace/KeptIconGlyph";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * "새 그룹 생성", as Notion's (seen 2026-10-12; its measures 2026-11-08; its
 * "아이콘 및 이름" 2026-11-11): a group icon over the title and a line on what
 * groups do, the group's icon — an emoji picked from the box before the name,
 * the people mark until one is — and name,
 * its members picked from the workspace's ("아직 멤버 없음" until one is), a
 * full-width "그룹 생성" that waits for a name, and "취소" under it. It opens
 * on the members field, as Notion's (seen 2026-11-11). The group made goes to
 * `onCreated`, which opens its window.
 */
export function WorkspaceGroupCreateDialog({ session, workspace, members, make, onClose, onCreated }: {
  session: Session;
  workspace: Workspace;
  members: WorkspaceMemberDirectoryItem[];
  /** Runs the make so one sent without the answer isn't made twice. */
  make: MakeOnce;
  onClose(): void;
  onCreated(group: WorkspacePermissionGroup): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const nameId = useId();
  const membersId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const membersRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [iconPicking, setIconPicking] = useState(false);
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(name, picked);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy && !iconPicking) onClose(); },
    initialFocus: membersRef,
  });
  const people = members.filter((member) => !member.app);
  const byId = new Map(people.map((member) => [member.userId, member]));
  const normalized = query.trim().toLowerCase();
  const choices = normalized
    ? people.filter((member) => !picked.includes(member.userId)
      && `${member.displayName} ${member.email}`.toLowerCase().includes(normalized)).slice(0, 8)
    : [];

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim() || busy) return;
    setBusy(true);
    setError("");
    try {
      onCreated(await make(() => api.createWorkspaceGroup(session, workspace.id, { name: name.trim(), icon, memberIds: picked })));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("groupCreateFailed")));
      setBusy(false);
    }
  }

  const dialog = (
    <div ref={overlayRef} className={groupStyles.overlay} style={{ zIndex }}>
      <button type="button" className={groupStyles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")}
        disabled={busy} onClick={() => { if (!busy) onClose(); }} />
      <section ref={dialogRef} className={`${groupStyles.dialog} ${groupStyles.createDialog}`} role="dialog" aria-modal="true" aria-labelledby={titleId}
        aria-busy={busy || undefined} tabIndex={-1} data-group-create-dialog>
        <GroupPeopleIcon size="large" aria-hidden="true" />
        <h2 id={titleId}>{t("groupCreateTitle")}</h2>
        <p className={groupStyles.dialogLead}>{t("groupCreateLead")}</p>
        <form className={groupStyles.dialogForm} onSubmit={submit}>
          <label htmlFor={nameId}>{t("groupCreateIconAndName")}</label>
          <span className={groupStyles.nameRow}>
            <span className={groupStyles.iconSlot}>
              <button type="button" className={groupStyles.nameIcon} aria-label={t("groupIconChange")} disabled={busy}
                onClick={() => setIconPicking(true)} data-group-create-icon>
                {/* Notion's box holds its two people at 24px, a picked emoji at 20 (seen 2026-11-13). */}
                <WorkspaceGroupIcon icon={icon} size={icon ? 20 : 24} />
              </button>
              {iconPicking ? (
                <ErrorBoundary scope="group-icon-picker">
                  <PageIconPicker placement="inline" offerRandom={false} current={icon} uploadTarget={keptIconUploadTarget(workspace.id, "group")}
                    onPick={(emoji) => { setIconPicking(false); setIcon(emoji); }}
                    onPickBuiltInIcon={(value) => { setIconPicking(false); setIcon(value); }}
                    onPickImage={(value) => { const picture = keptIconPicture(value); if (!picture) return; setIconPicking(false); setIcon(picture); }}
                    onRemove={() => { setIconPicking(false); setIcon(""); }}
                    onClose={() => setIconPicking(false)} />
                </ErrorBoundary>
              ) : null}
            </span>
            <input id={nameId} value={name} maxLength={200} placeholder={t("groupCreateNamePlaceholder")}
              autoComplete="off" onChange={(event) => setName(event.target.value)} />
          </span>
          <label htmlFor={membersId}>{t("groupCreateMembersLabel")}</label>
          <div className={groupStyles.memberField}>
            {picked.map((userId) => {
              const member = byId.get(userId);
              return (
                <span key={userId} className={groupStyles.chip}>
                  {member?.displayName || member?.email || userId}
                  <button type="button" aria-label={t("groupCreateRemoveMember", { name: member?.displayName || member?.email || userId })}
                    onClick={() => setPicked((current) => current.filter((id) => id !== userId))}>
                    <X size="tiny" aria-hidden="true" />
                  </button>
                </span>
              );
            })}
            <input ref={membersRef} id={membersId} value={query} autoComplete="off"
              placeholder={picked.length ? "" : t("groupCreateMembersPlaceholder")}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (fieldEnter(event) && choices[0]) {
                  event.preventDefault();
                  setPicked((current) => [...current, choices[0].userId]);
                  setQuery("");
                } else if (event.key === "Backspace" && !query && picked.length) {
                  setPicked((current) => current.slice(0, -1));
                }
              }} />
          </div>
          {choices.length ? (
            <ul className={groupStyles.choices} role="listbox" aria-label={t("groupCreateMembersLabel")}>
              {choices.map((member) => (
                <li key={member.userId} role="option" aria-selected="false">
                  <button type="button" onClick={() => {
                    // Picked by pointer, the focus rests on no control, as
                    // Notion's (seen 2026-11-15): not on the "그룹 생성"
                    // that stands where the list was, which Enter would press.
                    dialogRef.current?.focus({ preventScroll: true });
                    setPicked((current) => [...current, member.userId]); setQuery("");
                  }}>
                    <PersonAvatar userId={member.userId} name={member.displayName} size={20} />
                    <span>{member.displayName}</span>
                    <span className={groupStyles.choiceEmail}>{member.email}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : null}
          {error ? <p className={groupStyles.error} role="alert" {...writeUncertainMark(error)}>{error}</p> : null}
          <button type="submit" className={groupStyles.create} disabled={!name.trim() || busy}>{t("groupCreate")}</button>
          <button type="button" className={groupStyles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
        </form>
      </section>
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
