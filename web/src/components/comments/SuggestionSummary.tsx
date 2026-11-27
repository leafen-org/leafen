import { Fragment, type ReactNode } from "react";
import editorStyles from "@/components/editor/editorStyles.generated.module.css";
import { SuggestEditIcon } from "@/icons/leafen";
import { suggestionSummaryParts, type SuggestionFacts } from "./suggestionModel";
import styles from "./SuggestionSummary.module.css";

/**
 * A suggestion's summary on its card, as Notion's margin and panel cards
 * draw it, open or closed (seen 2026-11-15, dark, twice): what it does in
 * the kind's faint color and the quoted text in its own — "추가:" and
 * "굵게:" in the tertiary blue before the text in blue, "삭제:" in the
 * tertiary gray before the text in the secondary gray — each part on a
 * line of its own (seen 2026-11-15 twice: a closed card shows the first);
 * a replacement's old words in the deletion's colors, its new ones in the
 * addition's (seen 2026-11-22).
 */
export function SuggestionSummary({ facts, inline = false }: {
  facts: SuggestionFacts;
  /** In one line, as the top bar's "토론" list writes it (SuggestionLine). */
  inline?: boolean;
}) {
  if (inline) return <SuggestionLine facts={facts} />;
  return suggestionSummaryParts(facts).map(({ label, quote, kind }, index) => (
    <span key={index} className={styles.part} data-suggestion-part>
      <span className={styles.label} data-kind={kind}>{label}</span>
      {quote ? <span className={styles.quote} data-kind={kind}>{quote}</span> : null}
    </span>
  ));
}

/**
 * A suggestion's summary in one line, as Notion's top bar "토론" list writes
 * it (seen 2026-11-22 twice at 1440 dpr 1, dark): its parts one after
 * another a space apart, no " | "; each label fainter than a card's — a
 * removal's gray, an addition's blue at 0.57 — and the quoted words marked
 * as the page marks them (EditorInlineMarks: blue over a blue line, or the
 * disabled gray struck through over a gray one) between quote marks in the
 * words' blue or a faint gray. A replacement reads "바꾸기: “golf”
 * 바꾼 내용:: “hotel”", its old words struck.
 */
function SuggestionLine({ facts }: { facts: SuggestionFacts }) {
  return (
    <span className={editorStyles.richMarks} data-suggestion-line="">
      {suggestionSummaryParts(facts).map(({ label, open, words, close, kind }, index) => (
        <Fragment key={index}>
          {index > 0 ? " " : null}
          <span className={styles.lineLabel} data-kind={kind}>{label}</span>
          {open || words || close ? <>
            <span className={styles.lineMark} data-kind={kind}>{open}</span>
            <span data-suggestion={kind}>{words}</span>
            <span className={styles.lineMark} data-kind={kind}>{close}</span>
          </> : null}
        </Fragment>
      ))}
    </span>
  );
}

/**
 * A suggestion's summary on an open card, beside the page or in "모든 토론",
 * with what follows it there (its state once settled); its parts' spacing
 * and the card's line height in SuggestionSummary.module.css.
 */
export function SuggestionCardSummary({ facts, listed, children }: {
  facts: SuggestionFacts;
  listed?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className={listed ? `${styles.box} ${styles.listed}` : styles.box} data-suggestion-kind={facts.kind}
      data-suggestion-state={facts.state ?? undefined}>
      <SuggestionSummary facts={facts} />
      {children}
    </div>
  );
}

/**
 * The face of a suggestion's author with Notion's mark on its corner: a 16px
 * round of the card's blue tint holding a 10px pencil, its corner at the
 * face's middle, on every suggestion's card (seen 2026-11-15, dark: a 1px
 * rgba(35, 131, 226, 0.14) ring over black, a 0.07 tint, the pencil
 * rgb(35, 131, 226)).
 */
export function SuggestionFace({ children }: { children: ReactNode }) {
  return (
    <span className={styles.face} data-suggestion-face>
      {children}
      <span className={styles.badge} aria-hidden="true"><SuggestEditIcon size="badge" /></span>
    </span>
  );
}

/** A face, with the suggestion's mark when its comment holds one. */
export function wrapSuggestionFace(suggestion: SuggestionFacts | null, face: ReactNode) {
  return suggestion ? <SuggestionFace>{face}</SuggestionFace> : face;
}
