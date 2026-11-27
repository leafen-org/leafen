import type { ProductStatusSink } from "@/lib/productStatus";
import { LocalAddressHint } from "@/components/product/LocalAddressHint";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { sharePhoneStyles } from "./TopBarSharePhone";
import { useDialog } from "react-aria";
import { Tab, TabList, TabPanel, Tabs } from "react-aria-components";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { routeOverlayKeyDown } from "@/lib/overlayLayer";
import { TopBarShareAccessPanel } from "./TopBarShareAccessPanel";
import { useShareInviteDraft } from "./TopBarShareInviteDraft";
import { TopBarSharePublishTab, useShareSiteDraft } from "./TopBarSharePublishTab";
import type { GeneralAccessChange } from "./TopBarShareGeneralAccess";
import type { WebLinkRole } from "./TopBarShareWebRole";
import { useShareInheritanceActions } from "./TopBarShareInheritanceActions";
import { TopBarEditRequest } from "./TopBarEditRequest";
import access from "./TopBarShareAccess.module.css";
import { publishSheetFill } from "./TopBarSitePublishIntro";
import { copyText } from "@/lib/clipboard";
import { newId } from "@/lib/ids";
import { userErrorText } from "@/lib/settingsErrors";
import { ShareErrorLine, shareErrorText, shareFieldErrors, type ShareChangeKind } from "./ShareErrorLine";
import { isUncertainWrite } from "@/lib/uncertainWrites";
import { publicMutationService } from "@/lib/publicMutationService";
import { organizationModule } from "@/lib/organizationModule";
import { PUBLIC_CONTENT_CHANGED_EVENT, takePageShareOpener, useShareReadAgain } from "./pageShareEvents";
import { publicMutationRoles, type PublicMutationInput, type PublicMutationPageRuleInput, type PublicMutationPermission,
  type PublicMutationRole, type PublicMutationShareInheritanceInput, type PublicMutationState } from "@/publicMutationWire.generated";
import type { Session } from "@/types";
import { styles } from "./topBarStyles";
import { serverUrl } from "@/lib/serverOrigin";
import { absolutePageUrl } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import { useOptionalProductToast } from "@/lib/productToast";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { sharePermissionLabel, shareRoleRank } from "./TopBarSharePermissionPicker";
import { PageIconGlyph } from "./PageIcon";
import { questionOverShare, shareLayerUnder, underQuestion, useFocusBackFromQuestion } from "./shareUnderQuestion";
import { pageTitle, shareParentTitle, type ShareMutationOutcome as MutationOutcome, type SharePage } from "./topBarShareModel";
import { listWorkspaceApps } from "../settings/WorkspaceAppAccounts";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";

type ShareTab = "share" | "publish";
/** Unlinking a page's sharing from its parent's, or linking it again; or who may connect agents. */
type InheritanceRequest = { inheritance: Omit<PublicMutationShareInheritanceInput, "expectedRevision"> }
  | { agentGrantScope: "full_access" | "access" };

const clipboardBounds = { maxTextCharacters: 4096, maxHtmlCharacters: 0, maxEntries: 0, maxEntryCharacters: 0, maxTotalEntryCharacters: 0 };

function publicHref(state: PublicMutationState | null) {
  const link = state?.shareLink;
  if (!state?.webSharingEnabled || !link?.enabled || !link.token) return "";
  return serverUrl(`/share/${encodeURIComponent(link.token)}`);
}

/** The page's share window: a touch screen's is the phone's sheet, as Notion's iPad (TouchSheetLayout). */
export function TopBarShare(props: Parameters<typeof ShareWindow>[0]) {
  return <TouchSheetLayout><ShareWindow {...props} /></TouchSheetLayout>;
}

