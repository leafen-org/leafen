import { useEffect, useState, type FormEvent, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "@/components/page/AnchoredEmojiPicker";
import { ICON_FIRST_TABS } from "@/components/page/pageIconPickerModel";
import { SharePeopleIcon, TeamspaceIcon } from "@/icons/leafen";
import { TeamspaceKindIcon } from "./TeamspaceSecurityIcon";
import { writeUncertainMark } from "@/lib/settingsErrors";
import { useMadeWithoutAnswer } from "@/lib/uncertainWrites";
import { securityKinds, type TeamspaceSecurity } from "./teamspaceDialogModel";
import type { TeamspaceMemberRole } from "@/teamspaceWire.generated";
import { TeamspaceChoiceMenu, type TeamspaceChoice } from "./TeamspaceChoiceMenu";
import type { TeamspaceDialogController } from "./useTeamspaceDialog";
import styles from "./TeamspaceSettings.module.css";
import { KeptIconGlyph, keptIconPicture, keptIconUploadTarget } from "./KeptIconGlyph";

/** A group a teamspace is made from: it starts with the group's name and mark, the group its member. */
export type TeamspaceFromGroup = { id: string; name: string; icon?: string };

/**
 * "팀스페이스 만들기", as Notion's (seen 2026-10-06): its icon and name, a
 * description and the kind of teamspace, then the button, dim until it has a
 * name. Made from a group, as Notion's "그룹 정보로 팀스페이스 생성" (its code,
 * 2026-11-11; its window measured 2026-11-11), the window is 520 wide, the
 * kind reads "워크스페이스 사용 권한" and starts 참가 제한, a "그룹 역할" (멤버
 * or 소유자, by the people mark) follows it, the fields 20px apart, and a
 * full-width "팀스페이스 생성하기" stands over "취소" in words.
 */

export function TeamspaceCreatePanel({ controller, titleId, closeButton, workspaceName, nameRef, iconRef, fromGroup, onCancel, onCreated }: {
  controller: TeamspaceDialogController; titleId: string; closeButton: ReactNode; workspaceName: string;
  nameRef: RefObject<HTMLInputElement | null>; iconRef: RefObject<HTMLButtonElement | null>;
  fromGroup?: TeamspaceFromGroup; onCancel?(): void;
  /** The teamspace just made, as the window that opened it may take it as a place. */
  onCreated(made: { id: string; name: string; icon: string }): void;
}) {
  const { t } = useTranslation(["teamspaceDialog", "common"]);
  const [iconPicking, setIconPicking] = useState(false);
  // Its picker opens on "아이콘", its middle under the icon's, 4px down, over the window rather than in it, as
  // Notion's new teamspace (seen 2026-11-16 at 1440: picker 312,313 under the 32px icon at 500,277).
  const iconAnchor = usePageIconPickerAnchor(iconPicking, iconRef);
  const [security, setSecurity] = useState<TeamspaceSecurity>(fromGroup ? "closed" : "open");
  const [groupRole, setGroupRole] = useState<TeamspaceMemberRole>("member");
  const roleChoices: TeamspaceChoice<TeamspaceMemberRole>[] = (["member", "owner"] as const).map((role) => ({
    value: role, label: t(`createPanel.fromGroup.${role}`), description: t(`createPanel.fromGroup.${role}Description`),
  }));
  // The group joins the teamspace just made, then it is made the default one
  // if that was asked, each with the revision the step before left.
  async function finish(made: { id: string; revision: number }) {
    let revision = made.revision;
    if (fromGroup) {
      const added = await controller.addGroupToCreated(made, fromGroup.id, groupRole, t("notice.created"));
      if (added) revision = added.revision;
    }
    if (security === "default") await controller.setDefaultTeamspace({ id: made.id, revision }, t("notice.defaultSet"));
  }
  // The fields stay open while the teamspaces load, so the name takes focus at once.
  const { actionBusy, busy, workspaceAdmin } = controller;
  // "기본" only for the workspace's owners, as Notion's menu (its code, 2026-11-11).
  const choices: TeamspaceChoice<TeamspaceSecurity>[] = securityKinds(workspaceAdmin, false).map((kind) => ({
    value: kind, icon: <TeamspaceKindIcon security={kind} size="regular" />, label: t(`security.${kind}`), description: t(`security.${kind}Description`, { workspace: workspaceName }),
  }));
  const current = choices.find((choice) => choice.value === security)!;

  // A teamspace made without the server's answer and found once the
  // teamspaces are read again goes on as the answer would have
  // (lib/uncertainWrites.ts).
  const makeOnce = useMadeWithoutAnswer({
    items: controller.teamspaces,
    idOf: (teamspace) => teamspace.id,
    readAgain: () => void controller.refreshSnapshot(),
    landed: (made) => void (async () => {
      controller.landedWithoutAnswer(t("notice.created"));
      await finish(made);
      controller.setSelectedId(made.id);
      onCreated({ id: made.id, name: made.name, icon: made.icon });
    })(),
  });

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!controller.name.trim() || busy) return;
    const result = await controller.createTeamspace(t("notice.created"), makeOnce);
    if (!result || !result.teamspace) return;
    await finish({ id: result.teamspace.id, revision: result.revision });
    onCreated({ id: result.teamspace.id, name: result.teamspace.name, icon: result.teamspace.icon });
  }

  // The window takes its first focus on its icon, with nothing lit, as
  // Notion's (seen 2026-11-12; measured 2026-11-16 in Chrome at 1440 with no
  // script before the press: "새 팀스페이스" pressed, no ring, "z" typed went
  // nowhere — the name stayed empty — and only then the icon was ringed,
  // Leafen the same). The icon stays focusable while the teamspaces
  // load and only its picker waits for them, so that first focus never falls
  // to the name field instead (2026-11-11: it did, with the field's ring).
  // Enter opens its picker, a space doesn't, as Notion's icon box (seen
  // 2026-11-19 in Chrome at 1440, keys pressed as the window opened, nothing
  // run in the page before: "t", space, "a" went nowhere, Enter opened it):
  // a name typed as the window opened opened the picker at its space, and its
  // next letters went into the picker's search.
  const noSpacePress = (event: KeyboardEvent<HTMLButtonElement>) => { if (event.key === " ") event.preventDefault(); };
  useEffect(() => {
    const icon = iconRef.current, dialog = icon?.closest('[role="dialog"]');
    if (dialog && !dialog.contains(document.activeElement)) icon?.focus({ preventScroll: true });
  }, [iconRef]);

  return (
    <form className={`${styles.create}${fromGroup ? ` ${styles.createFromGroup}` : ""}`} onSubmit={(event) => void create(event)} data-teamspace-create>
      <span className={styles.createMark} aria-hidden="true"><TeamspaceIcon /></span>
      <h2 id={titleId}>{t("createPanel.title")}</h2>
      <p className={styles.createLead}>{t("createPanel.subtitle")}</p>
      <div className={styles.field}>
        <span>{t("createPanel.iconAndName")}</span>
        <div className={styles.createName}>
          <span className={styles.iconSlot}>
            <button ref={iconRef} type="button" className={styles.createIcon} aria-label={t("head.changeIcon")} aria-disabled={busy || undefined}
              onClick={() => { if (!busy) setIconPicking(true); }} onKeyDown={noSpacePress} onKeyUp={noSpacePress}>
              <KeptIconGlyph icon={controller.icon} fallback="T" />
            </button>
            {iconPicking && iconAnchor ? (
              <ErrorBoundary scope="teamspace-icon-picker">
                <AnchoredIconPicker anchor={iconAnchor} marker="teamspace-icon-picker" onClose={() => setIconPicking(false)} picker={{
                  tabOrder: ICON_FIRST_TABS, offerRandom: false, uploadTarget: keptIconUploadTarget(controller.workspaceId, "teamspace"),
                  onPick: (icon) => { setIconPicking(false); controller.setIcon(icon); },
                  onPickBuiltInIcon: (icon) => { setIconPicking(false); controller.setIcon(icon); },
                  onPickImage: (value) => { const icon = keptIconPicture(value); if (!icon) return; setIconPicking(false); controller.setIcon(icon); },
                  onRemove: () => { setIconPicking(false); controller.setIcon(""); },
                }} />
              </ErrorBoundary>
            ) : null}
          </span>
          <input ref={nameRef} value={controller.name} maxLength={80} placeholder={t("createPanel.namePlaceholder")} aria-label={t("fields.name")}
            disabled={actionBusy} onChange={(event) => controller.setName(event.target.value)} data-teamspace-create-name />
        </div>
      </div>
      <label className={styles.field}>
        <span>{t("fields.description")}</span>
        <textarea value={controller.description} rows={3} maxLength={500} placeholder={t("createPanel.descriptionPlaceholder")} disabled={actionBusy}
          onChange={(event) => controller.setDescription(event.target.value)} />
      </label>
      <div className={styles.field}>
        <span>{t(fromGroup ? "createPanel.fromGroup.access" : "createPanel.security")}</span>
        <TeamspaceChoiceMenu value={security} choices={choices} label={t(fromGroup ? "createPanel.fromGroup.access" : "createPanel.security")} disabled={busy}
          className={`${styles.securityButton} ${styles.securityBoxed}`} row={styles.boxedRow} wide marks="secondary" data-teamspace-create-security={security}
          onChange={(kind) => {
            setSecurity(kind);
            controller.setAccess(kind === "default" ? "open" : kind);
            // The rest of the workspace has the members' role in an open teamspace's pages and no access to a closed or
            // private one's, as Notion's new teamspaces (seen 2026-11-18: an open one's rest "전체 허용").
            controller.setOpenPageRole(kind === "open" || kind === "default" ? controller.memberPageRole : "none");
          }}>
          {current.icon}
          <span className={styles.securityText}><span>{current.label}</span><span>{current.description}</span></span>
        </TeamspaceChoiceMenu>
      </div>
      {fromGroup ? (
        <div className={styles.field}>
          <span>{t("createPanel.fromGroup.role")}</span>
          <TeamspaceChoiceMenu value={groupRole} choices={roleChoices} label={t("createPanel.fromGroup.role")} disabled={busy}
            className={`${styles.securityButton} ${styles.securityBoxed}`} row={styles.boxedRow} wide gap={8} marks="secondary" data-teamspace-create-group-role={groupRole}
            onChange={(role: TeamspaceMemberRole) => setGroupRole(role)}>
            <SharePeopleIcon size="regular" aria-hidden="true" />
            <span className={styles.securityText}>
              <span>{roleChoices.find((choice) => choice.value === groupRole)?.label}</span>
              <span>{roleChoices.find((choice) => choice.value === groupRole)?.description}</span>
            </span>
          </TeamspaceChoiceMenu>
        </div>
      ) : null}
      {fromGroup ? (
        <div className={styles.createFootStacked}>
          <button type="submit" className={styles.primary} disabled={busy || !controller.name.trim()}>{t("createPanel.fromGroup.submit")}</button>
          <button type="button" className={styles.quietWide} disabled={actionBusy} onClick={onCancel}>{t("common:actions.cancel")}</button>
        </div>
      ) : (
        <div className={styles.createFoot}>
          <button type="submit" className={styles.primary} disabled={busy || !controller.name.trim()}>{t("createPanel.submit")}</button>
        </div>
      )}
      {controller.error ? <p className={styles.error} role="alert" {...writeUncertainMark(controller.error)}>{controller.error}</p> : null}
      {closeButton ? <span className={styles.closeSlot}>{closeButton}</span> : null}
    </form>
  );
}
