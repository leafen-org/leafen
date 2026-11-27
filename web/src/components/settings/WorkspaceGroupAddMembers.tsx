import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { UserCirclePlusIcon, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { WorkspaceMemberDirectoryItem } from "@/workspaceMembershipWire.generated";
import styles from "./WorkspaceGroupDetail.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A group window's "멤버 추가", as Notion's (seen 2026-11-09): over the
 * window, "〈그룹〉에 멤버 추가" and a line on what to do, a search of the
 * workspace's members whose picks gather in the field, then "취소하기" and
 * "멤버 추가하기", which waits for a pick.
 */
export function WorkspaceGroupAddMembers({ groupName, candidates, busy, lead, onClose, onAdd }: {
  groupName: string;
  /** The line under the title: whom it finds, a workspace's members unless said. */
  lead?: string;
  /** The people it finds: a workspace's members, or an organization's for its groups. */
  candidates: Pick<WorkspaceMemberDirectoryItem, "userId" | "displayName" | "email">[];
  busy: boolean;
  onClose(): void;
  onAdd(userIds: string[]): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, onEscape: () => { if (!busy) onClose(); } });
  const wanted = query.trim().toLowerCase();
  const choices = wanted
    ? candidates.filter((member) => !picked.includes(member.userId)
      && `${member.displayName} ${member.email}`.toLowerCase().includes(wanted)).slice(0, 8)
    : [];
  const pick = (userId: string) => { setPicked((current) => [...current, userId]); setQuery(""); };
  // A result picked by pointer leaves the focus on no control, as Notion's
  // group picker (seen 2026-11-15 in "새 그룹 생성": the next letter goes
  // nowhere and no ring is drawn) — not on what stands where the list was,
  // "취소하기", which the next Enter pressed.
  const pickByPointer = (userId: string) => { dialogRef.current?.focus({ preventScroll: true }); pick(userId); };
  const byId = new Map(candidates.map((member) => [member.userId, member]));

  return (
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.addWindow} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} data-group-add-members>
        <span className={styles.addMark} aria-hidden="true"><UserCirclePlusIcon size="large" /></span>
        <h2 id={titleId}>{t("groupAddTitle", { name: groupName })}</h2>
        <p>{lead ?? t("groupAddLead")}</p>
        <div className={styles.addField}>
          {picked.map((userId) => {
            const label = byId.get(userId)?.displayName || byId.get(userId)?.email || userId;
            return (
              <span key={userId} className={styles.chip}>{label}
                <button type="button" aria-label={t("groupCreateRemoveMember", { name: label })}
                  onClick={() => setPicked((current) => current.filter((id) => id !== userId))}><X size="tiny" aria-hidden="true" /></button>
              </span>
            );
          })}
          <input value={query} autoFocus={fieldFocusOnOpen()} autoComplete="off" placeholder={picked.length ? "" : t("groupAddSearch")} aria-label={t("groupAddSearch")}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (fieldEnter(event) && choices[0]) { event.preventDefault(); pick(choices[0].userId); }
              if (event.key === "Backspace" && !query && picked.length) setPicked((current) => current.slice(0, -1));
            }} />
        </div>
        {choices.length ? (
          <ul className={styles.choices} role="listbox" aria-label={t("groupAddSearch")}>
            {choices.map((member) => (
              <li key={member.userId} role="option" aria-selected="false">
                <button type="button" onClick={() => pickByPointer(member.userId)}>
                  <PersonAvatar userId={member.userId} name={member.displayName} size={20} />
                  <span>{member.displayName}</span>
                  <span className={styles.choiceEmail}>{member.email}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
        <div className={styles.addActions}>
          <button type="button" className={styles.quiet} disabled={busy} onClick={onClose}>{t("groupAddCancel")}</button>
          <button type="button" className={styles.primary} disabled={busy || !picked.length} onClick={() => onAdd(picked)}>{t("groupAddSubmit")}</button>
        </div>
      </section>
    </div>
  );
}
