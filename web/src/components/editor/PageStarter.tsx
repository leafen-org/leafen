import { type KeyboardEvent, type ReactNode, type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNativeToolbar } from "@/lib/nativeToolbar";
import { createFocusManager } from "@react-aria/focus";
import type { GraphViewType } from "@/contentGraph";
import { DocPlainTextIcon, TableIcon } from "@/icons/leafen";
import { useOptionalProductToast } from "@/lib/productToast";
import { useProductPages } from "@/components/product/productPagesContext";
import { PageDatabaseStartPicker } from "./PageDatabaseStartPicker";
import { PageStarterMore } from "./PageStarterMore";
import type { EditorPageStarterActions, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";

/** How long a page opening takes to settle under its starter. */
const STARTER_SETTLES_MS = 400;

/** Notion's starter lies over the page's foot: its box 254px above the
 * bottom of the page's scroller however tall the head above it is (seen
 * 2026-10-31 at 900, 700 and 450px tall, with a cover and without); how far
 * an empty page reaches under it is the editor's (EditorCanvas). Without a
 * scroller it stays in the flow. */
function usePinnedToScrollerFoot(ref: RefObject<HTMLDivElement | null>, visible: boolean) {
  useLayoutEffect(() => {
    const starter = ref.current;
    const surface = starter?.offsetParent;
    let scroller = starter?.parentElement ?? null;
    while (scroller && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) scroller = scroller.parentElement;
    if (!visible || !starter || !(surface instanceof HTMLElement) || !scroller) return;
    const foot = scroller;
    // While the page that shows it opens, it shows once its place holds for
    // a frame, and there at once; it slides only as the page changes after
    // (a cover added). Placed as the page opened, it slid from mid-page down
    // to its place as the head above it settled a frame later (2026-11-13:
    // 391 → 637px over 220 ms; Notion's stands in place).
    starter.style.transition = "none";
    starter.style.visibility = "hidden";
    const opened = performance.now();
    let held = "";
    let sliding: number | undefined;
    let frame = window.requestAnimationFrame(function settle() {
      const now = starter.style.top;
      if (now === held || performance.now() - opened > STARTER_SETTLES_MS) {
        starter.style.removeProperty("visibility");
        sliding = window.setTimeout(() => starter.style.removeProperty("transition"), STARTER_SETTLES_MS);
        return;
      }
      held = now;
      frame = window.requestAnimationFrame(settle);
    });
    const place = () => {
      const top = surface.getBoundingClientRect().top - foot.getBoundingClientRect().top + foot.scrollTop;
      starter.style.top = `${foot.clientHeight - 254 - top}px`;
      starter.dataset.pinned = "true";
    };
    place();
    const observer = new ResizeObserver(place);
    for (const element of [foot, surface, surface.parentElement]) if (element) observer.observe(element);
    return () => {
      observer.disconnect();
      window.cancelAnimationFrame(frame);
      window.clearTimeout(sliding);
      delete starter.dataset.pinned;
      starter.style.removeProperty("top");
      starter.style.removeProperty("transition");
      starter.style.removeProperty("visibility");
    };
  }, [ref, visible]);
}

/** Notion's empty-page starting points: a database chooser, a new form (not
 * for a guest) and "⋯" (the page as a database in another view, or an
 * import); its AI pills and "템플릿" are left out (seen 2026-10-31). Writing uses the
 * editor's blank line; Escape returns to that line. With a hint it is that
 * line alone, as a database template's empty body (Notion's, seen
 * 2026-10-30). */
export function PageStarter({ actions, adapter, onAddDatabase, onText, hint, visible = true }: {
  actions?: EditorPageStarterActions;
  hint?: string;
  adapter: NativeEditorGraphAdapter;
  placeholderBlockId: string | null;
  onAddDatabase: () => void;
  onText: () => void;
  visible?: boolean;
}) {
  const { t } = useTranslation("editor");
  const toast = useOptionalProductToast();
  const productPages = useProductPages();
  // A guest's row has no form, as Notion's guest's "데이터베이스 · 템플릿 · ⋯" (seen 2026-10-31).
  const guest = productPages?.workspaces?.find((workspace) => workspace.id === productPages.workspaceId)?.guest === true;
  const [pickerOpen, setPickerOpen] = useState(false), [creating, setCreating] = useState<GraphViewType | null>(null);
  const work = useRef<AbortController | null>(null);
  useEffect(() => () => { work.current?.abort(); }, []);
  useEffect(() => { if (!visible) { setPickerOpen(false); work.current?.abort(); } }, [visible]);
  const starterRef = useRef<HTMLDivElement>(null);
  const { toolbarProps } = useNativeToolbar({ "aria-label": t("starter.region"), orientation: "horizontal" }, starterRef);
  usePinnedToScrollerFoot(starterRef, visible && !hint);
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.defaultPrevented) return;
    if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onText(); return; }
    if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      const buttons = createFocusManager(starterRef);
      if (event.key === "Home") buttons.focusFirst(); else buttons.focusLast();
      return;
    }
    toolbarProps.onKeyDown?.(event);
  }
  // The page shows the database it became; Notion says nothing more.
  async function startView(view: GraphViewType) {
    if (!actions || work.current) return;
    const controller = new AbortController(); work.current = controller; setCreating(view);
    try {
      await actions.onStartDatabase(view, controller.signal);
    } catch {
      if (!controller.signal.aborted) {
        if (toast) toast.notify(t("couldntCreateDatabase"), "error");
        else adapter.moveContext?.onStatus?.(t("couldntCreateDatabase"), "error");
      }
    } finally {
      if (work.current === controller) { work.current = null; setCreating(null); }
    }
  }
  const openImport = productPages?.openImport;
  const busy = creating !== null;
  const button = (label: string, icon: ReactNode, onClick: () => void, disabled = false) => <button type="button"
    className={styles.pageStarterButton} data-page-starter-action disabled={disabled} onClick={onClick}>
    <span className={styles.pageStarterIcon}>{icon}</span><span className={styles.pageStarterText}>{label}</span>
  </button>;
  if (hint) {
    return visible ? <div className={styles.pageStarterHint} contentEditable={false}>
      <button type="button" data-page-starter-hint onClick={onText}>{hint}</button>
    </div> : null;
  }
  return <>
    {visible ? <div {...toolbarProps} ref={starterRef} className={styles.pageStarter} contentEditable={false} onKeyDown={onKeyDown}>
      <div className={styles.pageStarterLabel}>{t("starter.getStarted")}</div>
      <div className={styles.pageStarterPrimary} role="group" aria-label={t("starter.actions")}>
        {button(t("starter.database"), <TableIcon size="regular" aria-hidden="true" />,
          () => actions ? setPickerOpen(true) : onAddDatabase(), busy)}
        {actions && !guest ? button(creating === "form" ? t("starter.creating") : t("starter.form"), <DocPlainTextIcon size="regular" aria-hidden="true" />,
          () => void startView("form"), busy) : null}
        {actions ? <PageStarterMore disabled={busy} onView={(view) => void startView(view)} onImport={openImport ? () => openImport({ pageId: adapter.pageId,
          title: productPages?.pages.find((page) => page.id === adapter.pageId)?.title ?? "" }) : undefined} /> : null}
      </div>
    </div> : null}
    {pickerOpen && actions && visible ? <PageDatabaseStartPicker actions={actions} onClose={() => setPickerOpen(false)} /> : null}
  </>;
}
