import { useEffect, useRef, useState, type ReactNode } from "react";
import { ShareErrorLine } from "./ShareErrorLine";
import { usePendingChoice } from "@/lib/pendingChoice";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown, ChevronRight, GlobeIcon, InfoIcon, LockIcon, PersonCircleIcon } from "@/icons/leafen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { sharePhoneStyles as phoneCss } from "./TopBarSharePhone";
import { publicMutationRoles, type PublicMutationRole, type PublicMutationState } from "@/publicMutationWire.generated";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { sharePermissionLabel, TopBarSharePermissionPicker } from "./TopBarSharePermissionPicker";
import { TopBarShareWebExpiryPhone } from "./TopBarShareWebExpiryPhone";
import { TopBarShareWebRole, type WebLinkRole } from "./TopBarShareWebRole";
import { styles } from "./topBarStyles";
import access from "./TopBarShareAccess.module.css";
import choiceCss from "./TopBarShareGeneralChoices.module.css";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { inheritedWorkspaceLowers } from "./TopBarShareInheritanceActions";
import { ShareRowTip, ShareRowTipPage } from "./ShareRowTip";

/** A row of general access, and a choice in its menu. */
type GeneralRow = "invited" | "workspace" | "web";
/** A change to general access: a choice in a row's menu, or a row's role menu taking that row off. */
export type GeneralAccessChange = GeneralRow | "noWorkspace" | "noWeb";
type Held = GeneralRow | "both";

const WEB_LINK_ROLES = [publicMutationRoles.edit, publicMutationRoles.comment, publicMutationRoles.view];

const heldOf = (workspace: boolean, web: boolean): Held => workspace && web ? "both" : web ? "web" : workspace ? "workspace" : "invited";
const hasWorkspace = (held: Held) => held === "workspace" || held === "both";
const hasWeb = (held: Held) => held === "web" || held === "both";
function heldAfter(held: Held, change: GeneralAccessChange): Held {
  if (change === "invited") return "invited";
  return heldOf(change === "workspace" || (change !== "noWorkspace" && hasWorkspace(held)),
    change === "web" || (change !== "noWeb" && hasWeb(held)));
}

/**
 * A row's menu, as Notion's (seen 2026-11-08): with one row, the three
 * choices and a ✓ on the row's own; with the workspace's row and the web's
 * both on, each row's menu holds only "초대받은 사용자만" and its own choice,
 * the web's with a ✓ and the workspace's without. Pressing the checked choice
 * does nothing and leaves the menu open — a phone's sheet open with that row
 * lit — as Notion's (2026-11-18: a Galaxy A90's Chrome in two tabs, w1945 at
 * 14:4x and w1949 at 15:12; Chrome 1440 in two tabs at 15:13 and 15:35, w1949).
 */
function choicesOf(row: GeneralRow, held: Held): { choice: GeneralRow; checked: boolean }[] {
  if (held === "both") return [{ choice: "invited", checked: false }, { choice: row, checked: row === "web" }];
  return (["invited", "workspace", "web"] as const).map(choice => ({ choice, checked: choice === row }));
}

/**
 * "일반 사용 권한", as Notion's (seen 2026-10-06, 2026-11-08): who besides
 * the people named may open the page — only those invited, everyone in the
 * workspace (on the page, from a page above or through an open teamspace),
 * anyone on the web with the link, or the workspace and the web together on
 * two rows. Choosing the workspace or the web adds its row and leaves the
 * other's as set; "초대받은 사용자만" in either row's menu takes both off;
 * a row's role menu "제거" takes that row alone off. The web link's expiry
 * is in its role's menu (seen 2026-10-11).
 */
