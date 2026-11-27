import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LinkIcon, X, XMarkIcon } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useProductToast } from "@/lib/productToast";
import type { TeamspaceMemberRole, TeamspacePrincipalOption } from "@/teamspaceWire.generated";
import { teamspaceGlyph } from "./KeptIconGlyph";
import { TeamspaceRoleMenu } from "./TeamspaceRoleMenu";
import type { TeamspaceDialogController } from "./useTeamspaceDialog";
import { useTeamspaceInviteLink } from "./useTeamspaceInviteLink";
import styles from "./TeamspaceMembers.module.css";
import dialogStyles from "../TeamspaceDialog.module.css";
import { copyText } from "@/lib/copyText";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { ReadFailed } from "@/components/ReadFailed";

const keyOf = (item: { principalType: string; principalId: string }) => `${item.principalType}:${item.principalId}`;

/** Invite people or groups, with a skip action after creating a teamspace. */
export function TeamspaceInviteDialog({ controller, skippable = false, onClose }: {
  controller: TeamspaceDialogController; skippable?: boolean; onClose(): void;
}) {
  const { t } = useTranslation("teamspaceDialog");
  const { notify } = useProductToast();
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const titleId = useId();
  const [query, setQuery] = useState("");
  const [chosen, setChosen] = useState<TeamspacePrincipalOption[]>([]);
  const [role, setRole] = useState<TeamspaceMemberRole>("member");
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState(false);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onClose(); },
    initialFocus: inputRef,
  });
  const selected = controller.selected;
  // "초대 링크 복사", as Notion's help has it, while the teamspace's link is on.
  const inviteLink = useTeamspaceInviteLink(controller.session, controller.workspaceId, selected?.id ?? null,
    Boolean(selected && controller.canInviteSelected));
  const members = new Set(controller.members.map(keyOf));
  const chosenKeys = new Set(chosen.map(keyOf));
  const normalized = query.trim().toLocaleLowerCase();
  const candidates = useMemo(() => normalized ? controller.principalOptions.filter((option) => (
    !members.has(keyOf(option)) && !chosenKeys.has(keyOf(option)) && option.label.toLocaleLowerCase().includes(normalized)
  )).slice(0, 8) : [], [normalized, controller.principalOptions, controller.members, chosen]); // eslint-disable-line react-hooks/exhaustive-deps

  function choose(option: TeamspacePrincipalOption) {
    setChosen((current) => [...current, option]);
    setQuery("");
    setActive(0);
    inputRef.current?.focus();
  }

  async function invite() {
    if (!selected || !chosen.length || busy) return;
    setBusy(true);
    const added = await controller.addMembers(chosen, role, t("notice.memberAdded"));
    setBusy(false);
    if (!added) return;
    notify(t("inviteDialog.added", { count: added, teamspace: selected.name }), "success");
    onClose();
  }

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("inviteDialog.close")} onClick={() => { if (!busy) onClose(); }} />
      <div ref={dialogRef} className={styles.inviteDialog} role="dialog" aria-modal="true" aria-labelledby={titleId} data-teamspace-invite-dialog
        data-just-made={skippable ? "true" : undefined}>
        {skippable ? (
          <button type="button" className={`${dialogStyles.close} ${styles.inviteClose}`} aria-label={t("inviteDialog.close")} disabled={busy} onClick={onClose}>
            <X size="small" aria-hidden="true" />
          </button>
        ) : null}
        <h2 id={titleId} className={styles.inviteTitle}>
          <span className={styles.inviteLabel}>{t("inviteDialog.title")}</span>{" "}
          {selected ? <span className={styles.inviteGlyph} aria-hidden="true">{teamspaceGlyph(selected)}</span> : null}{" "}
          <span className={styles.inviteName} title={selected?.name}>{selected?.name}</span>
        </h2>
        <div className={styles.inviteBox} data-has-chips={chosen.length ? "true" : undefined}>
          <span className={styles.inviteEntries}>
          {/* A chip's × leaves the focus with no field, the next key going nowhere, as Notion's (seen 2026-11-16
              at 1440 after the press only: "q" typed after it reached nothing, the focus on BODY). */}
          <span className={styles.chips}>{chosen.map((item) => <span key={keyOf(item)} className={styles.chip} data-teamspace-invite-chip>
            <span>{item.label}</span>
            <button type="button" aria-label={t("inviteDialog.removeChip", { name: item.label })} disabled={busy} data-focus-leaves
              onClick={() => setChosen((current) => current.filter((other) => keyOf(other) !== keyOf(item)))}>
              <XMarkIcon size="small" aria-hidden="true" /></button>
          </span>)}</span>
          {/* An address field, as Notion's (type email, seen 2026-11-16): a phone's email keyboard, small letters. */}
          <input ref={inputRef} value={query} placeholder={chosen.length ? "" : t("inviteDialog.placeholder")} disabled={busy}
            inputMode="email" autoCapitalize="none" autoCorrect="off" spellCheck={false}
            role="combobox" aria-expanded={candidates.length > 0} aria-controls={`${titleId}-people`} aria-label={t("inviteDialog.placeholder")}
            onChange={(event) => { setQuery(event.target.value); setActive(0); }}
            onKeyDown={(event) => {
              if (imeComposing(event)) return;
              if (event.key === "Backspace" && !query && chosen.length) { setChosen((current) => current.slice(0, -1)); return; }
              if (event.key === "ArrowDown" && candidates.length) { event.preventDefault(); setActive((index) => (index + 1) % candidates.length); return; }
              if (event.key === "ArrowUp" && candidates.length) { event.preventDefault(); setActive((index) => (index - 1 + candidates.length) % candidates.length); return; }
              if (fieldEnter(event)) {
                event.preventDefault();
                if (candidates[active]) choose(candidates[active]);
                else void invite();
              }
            }} />
          </span>
          {/* A member who may invite brings in members only. */}
          <TeamspaceRoleMenu value={role} disabled={busy || !controller.canManageSelected} onChange={setRole} compact />
        </div>
        {normalized ? <div className={styles.inviteList} id={`${titleId}-people`} role="listbox" aria-label={t("inviteDialog.choose")}>
          <div className={styles.sectionLabel}>{t("inviteDialog.choose")}</div>
          {candidates.length ? candidates.map((option, index) => <div key={keyOf(option)} role="option" aria-selected={index === active}
            className={styles.option} data-active={index === active ? "true" : undefined} data-teamspace-invite-option
            onMouseDown={(event) => event.preventDefault()} onClick={() => choose(option)}>
            {option.principalType === "user"
              ? <PersonAvatar className={styles.avatar} userId={option.principalId} name={option.label} size={26} />
              : <span className={styles.avatar} aria-hidden="true">{option.label.trim().slice(0, 1).toUpperCase() || "?"}</span>}
            <span>{option.label}</span>
          </div>) : controller.managementRead ? <div className={styles.empty}>{t("inviteDialog.noMatch")}</div>
            // Who may be chosen comes from the members' read: its failure, not "no match", with a way to read again.
            : controller.readFailed ? <ReadFailed message={controller.readFailed} onRetry={controller.readManagementAgain} /> : null}
        </div> : null}
        <div className={styles.inviteFoot}>
          {inviteLink.address ? (
            <button type="button" className={styles.copyInviteLink} data-teamspace-copy-invite-link disabled={busy}
              onClick={() => {
                void copyText(inviteLink.address!).then((copied) => (copied
                  ? notify(t("inviteDialog.linkCopied"), "success") : notify(t("inviteDialog.linkCopyFailed"), "error")));
              }}>
              <LinkIcon size="small" aria-hidden="true" />{t("inviteDialog.copyLink")}
            </button>
          ) : null}
          {/* Just made, a teamspace with no one chosen yet offers only to skip, as Notion's. */}
          {skippable && !chosen.length
            ? <button type="button" className={styles.skip} disabled={busy} onClick={onClose}>{t("inviteDialog.skip")}</button>
            : <button type="button" className={styles.invite} disabled={!chosen.length || busy} onClick={() => void invite()}>{t("inviteDialog.invite")}</button>}
        </div>
        {controller.error && controller.error !== controller.readFailed ? <p className={styles.error} role="alert">{controller.error}</p> : null}
      </div>
    </div>,
    document.body,
  );
}
