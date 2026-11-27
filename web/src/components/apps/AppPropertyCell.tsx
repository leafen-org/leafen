import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { appText } from "@/components/admin/administratorAppsApi";
import { useProductPages } from "@/components/product/productPagesContext";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { DbProperty, Page } from "@/lib/types";
import { AppPlaceFrame } from "./AppPlaceFrame";
import styles from "./AppPropertyCell.module.css";
import { usableAppFor, useUsableAppList } from "./useUsableApps";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { appBridgeRefusal } from "./appFrameBridge";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// A database cell an app draws (docs/app-platform.md 10-1): the
// property names the app and its editor; while the person may use that app
// in the page's organization, the cell shows the app's frame, no bigger than
// the cell and taking no clicks, and choosing the cell opens the editor in a
// box Leafen draws under it. The value keeps the property's own type —
// checked here before it is written — so without the app the cell is
// Leafen's own again.

const EDITOR_HEIGHT = 240;
const EDITOR_WIDTH = 320;
const TEXT_MAX = 2000;

type Option = { id: string; name: string; color: string };

/** The value an editor sets, in the property's type, or why it can't be. */
function appCellValue(type: string, options: readonly Option[], raw: unknown): { value: unknown } | { refused: string } {
  const known = (id: unknown) => typeof id === "string" && options.some((option) => option.id === id);
  switch (type) {
    case "number":
      return raw === null || (typeof raw === "number" && Number.isFinite(raw)) ? { value: raw } : { refused: "a number or null" };
    case "checkbox":
      return typeof raw === "boolean" ? { value: raw } : { refused: "true or false" };
    case "select":
    case "status":
      return raw === null || known(raw) ? { value: raw } : { refused: "one of the property's option ids, or null" };
    case "multi_select":
      return Array.isArray(raw) && raw.length <= 100 && raw.every(known) && new Set(raw).size === raw.length
        ? { value: raw } : { refused: "a list of the property's option ids, each once" };
    default:
      return typeof raw === "string" && raw.length <= TEXT_MAX ? { value: raw } : { refused: "text of up to 2000 characters" };
  }
}

export function AppPropertyCell({ row, prop, editable, onSet, fallback }: {
  row: Page;
  prop: DbProperty;
  editable: boolean;
  /** Writes a checked value. */
  onSet(value: unknown): void;
  fallback: ReactNode;
}) {
  const { t, i18n } = useTranslation("common");
  const pages = useProductPages();
  const session = pages?.session;
  const named = prop.config?.appEditor;
  const { apps } = useUsableAppList(session, Boolean(session && named));
  const usable = named ? usableAppFor(apps, named.app, pages?.organizationId) : undefined;
  const editor = usable?.propertyEditors?.find((candidate) => candidate.key === named?.editor && candidate.types.includes(prop.type));
  const value = row.properties?.[prop.id] ?? null;
  const options = useMemo(() => (prop.config?.options ?? []).map(({ id, name, color }) => ({ id, name, color })), [prop.config?.options]);
  const shown = JSON.stringify({ value, options });
  const cellRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  const displayPort = useRef<MessagePort | null>(null);
  const editorPort = useRef<MessagePort | null>(null);
  const [visible, setVisible] = useState(false);
  const [open, setOpen] = useState<{ left: number; top: number; width: number } | null>(null);
  // The cell keeps focus while the app's editor is open (the frame can't
  // hear the page's keys), so it counts as the box's for Escape.
  const { zIndex } = useOverlayLayer(Boolean(open), { kind: "menu", ref: boxRef, onEscape: () => setOpen(null), onOutsidePress: () => setOpen(null),
    inside: () => [cellRef.current], returnFocusRef: cellRef });

  // A cell's frame opens only once the cell is on screen.
  useEffect(() => {
    const cell = cellRef.current;
    if (!cell || !editor || visible) return undefined;
    const watch = new IntersectionObserver((entries) => { if (entries.some((entry) => entry.isIntersecting)) setVisible(true); });
    watch.observe(cell);
    return () => watch.disconnect();
  }, [editor, visible]);
  // The value changed here or elsewhere: both frames hear it.
  useEffect(() => {
    const message = { type: "value", ...(JSON.parse(shown) as object) };
    displayPort.current?.postMessage(message);
    editorPort.current?.postMessage(message);
  }, [shown]);

  if (!session || !usable || !editor) return <>{fallback}</>;
  const name = appText(editor.name, i18n.language);
  const context = (place: "propertyCell" | "propertyEditor") => () => ({
    rowId: row.id, propertyId: prop.id, type: prop.type, editable: place === "propertyEditor" && editable, ...(JSON.parse(shown) as object),
  });
  const choose = () => {
    if (!editable || !cellRef.current) return;
    const box = cellRef.current.getBoundingClientRect();
    const width = Math.min(Math.max(box.width, EDITOR_WIDTH), window.innerWidth - POPOVER_MARGIN * 2);
    setOpen({ left: Math.max(POPOVER_MARGIN, Math.min(box.left, window.innerWidth - width - POPOVER_MARGIN)), top: box.bottom + 4, width });
  };
  const height = editor.height || EDITOR_HEIGHT;
  return (
    <>
      <div ref={cellRef} className={styles.cell} role={editable ? "button" : undefined} tabIndex={editable ? 0 : undefined}
        aria-label={t("appPlace.cellLabel", { name: prop.name, editor: name })} data-app-property-cell={prop.id}
        onClick={choose} onKeyDown={(event) => { if (fieldEnter(event) || event.key === " ") { event.preventDefault(); choose(); } }}>
        {visible ? (
          <AppPlaceFrame session={session} usable={usable} place="propertyCell" opens={{ surface: "propertyEditor", editor: editor.key }} shared
            title={name} style={{ position: "absolute", inset: 0, height: "100%", pointerEvents: "none" }} context={context("propertyCell")}
            port={displayPort} />
        ) : null}
      </div>
      {open && typeof document !== "undefined" ? createPortal(
        <div ref={boxRef} className={styles.editor} role="dialog" aria-label={name} data-app-property-editor={prop.id}
          style={{ left: open.left, top: Math.min(open.top, window.innerHeight - height - 44), width: open.width, zIndex }}>
          <div className={styles.head}>
            <span className={styles.name}>{name}</span>
            <span className={styles.mark}>{t("appPlace.markNamed", { app: appText(usable.name, i18n.language) })}</span>
          </div>
          <AppPlaceFrame session={session} usable={usable} place="propertyEditor" opens={{ surface: "propertyEditor", editor: editor.key }}
            title={name} style={{ height }} context={context("propertyEditor")} port={editorPort}
            extra={async (method, params) => {
              if (method === "closeEditor") {
                setOpen(null);
                return null;
              }
              if (method !== "setValue") throw appBridgeRefusal("unknownCall");
              const checked = appCellValue(prop.type, options, params.value);
              if ("refused" in checked) throw new Error(`invalid_value: ${checked.refused}`);
              onSet(checked.value);
              return null;
            }} />
        </div>,
        document.body,
      ) : null}
    </>
  );
}