export function TopBarShareGeneralAccess({ state, disabled, readOnly: viewerReadOnly, onAccess, note, onRole, onHideFromSearch, onWebRole, expiresAt, onExpiry }: {
  state: PublicMutationState | null; disabled: boolean; note?: string;
  /** Someone who can't change sharing reads it as words, as Notion's (seen 2026-11-04). */
  readOnly: boolean;
  onAccess(change: GeneralAccessChange): Promise<boolean>; onRole(role: PublicMutationRole): void; onHideFromSearch(hide: boolean): void;
  onWebRole(role: WebLinkRole): void; expiresAt: string; onExpiry(expiresAt: string): void;
}) {
  const { t } = useTranslation(["topBar", "peopleProfiles"]);
  const { phone } = useSidebarLayout(true);
  const [choosing, setChoosing] = useState<GeneralRow | null>(null);
  // The checked choice pressed in a phone's sheet stays lit, the sheet open (choicesOf).
  const [lit, setLit] = useState<GeneralRow | null>(null);
  // Choosing the workspace on a touch screen opens its role sheet at once, as Notion's phone and iPad (seen 2026-11-16).
  const [workspaceSheet, setWorkspaceSheet] = useState(false);
  // People's database page names its own rows' general access, and no one
  // changes them there, as Notion's (seen 2026-11-16).
  const readOnly = viewerReadOnly || state?.generalAccessFixed === true;
  const ownWeb = state?.webSharingEnabled ?? false;
  // A page under a page whose web link is on opens by it too, and shows it
  // as general access, as Notion's (seen 2026-11-06). Changing it here
  // unlinks the page's sharing from its parent's (TopBarShare).
  const inheritedWeb = !ownWeb ? state?.inheritedWebLink ?? null : null;
  const web = ownWeb || Boolean(inheritedWeb);
  const roles = [publicMutationRoles.view, publicMutationRoles.comment, publicMutationRoles.edit, publicMutationRoles.fullAccess];
  const inherited = [state?.teamspace?.openRole, state?.workspaceInheritedRole].filter(Boolean) as PublicMutationRole[];
  const inheritedRole = inherited.sort((a, b) => roles.indexOf(b) - roles.indexOf(a))[0];
  const openRole = [...inherited, state?.workspacePermission?.role].filter(Boolean)
    .sort((a, b) => roles.indexOf(b as PublicMutationRole) - roles.indexOf(a as PublicMutationRole))[0] as PublicMutationRole | undefined;
  const workspaceLabel = t("everyoneInWorkspace", { name: state?.workspaceName ?? "" });
  const workspaceInitial = (state?.workspaceName ?? "").trim().slice(0, 1).toUpperCase() || "W";
  const held = heldOf(Boolean(openRole), web);
  // The choice made shows at once while its change is out, and the row says
  // so after ten seconds without an answer (lib/pendingChoice.ts).
  const pending = usePendingChoice<Held>(held, disabled);
  const shown = pending.shown;
  const rows: GeneralRow[] = shown === "both" ? ["workspace", "web"] : [shown];
  // A choice the server refused takes its row and the sheet opened with it away.
  useEffect(() => { if (!hasWorkspace(shown)) setWorkspaceSheet(false); }, [shown]);
  const change = (next: GeneralAccessChange) => {
    const after = heldAfter(held, next);
    if (after === held) return;
    pending.pick(after);
    void onAccess(next);
  };
  const webAllowed = web || state?.publishingAllowed !== false;
  const labelOf = (row: GeneralRow) => row === "web" ? t("anyoneOnWeb") : row === "workspace" ? workspaceLabel : t("invitedOnly");
  // An open teamspace lets everyone in the workspace in; it is set there, so
  // here it can't be taken off. A page above's lets them in until the page
  // lowers it or takes it off, unlinking (TopBarShareInheritanceActions).
  const lowers = inheritedWorkspaceLowers(state);
  // Everyone's role menu hides the page from their search, the workspace's own row or the one from a page above,
  // whose change unlinks the page (TopBarShare).
  const searchToggle = state?.workspacePermission || lowers ? { label: t("hideFromSearch"),
    checked: state?.workspacePermission?.hideFromSearch === true, onChange: onHideFromSearch } : undefined;
  const keptRole = lowers ? undefined : inheritedRole;
  const offOf = (choice: GeneralRow) => choice === "invited" ? Boolean(keptRole)
    : choice === "workspace" ? !state?.workspaceSharingAllowed : !webAllowed;
  const webPicker = !hasWeb(held) ? null : inheritedWeb ? (readOnly
      ? <TopBarSharePermissionPicker readOnly value={inheritedWeb.role} disabled onChange={onRole} />
      : <TopBarShareWebRole role={inheritedWeb.role as WebLinkRole} expiresAt={inheritedWeb.expiresAt} disabled={disabled} onRole={onWebRole}
        onExpiry={onExpiry} onRemove={() => change("noWeb")} note={t("teamspaceRoleNote")} />)
    : readOnly ? null
    : <TopBarShareWebRole role={state?.shareLink?.role ?? "view"} expiresAt={expiresAt} disabled={disabled} onRole={onWebRole}
      onExpiry={onExpiry} onRemove={() => change("noWeb")} />;
  const workspacePicker = !openRole || !hasWorkspace(held) ? null
    : readOnly ? <TopBarSharePermissionPicker readOnly value={openRole} disabled onChange={onRole} />
    : <TopBarSharePermissionPicker value={openRole} disabled={disabled || !state?.workspaceSharingAllowed}
      onChange={onRole} onRemove={!keptRole ? () => change("noWorkspace") : undefined}
      toggle={searchToggle}
      minimumRole={keptRole} note={lowers ? t("teamspaceRoleNote") : undefined} />;
  const pickerOf = (row: GeneralRow) => row === "web" ? webPicker : row === "workspace" ? workspacePicker : null;
  // A touch screen's rows, as a person's (Notion's phone and iPad, seen
  // 2026-11-13): the role under the choice and "⋯" opening "사용 권한 변경",
  // the web link's with its expiry and "제거".
  const webLink = inheritedWeb ? { role: inheritedWeb.role as WebLinkRole, expiresAt: inheritedWeb.expiresAt }
    : { role: (state?.shareLink?.role ?? "view") as WebLinkRole, expiresAt };
  const webRole = usePendingChoice<WebLinkRole>(webLink.role, disabled);
  // The workspace's row shows as soon as it is chosen, with the role its grant asks for (TopBarShare) until the answer.
  const workspaceRole = openRole ?? publicMutationRoles.fullAccess;
  const roleOf = (row: GeneralRow) => row === "web" ? (hasWeb(held) ? sharePermissionLabel(webRole.shown, t) : "")
    : row === "workspace" && hasWorkspace(shown) ? sharePermissionLabel(workspaceRole, t) : "";
  const touchPickerOf = (row: GeneralRow) => readOnly ? null : row === "web" ? (hasWeb(held) ? <TopBarSharePermissionPicker more
      value={webRole.shown} roles={WEB_LINK_ROLES} disabled={disabled} note={inheritedWeb ? t("teamspaceRoleNote") : undefined}
      onChange={next => { const role = next as WebLinkRole; if (role !== webLink.role) webRole.pick(role); onWebRole(role); }}
      onRemove={() => change("noWeb")}
      removeHead={<TopBarShareWebExpiryPhone expiresAt={webLink.expiresAt} disabled={disabled} onExpiry={onExpiry} />} /> : null)
    : row === "workspace" && hasWorkspace(shown) ? <TopBarSharePermissionPicker more value={workspaceRole}
      disabled={disabled || !state?.workspaceSharingAllowed} onChange={onRole} onRemove={!keptRole ? () => change("noWorkspace") : undefined}
      toggle={searchToggle} target={<><span className={phoneCss.generalTile}>{workspaceInitial}</span><span>{workspaceLabel}</span></>} removeMark
      sheetOpen={workspaceSheet} onSheetOpen={setWorkspaceSheet}
      minimumRole={keptRole} note={lowers ? t("teamspaceRoleNote") : undefined} /> : null;
  const pick = (row: GeneralRow, choice: GeneralRow) => { if (choice !== row || choice === "invited") change(choice); };
  // The web link's choice goes back to the share sheet, as Notion's; the workspace's opens its roles over it.
  const pickTouch = (row: GeneralRow, choice: GeneralRow) => {
    pick(row, choice);
    if (choice === "workspace" && !hasWorkspace(shown) && state?.workspaceSharingAllowed) setWorkspaceSheet(true);
  };
  // The workspace's owners hold People's pages: their row comes first, with
  // full access as words, as Notion's person page and People database name
  // "<workspace>의 소유자 — 전체 허용" (seen 2026-11-16).
  const owners = state?.workspaceOwnersHold ? t("ownersOfWorkspace", { name: state.workspaceName }) : "";
  const fullAccess = sharePermissionLabel(publicMutationRoles.fullAccess, t);
  // A person's page's everyone row is People's rule, and says so over it, as Notion's (seen 2026-11-16 at
  // 1440 in two tabs: that the access comes from People, and that changing it narrows who reaches the page).
  const peopleTip = owners && lowers ? <Trans t={t} i18nKey="topBar:rowTipInherited" values={{ title: t("peopleProfiles:people") }}
    components={{ page: <ShareRowTipPage icon={<PersonCircleIcon size="small" />} /> }} /> : undefined;
  // A phone's, as Notion's (seen 2026-10-27): a 40px tile and the choice
  // with "›", which opens the choices as a sheet of their own.
  if (phone) {
    const tileOf = (row: GeneralRow) => <span className={phoneCss.generalTile}>{row === "web" ? <GlobeIcon size="regular" />
      : row === "workspace" ? workspaceInitial : <LockIcon size="page" weight="fill" />}</span>;
    const iconOf = (choice: GeneralRow) => choice === "web" ? <GlobeIcon size="small" />
      : choice === "workspace" ? <span className={choiceCss.choiceMark}>{workspaceInitial}</span> : <LockIcon size="small" weight="fill" />;
    const textOf = (row: GeneralRow, more: boolean) => <span className={phoneCss.generalText}>
      <span className={phoneCss.generalChoice}><span className={phoneCss.generalLabel}>{labelOf(row)}</span>
        {more ? <ChevronRight className={phoneCss.generalMore} size="small" aria-hidden="true" /> : null}</span>
      {roleOf(row) ? <span className={phoneCss.personRole} data-share-general-role>{roleOf(row)}</span> : null}
    </span>;
    return <>
      <div className={phoneCss.sectionLabel}>{t("generalAccess")}</div>
      {owners ? <div className={phoneCss.general} data-share-general-access="owners">
        <span className={phoneCss.generalButton}><span className={phoneCss.generalTile}>{workspaceInitial}</span>
          <span className={phoneCss.generalText}><span className={phoneCss.generalChoice}><span className={phoneCss.generalLabel}>{owners}</span></span>
            <span className={phoneCss.personRole} data-share-general-role>{fullAccess}</span></span></span>
      </div> : null}
      {rows.map(row =><div key={row} className={phoneCss.general} data-share-general-access={row}>
        {readOnly ? <span className={phoneCss.generalButton}>{tileOf(row)}{textOf(row, false)}</span>
          : <button type="button" className={phoneCss.generalButton} disabled={disabled} aria-haspopup="dialog" aria-expanded={choosing === row}
            aria-label={`${t("generalAccessMenu")}: ${labelOf(row)}`} onClick={() => setChoosing(row)}>
            {tileOf(row)}{textOf(row, true)}
          </button>}
        {touchPickerOf(row)}
      </div>)}
      {choosing ? <PhoneSheet headEven title={t("generalAccess")} onClose={() => { setChoosing(null); setLit(null); }} data={{ "data-share-general-sheet": "" }}>
        <div className={choiceCss.choices}>
          {choicesOf(choosing, held).map(({ choice, checked }) => <button key={choice} type="button" className={choiceCss.choice} disabled={offOf(choice)}
            data-share-general-choice={choice} aria-pressed={checked} data-lit={lit === choice ? "true" : undefined}
            onClick={() => { if (checked) { setLit(choice); return; } setChoosing(null); setLit(null); pickTouch(choosing, choice); }}>
            <span className={choiceCss.choiceIcon}>{iconOf(choice)}</span>
            <span className={choiceCss.choiceLabel}>{labelOf(choice)}</span>
            {checked ? <CheckIcon className={choiceCss.choiceCheck} size="page" aria-hidden="true" /> : null}
          </button>)}
        </div>
      </PhoneSheet> : null}
    </>;
  }
  return <>
    <div className={access.generalLabel}>{t("generalAccess")}</div>
    {owners ? <div className={access.generalRow} data-share-general-access="owners">
      <span className={access.generalIcon}>{workspaceInitial}</span>
      <span className={access.generalPlain}>{owners}</span>
      <TopBarSharePermissionPicker readOnly value={publicMutationRoles.fullAccess} disabled onChange={onRole} />
    </div> : null}
    {rows.map(row => <GeneralAccessRow key={row} row={row} label={labelOf(row)} readOnly={readOnly} disabled={disabled} waitingNote={pending.note}
      tip={row === "workspace" ? peopleTip : undefined}
      mark={row === "web" ? <GlobeIcon size="regular" /> : row === "workspace" ? workspaceInitial : <LockIcon size="page" weight="fill" />}
      choices={choicesOf(row, held).map(({ choice, checked }) => ({ choice, checked, label: labelOf(choice), off: offOf(choice),
        // With the workspace's mark, as Notion's (seen 2026-10-17).
        mark: choice === "web" ? <GlobeIcon size="regular" className={access.generalItemGlobe} aria-hidden="true" />
          : choice === "workspace" ? <span className={access.generalItemMark} aria-hidden="true">{workspaceInitial}</span>
          : <LockIcon size="regular" weight="fill" aria-hidden="true" /> }))}
      onPick={choice => pick(row, choice)} picker={pickerOf(row)} />)}
    {/* A change of it not saved, or not known to be, is told under its rows. */}
    <ShareErrorLine text={note ?? ""} className={styles.shareError} data-share-general-note />
    {/* What a web link or a published site lets anyone see, under the rows,
        as Notion's (seen 2026-11-05; with a site alone, 2026-11-06). */}
    {web || state?.site?.published ? <p className={access.webNote} data-share-web-note><InfoIcon size="small" aria-hidden="true" /><span>{t("webLinkNote")}</span></p> : null}
  </>;
}

