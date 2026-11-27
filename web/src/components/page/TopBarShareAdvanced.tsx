import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { WorkspaceApp } from "@/appRegistrationWire.generated";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { CheckIcon, ChevronLeft, ChevronRight, ConnectionsIcon } from "@/icons/leafen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, usePhoneSheetHead } from "@/components/shell/PhoneSheet";
import { sharePhoneStyles as phoneCss } from "./TopBarSharePhone";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { PublicMutationAgentGrantScopeInput, PublicMutationState } from "@/publicMutationWire.generated";
import { TopBarSharePageRules, type PageRulesAccess } from "./TopBarSharePageRules";
import { sharePermissionLabel } from "./TopBarSharePermissionPicker";
import { styles } from "./topBarStyles";
import access from "./TopBarShareAccess.module.css";
import advanced from "./TopBarShareAdvanced.module.css";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { useOpenedFromKeyboard } from "@/lib/openedFromKeyboard";

/**
 * "고급" in the share tab, as Notion's (seen 2026-10-11, its sections placed
 * as Notion's 2026-11-14): a screen of its own in the window with "‹ 고급" at
 * its head, a database's rule sections, then "에이전트 사용 권한 설정" —
 * who may give agents access, and the agents that have it here. Leafen's
 * agents are the apps installed as their own accounts, connected to a page
 * the way a person is shared with it. People with full access always may;
 * "사용 권한이 있는 모든 사용자" lets those who may edit the page connect and
 * disconnect apps too (a page's other shares stay with full access). A
 * database's "속성 사용 권한" comes from the organization module.
 */
export function TopBarShareAdvanced({ state, loadApps, database, disabled, pageRules, propertyRules, guestLinks, onAgentGrantScope, onBack }: {
  state: PublicMutationState | null; database: boolean; disabled: boolean;
  /** "게스트 초대 링크" (TopBarShareGuestLinks), for whoever may share the page. */
  guestLinks?: ReactNode;
  /** A database's page-level rules. */
  pageRules: PageRulesAccess;
  /** Its properties' access rules, from the organization module. */
  propertyRules: ReactNode;
  loadApps(signal: AbortSignal): Promise<WorkspaceApp[]>;
  onAgentGrantScope(scope: AgentGrantScope): void;
  onBack(): void;
}) {
  const { t } = useTranslation("topBar");
  const backRef = useRef<HTMLButtonElement>(null);
  const [apps, setApps] = useState<WorkspaceApp[] | null>(null);
  const { phone } = useSidebarLayout(true);
  // A phone's names the sheet's head "‹ 고급", as Notion's (seen 2026-10-27), with its count 4px after it in the
  // line's middle, as the computer's head has it (Notion's "고급 1" read 2026-11-18 on a Galaxy A90 at 411: the
  // words and an 18.3px mark in a 22px line, gap 4; w1949 saw "고급 2" too).
  const count = shareAdvancedCount(state);
  const headTitle = useMemo(() => count
    ? <span className={advanced.advancedHeadTitle}>{t("advanced")}<AdvancedCountMark count={count} head /></span> : t("advanced"), [count, t]);
  usePhoneSheetHead(phone ? { title: headTitle, onBack } : null);
  // Pressed open, the screen takes no focus, as Notion's; opened with keys,
  // "‹" takes them (not measured). Measured 2026-11-16 after the press only (no script in the page before it):
  // Notion in Chrome 1440 (two tabs 6 minutes apart), "고급" pressed, nothing ringed, "z" typed went
  // nowhere, the focus the page's; on a Galaxy A90's Chrome the same (two tabs 20 minutes apart), the keyboard
  // down. Leafen had "‹" (a phone's first row) take it, ringed by the letter.
  const keysNow = useOpenedFromKeyboard();
  const [openedByKeys] = useState(keysNow);
  useEffect(() => { if (openedByKeys) backRef.current?.focus(); }, [openedByKeys]);
  useEffect(() => {
    const controller = new AbortController();
    loadApps(controller.signal).then(setApps, () => { if (!controller.signal.aborted) setApps([]); });
    return () => controller.abort();
  }, [loadApps]);
  const agents = (apps ?? []).flatMap(app => {
    const permission = state?.permissions.find(item => item.principalType === "user" && item.principalId === app.userId);
    return permission ? [{ app, role: permission.role }] : [];
  });
  // People's pages hold People's own rule, its database's and each person's (people_database_page_rule.go).
  const rules = (database || state?.workspaceOwnersHold === true) && state?.canManage !== false;
  const agentScope = shownAgentScope(state);
  const agentSection = <>
    <div className={phone ? phoneCss.advancedLabel : advanced.advancedLabel}>{t("agentAccess")}</div>
    <AgentGrantPicker value={agentScope} disabled={disabled || !state} onChange={onAgentGrantScope} phone={phone} />
    {agents.map(({ app, role }) => <div key={app.appId} className={styles.shareRow} data-share-agent={app.appId}>
      <PersonAvatar className={styles.shareAvatar} userId={app.userId} name={app.name} size={32} />
      <span className={styles.shareText}><span>{app.name}</span><span>{t("agentAccount")}</span></span>
      <span className={styles.sharePermission}>{sharePermissionLabel(role, t)}</span>
    </div>)}
    {apps !== null && !agents.length ? <p className={phone ? phoneCss.advancedNote : advanced.advancedNote} data-share-no-agents>
      {t(database ? "noAgentsInDatabase" : "noAgentsInPage")} {t("agentScopeNote")}
    </p> : null}
  </>;
  if (phone) return <div className={`${styles.sharePanel} ${phoneCss.advancedScreen}`} data-share-panel="advanced">
    {rules ? <div className={phoneCss.advancedSection} data-share-advanced-section="page-rules">
      <TopBarSharePageRules rules={pageRules} disabled={disabled || !state} canAdd={database} />
    </div> : null}
    {rules && propertyRules ? <div className={phoneCss.advancedSection} data-share-advanced-section="property-rules">{propertyRules}</div> : null}
    <div className={phoneCss.advancedSection} data-share-advanced-section="agents">{agentSection}</div>
    {guestLinks ? <div className={phoneCss.advancedSection} data-share-advanced-section="guest-links">
      <div className={phoneCss.advancedLabel}>{t("guestLinks")}</div>{guestLinks}</div> : null}
  </div>;
  return <div className={styles.sharePanel} data-share-panel="advanced">
    <div className={`${access.stepHeader} ${advanced.advancedHead}`} data-share-advanced-header>
      <button ref={backRef} type="button" aria-label={t("backToSharing")} onClick={onBack}>
        <ChevronLeft size="regular" aria-hidden="true" />
      </button>
      <span>{t("advanced")}</span>
      <ShareAdvancedCount state={state} />
    </div>
    <div className={advanced.advancedBody}>
      {rules ? <div className={`${advanced.advancedSection} ${advanced.advancedRules}`} data-share-advanced-section="page-rules">
        <TopBarSharePageRules rules={pageRules} disabled={disabled || !state} canAdd={database} />
      </div> : null}
      {rules && propertyRules ? <div className={`${advanced.advancedSection} ${advanced.advancedRules}`} data-share-advanced-section="property-rules">{propertyRules}</div> : null}
      <div className={advanced.advancedSection} data-share-advanced-section="agents">{agentSection}</div>
      {guestLinks ? <div className={advanced.advancedSection} data-share-advanced-section="guest-links">
        <div className={advanced.advancedLabel}>{t("guestLinks")}</div>{guestLinks}</div> : null}
    </div>
  </div>;
}