function ShareWindow({
  id,
  anchorRef,
  initialInvite = "",
  initialTab = "share",
  pageUrl,
  page,
  pages,
  session,
  canRequestEdit = true,
  onClose,
  onStatus: givenStatus, onRestore,
}: {
  id: string;
  /** The share button it hangs under, however it was asked for (a menu, a notice, an invite). */
  anchorRef: RefObject<HTMLElement | null>;
  /** A name or email to start the invite with. */
  initialInvite?: string;
  /** The tab it opens at, "웹에 게시" from a published page's banner. */
  initialTab?: ShareTab;
  /** A row page opens over its database container. */
  pageUrl?: string;
  page: SharePage;
  pages: SharePage[];
  session: Session;
  /** Whether someone who may only read or comment here can ask to edit. */
  canRequestEdit?: boolean;
  onClose: () => void;
  /** Where its results are said; without one (a row page's menu) the product's toasts, so every way in says the same. */
  onStatus?: ProductStatusSink;
  /** "게시"'s "복원" for a page in the trash, whether it came out; the window stays and reads it again, as Notion's (seen 2026-11-13). */ onRestore?: () => Promise<boolean>;
}) {
  const { t } = useTranslation(["topBar", "common", "siteBuilder"]), toast = useOptionalProductToast();
  const onStatus: ProductStatusSink = givenStatus ?? ((message, tone) => { if (tone && message) toast?.notify(message, tone); });
  const untitled = untitledPageDisplayTitle("page");
  const dialogRef = useRef<HTMLDivElement>(null);
  const inviteInputRef = useRef<HTMLInputElement>(null);
  // No button on its notice: this window is the share window (Notion, 2026-11-07).
  const notifyPageLinkCopied = usePageLinkCopyNotice(session, page.id);
  const backdropRef = useRef<HTMLButtonElement>(null);
  const operationRef = useRef<AbortController | null>(null);
  // What was last heard of the page's sharing draws the first frame — its people and how it is open — and the read below brings the server's.
  const [state, setStateValue] = useState<PublicMutationState | null>(() => publicMutationService.lastKnown(session, page.id));
  // The latest state, so invites sent one after another each carry the
  // revision the one before left.
  const stateRef = useRef<PublicMutationState | null>(state);
  const setState = (next: PublicMutationState | null) => { stateRef.current = next; setStateValue(next); };
  const [activeShareTab, setActiveShareTab] = useState<ShareTab>(initialTab);
  const [advanced, setAdvanced] = useState(false);
  const loadApps = useCallback((signal: AbortSignal) => listWorkspaceApps(session, page.workspaceId, signal),
    [session, page.workspaceId]);
  const PropertyRules = organizationModule()?.PropertyAccessShareRules;
  const pageRules = useMemo(() => ({
    load: (signal: AbortSignal) => publicMutationService.pageRules(session, page.id, signal),
    change: (input: PublicMutationPageRuleInput) => publicMutationService.changePageRule(session, page.id, input),
    countChanged: (delta: number) => {
      const current = stateRef.current;
      if (current) setState({ ...current, pageRuleCount: Math.max(0, (current.pageRuleCount ?? 0) + delta) });
    },
  }), [page.id, session]);
  const site = useShareSiteDraft(page, untitled, onStatus, t);
  // "게시" opens with the last heard site too, before the first paint.
  useLayoutEffect(() => { if (stateRef.current) site.hydrate(stateRef.current); }, []);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [errorCode, setErrorCode] = useState("");
  // Which control a change that failed came from: its words show under it
  // ("general" — the general access row), else at the window's end.
  const errorAtRef = useRef("");
  const [errorAt, setErrorAt] = useState("");
  const [loadAttempt, setLoadAttempt] = useState(0), [restoring, setRestoring] = useState(false);
  // A change sent without the server's answer, or this person's access changed
  // elsewhere: the window reads what the server holds again (useShareReadAgain).
  const readAgain = useShareReadAgain();
  const [copied, setCopied] = useState<"web" | "site" | "page" | null>(null);

  // A publish from the site editor that didn't take, or wasn't answered,
  // keeps what is being edited there (shareChangeTook).
  function hydrate(next: PublicMutationState, keepSiteDraft = false) {
    setState(next);
    if (!keepSiteDraft) site.hydrate(next);
  }

  useEffect(() => () => {
    const controller = operationRef.current;
    if (controller) controller.abort();
    operationRef.current = null;
  }, [page.id, session.user.id]);

  useEffect(() => {
    const controller = new AbortController();
    operationRef.current?.abort();
    operationRef.current = controller;
    setLoading(true);
    setErrorCode("");
    setErrorAt("");
    void publicMutationService.state(session, page.id, controller.signal, (next) => {
      if (operationRef.current !== controller) return;
      if (controller.signal.aborted) return;
      setState(next);
    }).then(({ state: next }) => {
      if (operationRef.current !== controller) return;
      if (controller.signal.aborted) return;
      // Read again while the site editor is open (its publish unanswered,
      // then answered), what is being edited there stays, as after a refusal.
      hydrate(next, site.styling);
    }).catch((error: unknown) => {
      if (operationRef.current !== controller || controller.signal.aborted) return;
      setErrorCode(userErrorText(error, t("topBar:shareLoadFailed")));
    }).finally(() => {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setLoading(false);
      }
    });
    return () => controller.abort();
  }, [page.id, session.user.id, loadAttempt, readAgain]);

  // A refusal the grey note above already explains says so once, by turning
  // that note red (decided 2026-11-06).
  const policyRefused = Boolean(state?.canManage !== false && (state?.guestsAllowed === false || state?.externalEmailAllowed === false)
    && errorCode === t("topBar:sharingSecurityBlocked"));

  // A change refused says why in the window (ShareErrorLine); one from the site's address row says so of the address.
  async function runMutation(
    input: Omit<PublicMutationInput, "requestId" | "expectedRevision"> | InheritanceRequest,
    successMessage?: string, change: Pick<ShareChangeKind, "address"> = {},
  ): Promise<MutationOutcome> {
    const current = stateRef.current;
    if (!current || busy) return "failed";
    const controller = new AbortController();
    operationRef.current?.abort();
    operationRef.current = controller;
    const pageID = page.id;
    const userID = session.user.id;
    setLoading(false);
    setBusy(true);
    setErrorCode("");
    site.setFieldErrors({});
    setErrorAt(errorAtRef.current);
    try {
      const onWindow = (next: PublicMutationState) => {
        if (operationRef.current !== controller) return;
        if (!controller.signal.aborted) setState(next);
      };
      const result = "inheritance" in input
        ? await publicMutationService.shareInheritance(session, page.id,
          { ...input.inheritance, expectedRevision: current.revision }, controller.signal, onWindow)
        : "agentGrantScope" in input
          ? await publicMutationService.agentGrantScope(session, page.id,
            { scope: input.agentGrantScope, expectedRevision: current.revision }, controller.signal, onWindow)
        : await publicMutationService.mutate(session, page.id,
          { ...input, requestId: newId(), expectedRevision: current.revision }, controller.signal, onWindow);
      if (operationRef.current !== controller) return "failed";
      if (controller.signal.aborted || page.id !== pageID || session.user.id !== userID) return "failed";
      hydrate(result.state);
      window.dispatchEvent(new Event(PUBLIC_CONTENT_CHANGED_EVENT));
      if (successMessage) onStatus(successMessage, "default");
      return "succeeded";
    } catch (error: unknown) {
      if (operationRef.current !== controller || controller.signal.aborted) return "failed";
      setErrorCode(shareErrorText(error, t, { ...change, action: "action" in input ? input.action : undefined }));
      site.setFieldErrors(shareFieldErrors(error, t));
      invite.noteRefusal(error, input);
      // A change sent without the server's answer isn't read back now: that
      // read waits on the same network, and the window stayed locked under
      // its words (2026-11-06). It is read again once the server answers.
      if (isUncertainWrite(error)) return "failed";
      try {
        const recovered = await publicMutationService.state(session, page.id, controller.signal);
        if (operationRef.current !== controller || controller.signal.aborted) return "failed";
        if (page.id !== pageID || session.user.id !== userID) return "failed";
        hydrate(recovered.state, site.styling);
        return "recovered";
      } catch {
        return "failed";
      }
    } finally {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setBusy(false);
        keepFocusInDialog();
      }
    }
  }

  // A button the change disabled or replaced took the focus with it; the
  // dialog keeps it, so Escape still closes it. A menu the change opened
  // (choosing "모든 사용자" opens its role menu) keeps it: menus are drawn
  // outside the dialog. A field marked data-focus-leaves that the change put away
  // leaves the keys on the page, as the modal's focus keeper does (TopBarSitePublished).
  function keepFocusInDialog() {
    const held = document.activeElement;
    requestAnimationFrame(() => {
      const dialog = dialogRef.current, active = document.activeElement;
      if (!dialog || dialog.contains(active) || active?.closest("[data-native-menu]")) return;
      if (held && !held.isConnected && held.closest("[data-focus-leaves]")) return;
      dialog.focus({ preventScroll: true });
    });
  }

  function closeNow() {
    const controller = operationRef.current;
    if (controller) controller.abort();
    operationRef.current = null;
    onClose();
  }

  // Closing with an invite not yet sent asks first, as Notion's (seen 2026-10-05): "예" drops it; "취소" gives the keys back where they
  // were (confirmAction) — the field after Escape in it, nothing after a press outside or on 공유 — as Notion's (twice on 2026-11-15 in
  // Chrome 1440, different tabs, no script before, the screen and a letter after). Chosen people, a message or a refused address make
  // a draft; words typed in the field alone close at once, as Notion's (seen 2026-10-28, 2026-11-04).
  function close() {
    if (!invite.unsent) {
      closeNow();
      return;
    }
    void confirmAction({
      message: t("topBar:discardInviteTitle"), confirmLabel: t("topBar:discardInvite"), cancelLabel: t("topBar:keepInvite"),
    }).then(discard => { if (discard) closeNow(); });
  }

  async function copyLink(target: "web" | "site" | "page", value: string) {
    if (!value || !(await copyText(value, clipboardBounds))) {
      onStatus(t("topBar:toast.couldntCopyLink"), "error");
      return;
    }
    setCopied(target);
    // The page's own address says who can open it, as the top bar's copy does; a web link only that it was copied (Notion, 2026-11-13).
    if (target === "page") { notifyPageLinkCopied(); return; }
    onStatus(target === "site" ? t("siteBuilder:copied") : t("topBar:copiedLink"), "default");
  }

  async function removePermission(permission: PublicMutationPermission) {
    const other = permission.otherAccess;
    // As Notion's (seen 2026-10-06): taking a share away from someone who still opens the page another
    // way says so first (an untitled database above as "새 데이터베이스", shareParentTitle); else it just goes.
    if (other) {
      const source = other.source === "teamspace" ? t("topBar:otherAccessTeamspace", { name: other.label })
        : other.source === "page" ? shareParentTitle({ title: other.label, database: other.database })
        : other.label || untitled;
      const remove = await confirmAction({
        message: [t("topBar:otherAccessWarning"), t("topBar:otherAccessSource", { source }),
          t("topBar:otherAccessRole", { role: sharePermissionLabel(other.role, t) })].join("\n"),
        confirmLabel: t("topBar:removeConfirm"), danger: true,
      });
      if (!remove) return;
    }
    void runMutation({ action: "remove_permission", permissionId: permission.id }, t("topBar:toast.removedFromAccessList"));
  }

  const { changeInherited, changeTeamspaceRole, changeInheritedWeb, inheritedWebRole, relinkSharing, changeInheritedWorkspace,
    lowersInheritedWorkspace } = useShareInheritanceActions({ stateRef, runMutation, t });
  // A change's words go under the row it was made on (a person's, or the
  // general access row), not to the window's end.
  async function changeAt<T>(where: string, change: () => Promise<T>) {
    errorAtRef.current = where;
    try {
      return await change();
    } finally {
      errorAtRef.current = "";
    }
  }

  function updatePermission(permission: PublicMutationPermission, role: PublicMutationRole) {
    void changeAt(`permission:${permission.id}`, () => runMutation({ action: "update_permission", permissionId: permission.id, role }));
  }

  const updateGeneralAccess = (change: GeneralAccessChange) => changeAt("general", () => changeGeneralAccess(change));

  // The workspace's row and the web's are each the page's own, as Notion's
  // (seen 2026-11-08): choosing one adds it and leaves the other as set, a
  // row's "제거" takes it alone off and "초대받은 사용자만" takes both off.
  async function changeGeneralAccess(change: GeneralAccessChange) {
    if (change === "web") return await runMutation({ action: "set_web_sharing", enabled: true,
      expiresAt: stateRef.current?.shareLink?.expiresAt ?? "" }) === "succeeded";
    if (change === "workspace") return await runMutation({ action: "grant_permission", principalType: "workspace",
      principalId: stateRef.current?.workspaceId ?? "", role: stateRef.current?.workspacePermission?.role ?? publicMutationRoles.fullAccess }) === "succeeded";
    // Everyone in the workspace from a page above goes first, once asked: it unlinks the page.
    if (change !== "noWeb" && lowersInheritedWorkspace() && !await changeInheritedWorkspace(null)) return false;
    if (change !== "noWorkspace") {
      if (inheritedWebRole() && !await changeInheritedWeb("none")) return false;
      if (stateRef.current?.webSharingEnabled && await runMutation({ action: "set_web_sharing", enabled: false }) !== "succeeded") return false;
    }
    if (change === "noWeb") return true;
    const permission = stateRef.current?.workspacePermission;
    return !permission || await runMutation({ action: "remove_permission", permissionId: permission.id }) === "succeeded";
  }

  async function updateExpiry(expiresAt: string) {
    const inherited = inheritedWebRole();
    if (inherited && !await changeInheritedWeb(inherited as WebLinkRole)) return;
    await runMutation({
      action: "set_web_sharing",
      enabled: true,
      expiresAt,
    });
  }

  async function loadMorePrincipalOptions() {
    const principalCursor = state?.principalOptionsNextCursor;
    if (!principalCursor || busy) return;
    const controller = new AbortController();
    operationRef.current?.abort();
    operationRef.current = controller;
    const current = state;
    const pageID = page.id;
    const userID = session.user.id;
    setBusy(true);
    try {
      const result = await publicMutationService.principalWindow(
        session,
        page.id,
        principalCursor,
        current.revision,
        controller.signal,
      );
      if (operationRef.current !== controller || controller.signal.aborted) return;
      if (page.id !== pageID || session.user.id !== userID) return;
      setState({
        ...current,
        principalOptions: [...current.principalOptions, ...result.state.principalOptions],
        principalOptionsNextCursor: result.state.principalOptionsNextCursor,
      });
      invite.focusField();
    } catch (error: unknown) {
      if (operationRef.current !== controller || controller.signal.aborted) return;
      setErrorCode(shareErrorText(error, t));
    } finally {
      if (operationRef.current === controller) {
        operationRef.current = null;
        setBusy(false);
        keepFocusInDialog();
      }
    }
  }

  // Drawn from what was last heard, it answers at once; a change the server's newer sharing refuses is read again (runMutation).
  const controlsDisabled = busy || !state;
  const webLink = typeof window === "undefined" ? "" : publicHref(state);
  const { phone } = useSidebarLayout(true);
  const fill = publishSheetFill(phone, activeShareTab === "publish");
  const invite = useShareInviteDraft({ session, pageId: page.id, pageTop: page.parentId === "" || page.parentId === null, initialInvite, state, stateRef, busy, setBusy, setErrorCode, runMutation,
    onStatus, toast, phone, inputRef: inviteInputRef, t });

  const [returnFocusRef] = useState<RefObject<HTMLElement | null>>(() => { const asker = takePageShareOpener(page.id); return asker ? { current: asker } : anchorRef; });
  const [question] = useState(questionOverShare);
  useFocusBackFromQuestion(question, dialogRef);
  const placement = usePopoverPlacement(dialogRef, (size, viewport) => placeAround(
    popoverRectOf(anchorRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 8 },
  ), [page.id], undefined, { kind: "dialog", ...shareLayerUnder(question, close), modalOwned: () => [backdropRef.current],
    returnFocusRef, restoreFocus: true, onEscape: close });
  const { dialogProps } = useDialog({ "aria-label": t("topBar:shareDialog", { pageTitle: pageTitle(page, untitled) }) }, dialogRef);

  const tabs = (
      <Tabs selectedKey={activeShareTab} keyboardActivation="manual" className={fill.tabs}
        onSelectionChange={key => { setActiveShareTab(key as ShareTab); setErrorCode(""); }}>
        <TabList className={styles.shareTabs} data-share-tabs="true" aria-label={t("topBar:share")}
          style={((invite.inviting && !phone) || advanced) && activeShareTab === "share" ? { display: "none" } : undefined}>
          {(["share", "publish"] as ShareTab[]).map(tab => <Tab key={tab} id={tab} className={styles.shareTab}
            data-active={activeShareTab === tab ? "true" : undefined}>{t(tab === "share" ? "topBar:share" : "topBar:publish")}
            {/* A published page's "게시" carries a blue dot, as Notion's (seen 2026-11-05). */}
            {tab === "publish" && state?.site?.published ? <span className={styles.shareTabDot} aria-hidden="true" /> : null}</Tab>)}
        </TabList>
        <TabPanel id="share">
          <TopBarShareAccessPanel state={state} currentUser={session.user} disabled={controlsDisabled} busy={busy} visible={Boolean(placement)} policyRefused={policyRefused} loading={loading} inTrash={state?.inTrash === true && !restoring}
            dialogRef={dialogRef} {...invite.panel} onMore={() => void loadMorePrincipalOptions()} onPermission={updatePermission}
            onRemove={permission => void removePermission(permission)}
            onGeneralAccess={updateGeneralAccess} writeNote={{ at: errorAt, text: errorCode }} onWorkspaceRole={role => void changeAt<unknown>("general", () =>
              lowersInheritedWorkspace() && shareRoleRank(role) < shareRoleRank(state?.workspaceInheritedRole) ? changeInheritedWorkspace(role)
              : runMutation({ action: "grant_permission", principalType: "workspace", principalId: state?.workspaceId ?? "", role }))}
            onHideFromSearch={hideFromSearch => void changeAt<unknown>("general", () => lowersInheritedWorkspace()
              ? changeInheritedWorkspace((state?.workspaceInheritedRole || "view") as PublicMutationRole, hideFromSearch) : runMutation({ action: "grant_permission",
              principalType: "workspace", principalId: state?.workspaceId ?? "", role: state?.workspacePermission?.role ?? "view", hideFromSearch }))}
            expiresAt={state?.shareLink?.expiresAt ?? ""} onExpiry={next => void changeAt("general", () => updateExpiry(next))}
            onWebRole={role => void changeAt<unknown>("general", () => inheritedWebRole() ? changeInheritedWeb(role) : runMutation({ action: "set_web_sharing",
              enabled: true, expiresAt: stateRef.current?.shareLink?.expiresAt ?? "", role }))}
            advanced={advanced} onAdvanced={setAdvanced} loadApps={loadApps} database={page.kind === "database"}
            onAgentGrantScope={scope => void runMutation({ agentGrantScope: scope })} pageRules={pageRules}
            propertyRules={PropertyRules && page.kind === "database" ? <PropertyRules session={session} workspaceId={page.workspaceId}
              pageId={page.id} disabled={controlsDisabled} onCountChange={pageRules.countChanged} /> : null}
            onCopy={() => void (webLink ? copyLink("web", webLink) : copyLink("page", (pageUrl ?? absolutePageUrl(page.id))))}
            editRequest={canRequestEdit ? <TopBarEditRequest page={page} session={session} onStatus={onStatus} className={access.readStripRequest} /> : null}
            pageTitle={pageTitle(page, untitled)} onInheritedChange={(person, role) => void changeInherited(person, role)}
            onTeamspaceRole={(group, role) => void changeTeamspaceRole(group, role)}
            pageIcon={pageId => { const above = pages.find(candidate => candidate.id === pageId);
              return <PageIconGlyph page={{ icon: above?.icon ?? "", iconType: above?.iconType ?? "none", kind: above?.kind,
                hasContent: above?.hasContent, workspaceId: above?.workspaceId }} size={18} />; }}
            onParentDefaults={href => { close(); pushRoute(href); }} onRelink={() => void relinkSharing()}
            loadInheritedPeople={pageAboveId => publicMutationService.inheritedPeople(session, page.id, pageAboveId)} />
          {/* Said to whoever hands the link on: a reader can't change it, and has it from someone who could open it. */}
          {state?.canManage ? <LocalAddressHint session={session} address={webLink || (pageUrl ?? absolutePageUrl(page.id))} onLeave={close}
            className={phone ? `${styles.shareSecurityNote} ${sharePhoneStyles.note}` : styles.shareSecurityNote} /> : null}
          {!invite.guestRequest && !policyRefused && !errorAt ? <ShareErrorLine text={errorCode} className={styles.shareError}>
            {!state && !loading ? <> <button type="button" className={access.readStripRequest} data-share-load-retry
              onClick={() => setLoadAttempt(value => value + 1)}>{t("common:retry")}</button></> : null}</ShareErrorLine> : null}
          {invite.guestNote}
        </TabPanel>
        <TabPanel id="publish" className={fill.panel}>
          <TopBarSharePublishTab site={site} state={state} session={session} page={page} pages={pages} untitled={untitled} busy={busy}
            controlsDisabled={controlsDisabled} copied={copied === "site"} errorCode={errorCode} setErrorCode={setErrorCode}
            onCopy={siteLink => void copyLink("site", siteLink)} runMutation={runMutation} inTrash={state?.inTrash === true && !restoring}
            onRestore={onRestore ? () => { setRestoring(true); void onRestore().then(ok => ok ? setLoadAttempt(value => value + 1) : setRestoring(false)); } : undefined}
            hydrate={next => hydrate(next)} close={close} closeNow={closeNow} t={t} />
        </TabPanel>
      </Tabs>
  );
  // A phone's share is Notion's "공유 설정" sheet (seen 2026-10-27), "완료" at its right.
  if (phone) return (
    <PhoneSheet title={t("topBar:shareSettings")} label={t("topBar:shareDialog", { pageTitle: pageTitle(page, untitled) })} sheetRef={dialogRef}
      headEven onClose={close} action={{ label: t("common:actions.done"), onPress: close }} onKeyDown={routeOverlayKeyDown} bodyClassName={fill.body}
      data={{ "data-share-menu": "true", "data-share-phone-sheet": "", "data-access-loaded": state ? "true" : "false" }}>
      {/* A failure belongs to the tab it happened on. */}
      <div className={sharePhoneStyles.sheet}>{tabs}</div>
    </PhoneSheet>
  );
  return createPortal(<>
    <button ref={backdropRef} type="button" className={styles.menuBackdrop} style={{ ...popoverBackdropStyle(placement), ...underQuestion(question, 2) }}
      tabIndex={-1} aria-label={t("topBar:closePageMenu")} onClick={close} />
    <div {...dialogProps} id={id} ref={dialogRef} className={styles.shareMenu} aria-modal="true"
      style={{ ...popoverPlacementStyle(placement), ...underQuestion(question, 1), position: "fixed", insetInlineEnd: "auto", visibility: placement ? "visible" : "hidden" }}
      data-share-menu="true" data-access-loaded={state ? "true" : "false"} onKeyDown={routeOverlayKeyDown}>
      {/* A failure belongs to the tab it happened on. */}
      {tabs}
    </div>
  </>, document.body);
}
