"use client";

import { type ReactElement, type ReactNode, useId, useLayoutEffect, useRef, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { i18next } from "@/i18n";
import { focusWhenFree, lockedOpener } from "@/lib/focusThroughWaits";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { Trash, X } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import styles from "./ConfirmActionDialog.module.css";
import { enterBeingPressed, focusedOpener } from "@/lib/lastPress";

export type ConfirmActionRequest = {
  message: string;
  /** A smaller line under the message, as Notion's database trash asks. */
  detail?: ReactNode;
  confirmLabel: string;
  /** null leaves only the action, for a notice that is acknowledged. */
  cancelLabel?: string | null;
  /** A destructive action in red, each card as Notion's draws it (seen
   *  2026-11-08): true is red words on a red outline (a property's, a view's
   *  or an option's delete, a page deleted for good); "filled" is light words
   *  (redTexInvPri) on solid red (a database or data source to the trash, a
   *  template's, a comment's or an automation's delete, leaving an
   *  automation unsaved, a workspace emoji's delete); "filled-white" is the
   *  same fill under white words, where Notion writes them white (its
   *  warning before a page is copied to another workspace, seen 2026-11-24).
   *  The only place a card's red look is chosen. */
  danger?: boolean | "filled" | "filled-white";
  /** A mark above the message, 36px unless its card draws it 32, 10px over the question, as Notion's
   *  before a variable's formula turns to words (seen 2026-11-21). */
  icon?: ReactNode;
  /** Notion's question before unsaved changes go — an automation's, a
   *  template repeat's (seen 2026-11-21 at 1440: 324 wide, a grey 32px bin
   *  10 over the question; 36 on a phone, seen 2026-10-24) — its bin drawn
   *  here and its action filled red. */
  discard?: boolean;
  /** Paragraphs in a gray box under the message, in a wider card, as
   *  Notion's warning before a page is copied to another workspace. */
  notes?: readonly string[];
  /** A card wide enough to keep its gray line on one line, its words
   *  centered, as Notion's settings ask before deleting a workspace emoji
   *  (seen 2026-11-07). */
  wide?: boolean;
  /** A 406px card for a longer question, its words still centered; with an
   *  `icon`, a 32px mark over the question 17/22 and its line 14/20, as
   *  Notion's developer tools ask before a webhook subscription goes (seen
   *  2026-11-21). */
  roomy?: boolean;
  /** A 406px card whose action is blue over a plain words-only other
   *  answer, as Notion's new form asks (seen 2026-11-02); the action that
   *  takes focus is the blue one, a "×" before it or not. */
  primary?: boolean;
  /** False for a question that asks on its own, not from a press, leaving no
   *  focus behind when answered, as Notion's new form (seen 2026-11-06). */
  returnFocus?: boolean;
  /** Where the focus goes back as it is answered, when that is not where the
   *  focus is as it opens: a menu's "…" its closing gives the focus only
   *  once painted (a comment's delete question); unsaid, the focus's place. */
  opener?: HTMLElement | null;
  /** Escape leaves it up, its action ringed as the keys now lead; an answer
   *  or the dimmed page closes it, as Notion's new form asks (seen
   *  2026-11-25 in two tabs: Escape twice kept it, the page around closed it). */
  staysOnEscape?: boolean;
  /** A 32px mark and the line under the question in 12px, as Notion's
   *  asks before unlinking a page's sharing (seen 2026-11-06). */
  compact?: boolean;
  /** The question 17/22 and its line 14/20 8px under it, as Notion's asks
   *  before a connection leaves a page (seen 2026-11-21); with `compact`,
   *  its line 12/16 600 under the mark. */
  titleSm?: boolean;
  /** Its action blue and filled over the outlined other, in the 324px card,
   *  as Notion's asks before relinking a page's sharing (seen 2026-11-06). */
  accent?: boolean;
  /** What the action changes, in a lined box under the question, as
   *  Notion's relink lists each person's role before and after. */
  box?: ReactNode;
  /** Notion's question before a page moves here from another relation (seen
   *  2026-11-07): 300px, a 32px mark, the question 17/22 500, the detail
   *  14/20 grey 20px under it, a round "×" at the top right. With
   *  `primary`, Notion's question before a data source moves (seen
   *  2026-11-11): 380px, the question 600, its blue action over the other. */
  move?: boolean;
  /** A "?" after the detail that tells more under the pointer. */
  hint?: string;
  /** Notion's developer tools' questions (seen 2026-11-21, a connection's
   *  delete; its tokens' refresh with answers to choose, 480px): 440px, a
   *  32px mark, the question 17/22, its detail 14/20 grey and a round "×"
   *  at the top right (developers/DeveloperQuestions.tsx). */
  large?: boolean;
  /** A line over the answers to choose, at their start, as Notion's line
   *  asking when to revoke the old token (seen 2026-11-21). */
  choicesLabel?: string;
  /** The round "×" takes the focus as it opens, not the action, as Notion's
   *  token refresh question (seen 2026-11-21 in two tabs, the "×" focused
   *  without its ring); a card without the "×" focuses its action. */
  closeFirst?: boolean;
  /** "□ 다시 묻지 않기" under the answers: ticked and answered yes, the
   *  question under this key is not asked again on this device. */
  rememberKey?: string;
  /** Answers to choose before a delete, as Notion's two-way relation asks
   *  (seen 2026-11-11): 400px, a 32px bin, the question 17/22 500, the
   *  chosen answer's ring and radio red, a round "×" at the top right. */
  dangerChoices?: boolean;
  /** Words of the message drawn bold, as Notion's names the database. */
  emphasis?: string;
  /** The question 18/27, as Notion's asks before a form view is deleted
   *  (seen 2026-11-12); its other questions, a view's delete too, 16/24. */
  headline?: boolean;
  /** Tab and ⇧Tab leave the focus on the action, ringed, never on "취소", as
   *  Notion's question before a comment goes (seen 2026-11-26 in two tabs,
   *  three Tabs after it opened, a letter after: "삭제" kept the focus each
   *  time); its other questions, a property's delete among them, go round
   *  their answers. */
  tabKeepsAction?: boolean;
};

const REMEMBERED = "leafen:confirm-remembered:";

function remembered(key: string) {
  try { return window.localStorage.getItem(REMEMBERED + key) === "1"; } catch { return false; }
}

function remember(key: string) {
  try { window.localStorage.setItem(REMEMBERED + key, "1"); } catch { /* asked again next time */ }
}

/** One of the answers a confirmation offers, as a card with a radio. */
export type ConfirmChoice = { value: string; label: string; description?: string };

let host: HTMLDivElement | null = null;
let root: Root | null = null;
let cancelOpen: (() => void) | null = null;

/**
 * Asks in the app, as Notion's confirmations do (seen 2026-10-06: a property
 * delete asks in a small card in the middle of the window, its action above
 * "취소"), rather than in the browser's own window, which some browsers hold
 * back. Resolves true only when the action is chosen; Escape, the backdrop
 * and "취소" resolve false, and focus goes back where it was.
 *
 * What asks: an action that at once ends another person's, agent's or app's
 * access, or a token, session or link, and cannot be undone in one step, as
 * Notion asks before removing a member (seen 2026-10-08).
 */
export function confirmAction(request: ConfirmActionRequest & Pick<ConfirmCardOptions, "markers">): Promise<boolean> {
  if (request.rememberKey && remembered(request.rememberKey)) return Promise.resolve(true);
  return openConfirm(false, (answer) => <ConfirmActionDialog {...request} onAnswer={(value) => answer(value !== null)} />, request.returnFocus !== false, request.opener);
}

/**
 * Asks as Notion asks before turning a feature off (seen 2026-10-10): a
 * title, the answers as cards with a radio, the first chosen, then the
 * action above "취소". Resolves the chosen answer, or null when cancelled.
 */
export function confirmChoice(request: ConfirmActionRequest & { choices: readonly ConfirmChoice[] }): Promise<string | null> {
  return openConfirm<string | null>(null, (answer) => <ConfirmActionDialog {...request} onAnswer={answer} />);
}

/**
 * Asks with several actions stacked above "취소", as Notion asks how far a
 * default template reaches (seen 2026-11-03: "모든 보기에 적용", "이 보기에만
 * 적용"). Resolves the chosen action's value, or null when cancelled.
 */
export function confirmActions(request: Omit<ConfirmActionRequest, "confirmLabel"> & { actions: readonly { value: string; label: ReactNode; danger?: boolean }[] }): Promise<string | null> {
  return openConfirm<string | null>(null, (answer) => <ConfirmActionDialog {...request} confirmLabel="" onAnswer={answer} />);
}

function openConfirm<T>(cancelled: T, render: (answer: (value: T) => void) => ReactElement, returnFocus = true, given?: HTMLElement | null): Promise<T> {
  if (typeof document === "undefined") return Promise.resolve(cancelled);
  cancelOpen?.();
  // Where the focus was as it opens: a control its request locked while it
  // asks (a refusal of what Enter in a field or a press of a button sent)
  // still counts as holding it, as Notion's notices give it back there (seen
  // 2026-11-16: "그룹이 이미 존재합니다." answered, the field after Enter,
  // the button after its press).
  const opener = returnFocus ? (given?.isConnected ? given : null) ?? lockedOpener() ?? focusedOpener() : null;
  if (!host || !host.isConnected) {
    host = document.createElement("div");
    host.dataset.confirmActionHost = "";
    // A confirmation is drawn over everything, so nothing that hides the page
    // outside a window keeps it hidden (2026-10-28: a combobox open in the
    // share window left this host inert, and its "변경 내용을 삭제할까요?"
    // could be neither focused nor pressed). Aria keeps top layers visible.
    host.dataset.reactAriaTopLayer = "";
    document.body.append(host);
    root = createRoot(host);
  }
  return new Promise((resolve) => {
    let open = true;
    const answer = (value: T) => {
      if (!open) return;
      open = false;
      if (cancelOpen === cancel) cancelOpen = null;
      root?.render(null);
      // Unless the answer's caller moved it on (the invite field kept), focus
      // goes back to the opener — once it is free, where the answer's action
      // locked it (2026-10-28: a server user's row went dim and the focus
      // stayed on the page).
      if (opener?.isConnected) window.requestAnimationFrame(() => {
        const active = document.activeElement;
        if (!active || active === document.body) focusWhenFree(opener);
      });
      resolve(value);
    };
    const cancel = () => answer(cancelled);
    cancelOpen = cancel;
    root?.render(render(answer));
  });
}

type DataMarks = Record<`data-${string}`, string>;

/** The message with its first use of the words drawn bold. */
function emphasized(message: string, emphasis: string | undefined): ReactNode {
  const at = emphasis ? message.indexOf(emphasis) : -1;
  if (!emphasis || at < 0) return message;
  return <>{message.slice(0, at)}<strong>{emphasis}</strong>{message.slice(at + emphasis.length)}</>;
}

type ConfirmCardOptions = {
  /** Data attributes on the card and its answers, for those who find them. */
  markers?: { dialog?: DataMarks; confirm?: DataMarks; cancel?: DataMarks };
  /** Both answers wait while the chosen one is under way. */
  busy?: boolean;
  /** Only an answer closes it: Escape and the dimmed page do nothing. */
  required?: boolean;
};

/**
 * The confirmation card itself, for a flow that keeps its own state and asks
 * in place (the sidebar's tab delete, a property's type change, sign-out
 * with unsynced changes): one part, so every confirmation looks and answers
 * as Notion's (2026-10-17: six were drawn apart).
 */
export function ConfirmCard(props: ConfirmActionRequest & ConfirmCardOptions & {
  onAnswer(answer: string | null): void;
}) {
  return <ConfirmActionDialog {...props} />;
}

function ConfirmActionDialog({ message, detail, confirmLabel, cancelLabel, danger, icon: ownIcon, discard, notes, wide, roomy, primary, compact, titleSm, accent, box, move, hint, large, choicesLabel, closeFirst, rememberKey, dangerChoices, emphasis, headline, staysOnEscape, tabKeepsAction, choices, actions, onAnswer, markers, busy, required }: ConfirmActionRequest & ConfirmCardOptions & {
  choices?: readonly ConfirmChoice[];
  /** Actions in place of the one confirm, each answering its value. */
  actions?: readonly { value: string; label: ReactNode; danger?: boolean }[];
  /** The chosen answer ("" without choices), or null when cancelled. */
  onAnswer(answer: string | null): void;
}) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const actionRef = useRef<HTMLButtonElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const messageId = useId();
  const detailId = useId();
  const [chosen, setChosen] = useState(choices?.[0]?.value ?? "");
  const [keep, setKeep] = useState(false);
  const cancel = cancelLabel === null ? null : cancelLabel ?? i18next.t("common:actions.cancel");
  const icon = discard ? <Trash size="large" aria-hidden="true" /> : ownIcon;
  const red = discard ? "filled" : danger;
  // The action takes focus, as Notion's confirmations do even before a
  // permanent delete (seen 2026-10-28: a database to the trash, a page
  // deleted for good, a comment, discarding a share's changes) — or the
  // "×" where the request asks it to (closeFirst).
  const closes = move || large || dangerChoices;
  // An Enter that asks — a field's Enter whose answer is a notice (a cover's
  // link that is no image's) — doesn't answer it too: its press would go on
  // to the action the card has just focused, and the card would close before
  // it showed (2026-11-18: the notice that the link is no image's never
  // stayed after the link field's Enter, though it did after "제출"). Every
  // confirmation is this card, asked by confirmAction, confirmChoice,
  // confirmActions or drawn in place (ConfirmCard), so none is answered by
  // the key that opened it, as a window closed by Enter presses nothing
  // behind it (overlayLayer.ts).
  useLayoutEffect(() => { enterBeingPressed()?.preventDefault(); }, []);
  const { zIndex } = useOverlayLayer(true, {
    kind: "alertdialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], restoreFocus: false,
    onEscape: () => { if (!required && !staysOnEscape) onAnswer(null); }, initialFocus: closeFirst && closes ? closeRef : actionRef,
  });
  return (
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={cancel ?? confirmLabel}
        onClick={() => { if (!required) onAnswer(null); }} />
      <div ref={dialogRef} className={styles.dialog} role="alertdialog" aria-modal="true" aria-labelledby={messageId}
        aria-describedby={detail ? detailId : undefined}
        onKeyDown={tabKeepsAction ? (event) => {
          if (event.key !== "Tab" || event.altKey || event.ctrlKey || event.metaKey) return;
          // Taken before the window's Tab ring (overlayLayer), which leaves a prevented key.
          event.preventDefault();
          actionRef.current?.focus({ preventScroll: true, focusVisible: true } as FocusOptions);
        } : undefined}
        data-confirm-action-dialog data-choices={choices ? "true" : undefined} data-notes={notes ? "true" : undefined} data-wide={wide ? "true" : undefined} data-roomy={roomy ? "true" : undefined} data-primary={primary ? "true" : undefined} data-compact={compact ? "true" : undefined} data-title-sm={titleSm ? "true" : undefined} data-accent={accent ? "true" : undefined} data-move={move ? "true" : undefined} data-large={large ? "true" : undefined} data-danger-choices={dangerChoices ? "true" : undefined} data-headline={headline ? "true" : undefined} data-discard={discard ? "true" : undefined} {...markers?.dialog}>
        {closes ? <button ref={closeRef} type="button" className={styles.close} aria-label={i18next.t("common:actions.close")} onClick={() => onAnswer(null)}>
          <X size="small" aria-hidden="true" /></button> : null}
        {icon ? <span className={styles.icon} aria-hidden="true">{icon}</span> : null}
        <p className={styles.message}><span id={messageId}>{emphasized(message, emphasis)}</span>{detail ? <span id={detailId} className={styles.detail}>{detail}
          {hint ? <> <button type="button" className={styles.hint} aria-label={hint} title={hint}><QuestionCircleIcon size="small" aria-hidden="true" /></button></> : null}
        </span> : null}</p>
        {box ? <div className={styles.box}>{box}</div> : null}
        {notes ? <div className={styles.notes}>{notes.map((note) => <p key={note}>{note}</p>)}</div> : null}
        {choicesLabel ? <p className={styles.choicesLabel}>{choicesLabel}</p> : null}
        {choices ? <div role="radiogroup" aria-labelledby={messageId} className={styles.choices}>
          {choices.map((choice) => <label key={choice.value} className={styles.choice} data-chosen={chosen === choice.value ? "true" : undefined}>
            <span>
              <strong>{choice.label}</strong>
              {choice.description ? <small>{choice.description}</small> : null}
            </span>
            <input type="radio" name={messageId} value={choice.value} checked={chosen === choice.value}
              onChange={() => setChosen(choice.value)} />
          </label>)}
        </div> : null}
        {actions ? actions.map((action, index) => (
          <button key={action.value} ref={index === 0 ? actionRef : undefined} type="button" className={styles.action} data-lead={index === 0 ? "true" : undefined} data-danger={action.danger ? "true" : undefined}
            disabled={busy} data-confirm-action-choice={action.value} onClick={() => onAnswer(action.value)}>{action.label}</button>
        )) : <button ref={actionRef} type="button" className={styles.action} data-lead="true" data-danger={typeof red === "string" ? red : red ? "true" : undefined}
          disabled={busy} {...markers?.confirm} onClick={() => { if (keep && rememberKey) remember(rememberKey); onAnswer(chosen); }}>{confirmLabel}</button>}
        {cancel === null ? null : <button type="button" className={styles.action}
          disabled={busy} {...markers?.cancel} onClick={() => onAnswer(null)}>{cancel}</button>}
        {rememberKey ? <label className={styles.remember}>
          <input type="checkbox" checked={keep} onChange={(event) => setKeep(event.target.checked)} />{i18next.t("common:dontAskAgain")}
        </label> : null}
      </div>
    </div>
  );
}
