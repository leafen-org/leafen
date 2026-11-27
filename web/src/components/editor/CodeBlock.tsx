import { Suspense, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import type { GraphBlock } from "@/contentGraph";
import { NotionSelect } from "@/components/database/NotionSelect";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import { yTextToRichText } from "@/lib/collaborationCrdtCodec";
import type { TextSpan } from "@/lib/types";
import { useCopyNotice } from "@/lib/copyNotice";
import { usePointedByMove } from "@/lib/pointerHover";
import { rememberCodeBlockLanguage } from "@/lib/blockDefaults";
import { CodeWrapIcon, Copy, DotsHorizontal } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import { codeLanguageOptions, PLAIN_TEXT_CODE_LANGUAGE } from "./codeLanguages";
import { openBlockToolMenu } from "./editorCommands";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusEditable, focusEditableNextFrame } from "./focus";
import { BlockCornerHandles } from "./BlockCornerHandles";
import { MermaidDownloadTool, MermaidExpandTool, MermaidFormatTool, MermaidLightbox, mermaidFormat } from "./MermaidBlockTools";
import { staticProjectionSpans } from "./nativeTiptapGraph";
import { NativeTiptapReactContext } from "./nativeTiptapReactContext";
import { followKeptDrag } from "./nativeTiptapWidgetSelection";
import styles from "./editorStyles.generated.module.css";

// Notion's language list: 240 wide, half the window high, 8px under its
// button (seen 2026-11-20: 361.5 in a 723 window; 480 seen 2026-10-29 in a
// taller one), whole: over its button where only that side has room, else
// under it moved up into the window, 4.5 from its top and foot (seen
// 2026-11-20 at 1440×723: a button at 314..338 put it at 346..707.5, at
// 374..398 over it at 4.5..366, at 373.5..397.5 under it moved to 357.5..719).
const codeLanguageMenu = () => ({ width: 240, maxHeight: (typeof window === "undefined" ? 900 : window.innerHeight) / 2, gap: 8, whole: 4.5 });

// Without its code the block's code stays plain, as it shows while that comes.
const CodeHighlight = lazyScreen(() => import("./CodeHighlight").then(({ CodeHighlight: View }) => ({ default: View })),
  { standIn: ({ code }) => <code className="code-highlight">{code || "\u200b"}</code> });
const MermaidPreview = lazyScreen(() => import("./MermaidPreview").then(({ MermaidPreview: View }) => ({ default: View })));

