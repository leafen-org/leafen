import type { ReactNode } from "react";
import { GuideCaret, GuideClick, GuideCursor, GuideWindowBar } from "./guide/GuideParts";
import { styles } from "./notionTokenGuideStyles";

/**
 * The words of Leafen's own Notion window the guide draws, read from the
 * window's catalog (importDialog) so the picture says what the screen says.
 */
export type LeafenImportWords = {
  windowTitle: string;
  windowLead: string;
  /** The window's steps by hand: connect, scope, import. */
  steps: string[];
  notionStepsTitle: string;
  openTokenPage: string;
  tokenLabel: string;
  tokenPlaceholder: string;
  saveToken: string;
  rootPickerTitle: string;
  rootSearch: string;
  rootKindPage: string;
  back: string;
  next: string;
};

/** Leafen's Notion window by hand: its head, its steps with `step` the one shown, its body and its foot. */
function LeafenWindow({ M, L, step, children, footer }: {
  M: Record<string, string>;
  L: LeafenImportWords;
  step: number;
  children: ReactNode;
  footer: ReactNode;
}) {
  return (
    <>
      <GuideWindowBar title={M.leafenTitle} />
      <div className={styles.leafenPanel}>
        <div className={styles.leafenWindowHead}>
          <strong>{L.windowTitle}</strong>
          <span>{L.windowLead}</span>
        </div>
        <div className={styles.leafenSteps}>
          {L.steps.map((label, index) => (
            <span key={label} data-active={index === step ? "true" : undefined}>
              <b>{index + 1}</b>
              {label}
            </span>
          ))}
        </div>
        <div className={styles.leafenBody}>{children}</div>
        <div className={styles.leafenFooter}>{footer}</div>
      </div>
    </>
  );
}

/**
 * The token step: Notion's steps (drawn as lines) under "Notion에서 할 일"
 * with "Notion 토큰 만들기" beside it, and the token's field. "open" presses
 * "Notion 토큰 만들기"; "paste" pastes the token and presses "토큰 저장".
 */
export function leafenTokenScreen(M: Record<string, string>, L: LeafenImportWords, moment: "open" | "paste") {
  const paste = moment === "paste";
  return (
    <LeafenWindow M={M} L={L} step={0}
      footer={
        <span className={paste ? `${styles.leafenPrimary} ${styles.leafenPrimaryWakes}` : `${styles.leafenPrimary} ${styles.leafenPrimaryOff}`}>
          {L.saveToken}
          {paste ? (
            <GuideCursor
              clickAt={2250}
              path={[
                { at: 300, x: -260, y: -10 },
                { at: 800, on: "tokenField", x: -40, y: 1 },
                { at: 1300, on: "tokenField", x: -40, y: 1 },
                { at: 2050, x: 0, y: 0 },
              ]}
            />
          ) : null}
        </span>
      }
    >
      <div className={styles.leafenCard}>
        <div className={styles.leafenCardHead}>
          <strong>{L.notionStepsTitle}</strong>
          <span className={styles.leafenButton}>
            {L.openTokenPage}
            {paste ? null : <GuideCursor path={[{ at: 500, x: -117, y: 113 }, { at: 1300, x: 0, y: 0 }]} clickAt={1600} />}
          </span>
        </div>
        <ol className={styles.leafenStepLines} aria-hidden="true">
          {["92%", "78%", "86%", "70%"].map((width) => (
            <li key={width}>
              <span className={styles.skeletonLine} style={{ width }} />
            </li>
          ))}
        </ol>
        <span className={styles.fieldLabel}>{L.tokenLabel}</span>
        <span className={`${styles.leafenInput} ${paste ? styles.leafenInputFocus : ""}`} data-guide-target="tokenField">
          {paste ? (
            <>
              <span className={styles.leafenTokenValue}>{M.tokenValue}</span>
              <GuideCaret />
              <GuideClick at={1000} />
            </>
          ) : (
            <span className={styles.leafenPlaceholder}>{L.tokenPlaceholder}</span>
          )}
        </span>
      </div>
    </LeafenWindow>
  );
}

/** The scope step: what the connection reaches at the top, a search, the page ticked. */
export function leafenScopeScreen(M: Record<string, string> & { pageNames: string[] }, L: LeafenImportWords) {
  return (
    <LeafenWindow M={M} L={L} step={1}
      footer={
        <>
          <span className={styles.leafenSecondary}>{L.back}</span>
          <span className={styles.leafenPrimary}>{L.next}</span>
        </>
      }
    >
      <strong className={styles.leafenListTitle}>{L.rootPickerTitle}</strong>
      <span className={styles.leafenInput}>
        <span className={styles.leafenPlaceholder}>{L.rootSearch}</span>
      </span>
      <span className={styles.leafenRootRow}>
        <i className={`${styles.checkbox} ${styles.checkboxTicks} ${styles.leafenCheckbox}`} aria-hidden="true">
          <GuideCursor path={[{ at: 500, x: -177, y: -73 }, { at: 1300, x: 0, y: 0 }]} clickAt={1750} />
        </i>
        <span className={styles.authText}>
          <strong>{M.pageNames[0]}</strong>
          <span>{L.rootKindPage}</span>
        </span>
      </span>
    </LeafenWindow>
  );
}