type AgentGrantScope = PublicMutationAgentGrantScopeInput["scope"];

/**
 * Who may give agents access, as this person reads it. A guest reads "모든 권한이 있는 사용자만" whatever the page
 * holds, as Notion's (seen 2026-11-16 in Chrome 1440 on one synthetic page: the owner's "사용 권한이 있는 모든 사용자",
 * the same page's guest with full access "모든 권한이 있는 사용자만", its ✓ there too; a guest who may edit read the same
 * on a database and a page the same night, w1377). A guest with full access still changes the page's own choice: the
 * one picked reaches the owner, the guest's line stays as it was.
 */
function shownAgentScope(state: PublicMutationState | null): AgentGrantScope {
  return state?.viewerGuest ? "full_access" : state?.agentGrantScope ?? "full_access";
}

/**
 * "고급"'s count, beside its word in the window's foot and at the head of its own screen: its page-level and property
 * rules, and one more when who may give agents access is not the page's default. Notion counts its non-default
 * "모든 권한이 있는 사용자만", which a guest always reads, so a guest always sees 1 (seen 2026-11-16 in Chrome 1440 dark: an
 * owner's page on "사용 권한이 있는 모든 사용자" shows none, switched to "모든 권한이 있는 사용자만" it shows 1 at the head
 * and the foot at once). Leafen's default is "모든 권한이 있는 사용자만", so it counts a page opened to
 * "사용 권한이 있는 모든 사용자", and a guest, who reads the default, sees none from it.
 */
export function ShareAdvancedCount({ state }: { state: PublicMutationState | null }) {
  return <AdvancedCountMark count={shareAdvancedCount(state)} />;
}

