"use client";

import { useEffect, useMemo, useState } from "react";
import { useInnerHtml } from "@/lib/innerHtml";
import type { TextSpan } from "@/lib/types";
import { markedCodeWriter, prismHighlightHtml } from "./codeHighlightPrismHtml";
import { loadedPrismGrammar, loadPrismGrammar, prismGrammarId } from "./codeHighlightPrismGrammars";
import { assertRichtextHtmlTextBound } from "./richtextHtmlEscaping";
import "./codeTheme.css";

/** A Prism grammar's id once it is loaded, loading it the first time it is asked for. */
function usePrismGrammar(id: string | null) {
  const [, setLoaded] = useState(0);
  const ready = id ? loadedPrismGrammar(id) !== null : false;
  useEffect(() => {
    if (!id || ready) return;
    let live = true;
    // A grammar that fails to load leaves the code as text.
    loadPrismGrammar(id).then(() => { if (live) setLoaded((count) => count + 1); }, () => undefined);
    return () => { live = false; };
  }, [id, ready]);
  return ready ? id : null;
}

/** A code block's source colored as Notion's: by the Prism grammar of its language, once loaded. */
export function CodeHighlight({ code, spans, language }: { code: string; spans?: readonly TextSpan[]; language: string }) {
  const prism = usePrismGrammar(prismGrammarId(language));
  const html = useMemo(() => {
    assertRichtextHtmlTextBound(code);
    const grammar = prism ? loadedPrismGrammar(prism) : null;
    const marks = spans ?? [{ text: code }];
    return grammar ? prismHighlightHtml(code, grammar, marks) : markedCodeWriter(marks)(code);
  }, [code, spans, prism]);
  const inner = useInnerHtml(html || "&#8203;");

  return (
    <code
      className={prism ? `code-highlight language-${prism}` : "code-highlight"}
      // Highlighted HTML is produced locally from the user's own code text
      // (escaped); no untrusted markup is injected.
      dangerouslySetInnerHTML={inner}
    />
  );
}