// The block's text as people type it: the collaborative text when the page is
// live, the stored text otherwise.
function useCodeText(adapter: NativeEditorGraphAdapter, block: GraphBlock) {
  const document = adapter.collaborationDocument;
  const stored = (): { text: string; spans: readonly TextSpan[] } => {
    const text = adapter.text(block);
    return { text, spans: staticProjectionSpans(block.content.rich) ?? [{ text }] };
  };
  const [code, setCode] = useState(stored);
  useEffect(() => {
    if (!document) {
      setCode(stored());
      return;
    }
    const shared = document.getText(BLOCK_TEXT_ROOT_PREFIX + block.id);
    const read = () => setCode({ text: shared.toString(), spans: yTextToRichText(shared) ?? [] });
    read();
    shared.observe(read);
    return () => shared.unobserve(read);
    // Read again for its own block's text, not for a new place among its
    // siblings, which every block after a line made or pasted takes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [adapter, block.id, block.content, document]);
  return code;
}

/** How many characters of the colored copy come before a press on it, or its end. */
function pressedOffset(copy: HTMLElement, x: number, y: number): number | "end" {
  const doc = copy.ownerDocument as Document & { caretPositionFromPoint?(x: number, y: number): { offsetNode: Node; offset: number } | null };
  const at = doc.caretPositionFromPoint?.(x, y) ?? null;
  const range = at ? null : doc.caretRangeFromPoint?.(x, y) ?? null;
  const node = at?.offsetNode ?? range?.startContainer, offset = at?.offset ?? range?.startOffset;
  if (!node || offset === undefined || !copy.contains(node)) return "end";
  const before = doc.createRange();
  before.setStart(copy, 0);
  before.setEnd(node, offset);
  return before.toString().replace(/\u200b/gu, "").length;
}

/**
 * A code block as Notion shows it (seen 2026-10-29): on hover one group of
 * its language, copy, word wrap and the block's menu, highlighting while the
 * block is not being edited, and a diagram preview for Mermaid. The caption
 * is the block's shared caption. Notion has no line numbers, so neither does
 * this (a stored `lineNumbers` is left alone and not drawn).
 */
export function CodeBlock({
  adapter,
  block,
  editable,
  engineFocused,
}: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  editable: ReactNode;
  engineFocused: boolean;
}) {
  const notice = useCopyNotice();
  const stored = typeof block.content.language === "string" ? block.content.language.trim() : "";
  const language = stored === PLAIN_TEXT_CODE_LANGUAGE ? "" : stored;
  const wrap = block.content.wrap === true;
  const { text: codeText, spans } = useCodeText(adapter, block);
  const isMermaid = language === "mermaid";
  const format = isMermaid ? mermaidFormat(block.content.previewFormat) : "code";
  const showsDiagram = isMermaid && format !== "code";
  // A diagram its code cannot draw has no picture to open, save or size: its tools and pills go, as Notion's (seen 2026-11-06).
  const [broken, setBroken] = useState(false);
  const drawn = showsDiagram && !broken;
  const showHighlight = !engineFocused && Boolean(language) && codeText.length > 0;
  const blockRef = useRef<HTMLDivElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  // Its bar and a diagram's side grips come with the pointer moving onto the
  // block, never with the caret in its code: a block "/코드" or "/mermaid"
  // makes under a resting pointer shows them only once the pointer moves.
  // Notion's, measured 2026-11-16 in Chrome at 1440, dark, with no script in
  // the page: the pointer resting where the block came up, no bar until it
  // moved (twice, in two tabs); the caret in a code or Mermaid block with the
  // pointer outside, no bar or grips; the pointer over it, both. As the page
  // scrolls it from a still pointer, :hover puts them away (the CSS).
  const [pointed, pointerHandlers] = usePointedByMove();
  const engine = useContext(NativeTiptapReactContext);
  const [expanded, setExpanded] = useState(false);
  // A diagram's block is sized by its side pills, centered, as Notion's (seen 2026-11-05).
  const [draggedWidth, setDraggedWidth] = useState<number | null>(null);
  const storedWidth = typeof block.content.width === "number" ? Math.max(1, Math.min(100, block.content.width)) : undefined;
  const width = showsDiagram ? draggedWidth ?? (storedWidth && storedWidth < 100 ? storedWidth : undefined) : undefined;

  // A language chosen while the caret was in the code gives the caret back
  // where it was, as Notion's (seen 2026-11-06); chosen from outside, the
  // focus stays on the language, as Notion's.
  const caretBeforeLanguage = useRef<number | null>(null);
  function rememberCaret() {
    caretBeforeLanguage.current = null;
    const code = blockRef.current?.querySelector('[data-rt-editable="true"]');
    const caret = window.getSelection();
    if (!engineFocused || !code || !caret?.rangeCount || !caret.anchorNode || !code.contains(caret.anchorNode)) return;
    const before = document.createRange();
    before.selectNodeContents(code);
    before.setEnd(caret.anchorNode, caret.anchorOffset);
    caretBeforeLanguage.current = before.toString().length;
  }

  // The icon stays and a toast says "코드 복사 완료", as Notion's code block (seen 2026-11-07).
  function copyCode() {
    return notice(codeText, blockItemText("code.copied"));
  }

  return (
    <div
      ref={blockRef}
      className={styles.codeBlock}
      data-wrap={wrap ? "true" : undefined}
      data-mermaid-format-shown={isMermaid ? format : undefined}
      data-resizing={draggedWidth !== null ? "true" : undefined}
      data-pointed={pointed ? "true" : undefined} {...pointerHandlers}
      style={width ? { width: `${width}%`, marginInline: "auto" } : undefined}
    >
      <div className={styles.codeToolbar} contentEditable={false} role="toolbar" aria-label={blockItemText("code.toolbar")}>
        <span className={styles.codeLanguageHold} onPointerDownCapture={rememberCaret}>
        <NotionSelect
          className={styles.codeLanguageSelect}
          buttonClassName={styles.codeLanguageButton}
          backdropClassName={styles.editorSelectBackdrop}
          optionClassName={styles.codeLanguageOption}
          menuSize={codeLanguageMenu()}
          settingsList
          ariaLabel={blockItemText("code.language")}
          value={stored || PLAIN_TEXT_CODE_LANGUAGE}
          disabled={adapter.readOnly}
          options={codeLanguageOptions(blockItemText, stored)}
          search={{
            ariaLabel: blockItemText("code.searchLanguage"),
            placeholder: blockItemText("code.searchLanguage"),
            noResultsLabel: blockItemText("code.noLanguage"),
          }}
          onChange={(next) => {
            adapter.updateBlock(block, { language: next });
            rememberCodeBlockLanguage(next);
            const at = caretBeforeLanguage.current;
            caretBeforeLanguage.current = null;
            if (at !== null) focusEditableNextFrame(block.id, at);
          }}
        />
        </span>
        <span className={styles.codeToolbarDivider} aria-hidden="true" />
        {isMermaid ? <MermaidFormatTool format={format} buttonClassName={styles.codeOption} disabled={adapter.readOnly}
          onChange={(next) => adapter.updateBlock(block, { previewFormat: next })} /> : null}
        {drawn ? <>
          <MermaidExpandTool buttonClassName={styles.codeOption} onExpand={() => setExpanded(true)} />
          <MermaidDownloadTool buttonClassName={styles.codeOption} previewRef={previewRef} />
        </> : null}
        <button type="button" className={styles.codeOption} data-code-copy aria-label={blockItemText("code.copy")}
          data-icon-tip={blockItemText("code.tips.copy")} data-tip-side="top" onClick={() => void copyCode()}>
          <Copy size="small" aria-hidden="true" />
        </button>
        <button
          type="button"
          className={styles.codeOption}
          data-code-wrap
          aria-label={blockItemText("code.toggleWrap")}
          aria-pressed={wrap}
          data-active={wrap ? "true" : undefined}
          data-icon-tip={blockItemText("code.wrap")}
          data-tip-side="top"
          disabled={adapter.readOnly}
          onClick={() => adapter.updateBlock(block, { wrap: !wrap })}
        >
          <CodeWrapIcon size="small" aria-hidden="true" />
        </button>
        {adapter.readOnly ? null : (
          <button type="button" className={styles.codeOption} aria-label={blockItemText("code.blockMenu")}
            data-icon-tip={blockItemText("code.tips.more")} data-tip-side="top"
            onClick={(event) => openBlockToolMenu(block.id, event)}>
            <DotsHorizontal size="small" aria-hidden="true" />
          </button>
        )}
      </div>
      {/* Code isn't spell-checked, as Notion's isn't (seen 2026-10-31): its
          text and the colored copy over it sit in the editor's editable area. */}
      <div
        className={styles.codeBody}
        data-hidden={format === "preview" ? "true" : undefined}
        spellCheck={false}
        onMouseDown={(event) => {
          if (adapter.readOnly || (event.target as HTMLElement).closest('[data-rt-editable="true"]')) return;
          event.preventDefault();
          focusEditable(block.id, "end");
        }}
      >
        <div className={styles.codeStack}>
          <pre className={styles.code} data-hidden={showHighlight ? "true" : undefined}>{editable}</pre>
          {showHighlight ? (
            <pre
              className={`${styles.codeHighlighted} ${styles.richMarks}`}
              aria-hidden="true"
              data-code-highlight
              // A drag over the colored words chooses the code's, as Notion's (seen
              // 2026-11-07): it doesn't start the page's selection of blocks.
              onPointerDown={(event) => { if (event.button === 0) event.stopPropagation(); }}
              onMouseDown={(event) => {
                // Clicking the highlighted text resumes editing the block where it was
                // pressed, as Notion's code takes the caret (seen 2026-11-06), linked
                // words too, which code doesn't draw as links (codeHighlightPrismHtml.ts).
                event.preventDefault();
                // The body around would put the caret at the end after this.
                event.stopPropagation();
                const placed = focusEditable(block.id, pressedOffset(event.currentTarget, event.clientX, event.clientY));
                if (placed && event.button === 0 && engine) followKeptDrag(engine.controller.editor.view);
              }}
            >
              <Suspense fallback={<code className="code-highlight">{codeText || "​"}</code>}>
                <CodeHighlight code={codeText} spans={spans} language={language} />
              </Suspense>
            </pre>
          ) : null}
        </div>
      </div>
      {showsDiagram ? (
        <div ref={previewRef} data-mermaid-preview data-format={format} onClick={(event) => {
          if ((event.target as Element).closest?.("svg")) setExpanded(true);
        }}>
          <Suspense
            fallback={(
              <div className={styles.mermaidPreview} contentEditable={false} role="img" aria-label={blockItemText("code.diagramPreview")}>
                <div className={styles.mermaidEmpty}>{blockItemText("code.diagramPreview")}</div>
              </div>
            )}
          >
            <MermaidPreview source={codeText} blockId={block.id} onBroken={setBroken} />
          </Suspense>
        </div>
      ) : null}
      {drawn && !adapter.readOnly ? <BlockCornerHandles kind="video" figureRef={blockRef} boxRef={blockRef} grips="sides"
        onSize={(size) => setDraggedWidth(size.width)} onEnd={(size) => {
          setDraggedWidth(null);
          if (size && size.width !== (storedWidth ?? 100)) adapter.updateBlock(block, { width: size.width });
        }} /> : null}
      {expanded ? <MermaidLightbox previewRef={previewRef} onClose={() => setExpanded(false)} /> : null}
    </div>
  );
}