function AdvancedCountMark({ count, head = false }: { count: number; head?: boolean }) {
  return count ? <span className={head ? `${advanced.advancedCount} ${advanced.advancedHeadCount}` : advanced.advancedCount} data-share-advanced-count>{count}</span> : null;
}

function shareAdvancedCount(state: PublicMutationState | null) {
  return state ? (state.pageRuleCount ?? 0) + (shownAgentScope(state) === "full_access" ? 0 : 1) : 0;
}

/**
 * The row is the menu's button; the menu (300px) opens under its right end
 * with a ✓ on the choice. One who may not change it — a guest, or anyone
 * short of full access — sees the row dimmed whole with no "›", as Notion's
 * (seen 2026-11-15 by a guest who may edit), and pressing it does nothing.
 * The row's value and the choices word full access apart, as Notion's: the
 * row "모든 권한이 있는 사용자만" / "Full access only", its choice "모두 허용 권한이
 * 있는 사람만" / "Only people with Full access" (seen 2026-11-16 in Chrome 1440
 * dark, Korean three times and English once), the role word written as the
 * share menu's "전체 허용".
 */
function AgentGrantPicker({ value, disabled, phone, onChange }: { value: AgentGrantScope; disabled: boolean; phone: boolean; onChange(next: AgentGrantScope): void }) {
  const { t } = useTranslation("topBar"), trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 4 },
  ) : null, [state.isOpen], undefined, { kind: "picker", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  const label = (scope: AgentGrantScope) => t(scope === "access" ? "everyoneWithAccess" : "fullAccessOnly");
  const choiceLabel = (scope: AgentGrantScope) => t(scope === "access" ? "everyoneWithAccess" : "fullAccessOnlyChoice");
  const [choosing, setChoosing] = useState(false);
  // A phone's row is Notion's (seen 2026-10-27): a 40px tile, 17px words and
  // the choice grey. Its choices are the menu laid in a whole sheet, as
  // Notion's (read 2026-11-18 on a Galaxy A90 at 411, twice, w1938 and
  // w1945): an empty 44px head under the grip, no title or "✕", then the
  // menu's 300px with the sheet's 16px in — one white 12px-round group
  // 268px across of 45px rows, the 17px choice 16px in and its ✓ 16px from
  // the end (TopBarShareAdvanced.module.css).
  if (phone) return <>
    <button type="button" className={phoneCss.advancedRow} disabled={disabled} data-share-agent-grant={value} aria-haspopup="dialog"
      aria-expanded={choosing} onClick={() => setChoosing(true)}>
      <span className={phoneCss.generalTile}><ConnectionsIcon size="regular" aria-hidden="true" /></span>
      <span>{t("whoCanGrantAgents")}</span>
      <span className={phoneCss.advancedValue}>{label(value)}</span>
      {disabled ? null : <ChevronRight size="small" aria-hidden="true" />}
    </button>
    {choosing ? <PhoneSheet title={null} label={t("whoCanGrantAgents")} bodyClassName={advanced.agentGrantSheet}
      onClose={() => setChoosing(false)} data={{ "data-share-agent-grant-sheet": "" }}>
      <PhoneSheetGroup>
        {(["access", "full_access"] as const).map(scope => <PhoneSheetRow key={scope} label={choiceLabel(scope)} more={false}
          value={value === scope ? <CheckIcon size="small" aria-hidden="true" /> : undefined} data={{ "data-share-agent-grant-choice": scope }}
          onPress={() => { setChoosing(false); if (scope !== value) onChange(scope); }} />)}
      </PhoneSheetGroup>
    </PhoneSheet> : null}
  </>;
  return <>
    <button {...buttonProps} ref={trigger} className={advanced.advancedRow} data-share-agent-grant={value}>
      <span className={advanced.advancedTile}><ConnectionsIcon size="regular" aria-hidden="true" /></span>
      <span>{t("whoCanGrantAgents")}</span>
      <span className={advanced.advancedValue}>{label(value)}{disabled ? null : <ChevronRight size="small" aria-hidden="true" />}</span>
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={`${styles.sharePermissionMenu} ${advanced.agentGrantMenu}`}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto" }}>
      <NativeActionMenu {...menuProps} aria-label={t("whoCanGrantAgents")} autoFocus={autoFocus}>
        {(["access", "full_access"] as const).map(scope => <NativeActionMenuItem id={scope} key={scope} role="menuitemradio"
          aria-checked={value === scope} onClick={() => { state.close(); if (scope !== value) onChange(scope); }}>
          <span>{choiceLabel(scope)}</span>
          {value === scope ? <CheckIcon size="small" className={access.roleCheck} aria-hidden="true" /> : null}
        </NativeActionMenuItem>)}
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}