/** One row of general access on a computer: its mark, its choice with a menu of its own, and its role. */
function GeneralAccessRow({ row, label, mark, readOnly, disabled, waitingNote, choices, onPick, picker, tip }: {
  row: GeneralRow; label: string; mark: ReactNode; readOnly: boolean; disabled: boolean; waitingNote?: string; tip?: ReactNode;
  choices: { choice: GeneralRow; checked: boolean; label: string; off: boolean; mark: ReactNode }[];
  onPick(choice: GeneralRow): void; picker: ReactNode;
}) {
  const { t } = useTranslation("topBar"), trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { state: menuState, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled);
  const placement = usePopoverPlacement(menu, menuState.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "start", gap: 4 },
  ) : null, [menuState.isOpen], undefined, { kind: "menu", onEscape: menuState.close, onOutsidePress: menuState.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  return <div className={access.generalRow} data-share-general-access={row}>
    <span className={access.generalIcon}>{mark}</span>
    {readOnly ? <span className={access.generalPlain}>{label}</span>
      : <ShareTipped tip={tip}><button {...buttonProps} ref={trigger} className={access.generalButton} aria-label={`${t("generalAccessMenu")}: ${label}`}
        data-waiting-note={waitingNote}>
        <span>{label}</span><ChevronDown size="small" aria-hidden="true" />
      </button></ShareTipped>}
    {picker ?? <span />}
    {menuState.isOpen ? createPortal(<div ref={menu} className={`${styles.sharePermissionMenu} ${access.generalMenu}`}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto" }}>
      <NativeActionMenu {...menuProps} aria-label={t("generalAccess")} autoFocus={autoFocus}>
        {choices.map(item => <NativeActionMenuItem key={item.choice} id={item.choice} role="menuitemradio" aria-checked={item.checked} aria-label={item.label}
          disabled={item.off} onClick={() => { if (item.checked) return; menuState.close(); onPick(item.choice); }}>
          <span className={access.generalItem}>{item.mark}{item.label}</span>
          {item.checked ? <CheckIcon size="small" className={access.roleCheck} aria-hidden="true" /> : null}
        </NativeActionMenuItem>)}
      </NativeActionMenu>
    </div>, document.body) : null}
  </div>;
}

/** A row's words with the tip its hover shows, where it has one. */
function ShareTipped({ tip, children }: { tip?: ReactNode; children: ReactNode }) {
  return tip ? <ShareRowTip text={tip}>{children}</ShareRowTip> : <>{children}</>;
}
