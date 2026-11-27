import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import styles from "./editorStyles.generated.module.css";
import { withLoadDeadline } from "@/lib/lazyScreen";
import { HtmlDiv } from "@/lib/innerHtml";

let mermaidModule: typeof import("mermaid").default | null = null;
let mermaidInitTheme: "dark" | "neutral" | null = null;
// Notion's diagrams use Mermaid's grey "neutral" theme in its own type, a
// node's words in a 120px box and 40px between ranks (seen 2026-10-31;
// EditorNotationBlocks.module.css), laid out by dagre in the classic look:
// Mermaid 12 lays out unconfigured diagrams by ELK in its "neo" look, so
// both are named here (a diagram's own front matter still chooses its own).
const MERMAID_FONT = "ui-sans-serif, -apple-system, BlinkMacSystemFont, \"Segoe UI\", Helvetica, Arial, sans-serif";
const MERMAID_SOURCE_MAX_TEXT_CHARS = 20_000_000;
// The code is drawn after a line of settings, as Notion's: what it can't draw
// counts the code's lines from 2 and quotes it from a new line (seen
// 2026-11-15: "Parse error on line 4" for a 3-line code, "for text: ⏎x").
const MERMAID_LEAD = "%%{init: {}}%%\n";

function assertMermaidSourceBound(source: string) {
  if (source.length > MERMAID_SOURCE_MAX_TEXT_CHARS) {
    throw new Error("Mermaid source limit exceeded");
  }
}

async function ensureMermaid(theme: "dark" | "neutral") {
  if (!mermaidModule) {
    mermaidModule = (await withLoadDeadline(import("mermaid"))).default;
  }
  if (mermaidInitTheme !== theme) {
    mermaidModule.initialize({ startOnLoad: false, theme, layout: "dagre", look: "classic", securityLevel: "strict", fontFamily: MERMAID_FONT,
      flowchart: { wrappingWidth: 120, rankSpacing: 40 } });
    mermaidInitTheme = theme;
  }
  return mermaidModule;
}

function currentMermaidTheme(): "dark" | "neutral" {
  if (typeof document === "undefined") return "neutral";
  return document.documentElement.dataset.theme === "dark" ? "dark" : "neutral";
}

export function MermaidPreview({ source, blockId, onBroken }: { source: string; blockId: string; onBroken?(broken: boolean): void }) {
  assertMermaidSourceBound(source);
  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => onBroken?.(error !== null), [error, onBroken]);
  const [theme, setTheme] = useState<"dark" | "neutral">(() => currentMermaidTheme());
  const renderId = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  const diagramLabelId = `${renderId}-label`;
  const diagramSourceId = `${renderId}-source`;
  const { t } = useTranslation(["mermaidPreview", "common"]);
  const trimmedSource = source.trim();

  useEffect(() => {
    const root = document.documentElement;
    const observer = new MutationObserver(() => setTheme(currentMermaidTheme()));
    observer.observe(root, { attributes: true, attributeFilter: ["data-theme"] });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    let cancelled = false;
    const trimmed = source.trim();
    const timer = window.setTimeout(() => {
      if (!trimmed) {
        if (cancelled) return;
        setSvg(null);
        setError(null);
        return;
      }
      const uid = `mermaid-${blockId}-${renderId}-${Date.now()}`;
      ensureMermaid(theme)
        .then((mermaid) => mermaid.render(uid, `${MERMAID_LEAD}${trimmed}`))
        .then(({ svg: out }) => {
          if (cancelled) return;
          setSvg(out);
          setError(null);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          document.getElementById(uid)?.remove();
          document.querySelector(`#d${uid}`)?.remove();
          setSvg(null);
          setError(err instanceof Error ? err.message : String(err));
        });
    }, 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [source, theme, blockId, renderId]);

  return (
    <div
      className={styles.mermaidPreview}
      contentEditable={false}
      aria-busy={!error && !svg && trimmedSource.length > 0}
    >
      {error ? (
        <div
          className={styles.mermaidError}
          role="alert"
          aria-live="assertive"
          aria-atomic="true"
        >
          {/* All of it in red under a rule below the code, as Notion's (seen 2026-11-05, 11-06). */}
          <pre className={styles.mermaidErrorRaw}><span className={styles.mermaidErrorTitle}>{t("mermaidPreview:syntaxError")}</span>{error}</pre>
        </div>
      ) : svg ? (
        <div
          className={styles.mermaidDiagram}
          role="img"
          aria-labelledby={diagramLabelId}
          aria-describedby={diagramSourceId}
        >
          <span id={diagramLabelId} className={styles.srOnly}>
            {t("mermaidPreview:diagramLabel")}
          </span>
          <span id={diagramSourceId} className={styles.srOnly}>
            {t("mermaidPreview:sourceDescription", { source: trimmedSource })}
          </span>
          <HtmlDiv aria-hidden="true" html={svg} />
        </div>
      ) : (
        <div className={styles.mermaidEmpty} role="status" aria-live="polite">
          {t("mermaidPreview:emptyPreview")}
        </div>
      )}
    </div>
  );
}
