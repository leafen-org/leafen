import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { DoubleChevronRight } from "@/icons/leafen";
import { FormUnavailableNotice, PublicFormView } from "@/components/PublicFormView";
import { FormPreviewActions, FormPreviewDialog } from "@/components/database/FormPreviewDialog";
import sheet from "@/components/database/databaseSheet";
import { storedRowPeekWidth } from "@/components/database/sidePeekWidth";
import { absoluteFormUrl, formEditHref, formHref } from "@/lib/formViewModel";
import { createNativeFormService, nativeFormSurfaceBounds } from "@/lib/nativeFormService";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { closeFormPeek } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import { useSideRowPeekPlace } from "@/lib/sideRowPeekLayout";
import { useSidePeekMotion } from "@/lib/sidePeekMotion";
import type { FormDefinitionResult } from "@/lib/types";
import type { Session } from "@/types";
import styles from "./FormPeek.module.css";

/**
 * A form's answer page a button opened, as Notion's (seen 2026-11-05):
 * over the page in the window its preview has, or beside it in a page's side
 * peek with "»" to close, either with "양식 편집" (to whoever may edit it),
 * "양식 링크 복사", "즐겨찾기" and "전체 페이지로 열기" in one pill at its top
 * right; the form as its respondents see it, or, when it takes no answers of
 * theirs, that it is not available.
 */
export function FormPeek({ token, mode, session }: { token: string; mode: "side" | "center"; session: Session }) {
  const { t } = useTranslation(["pagePeek", "formView"]);
  const service = useMemo(() => createNativeFormService(api, () => session), [session]);
  const [definition, setDefinition] = useState<FormDefinitionResult | null>(null);
  const answerable = token !== "-";
  const place = definition?.place;
  const actions = answerable ? {
    token,
    onEdit: place?.canEdit ? () => pushRoute(formEditHref(place)) : null,
    onCopyLink: () => absoluteFormUrl(token),
    onOpenFull: () => pushRoute(formHref(token)),
  } : { onEdit: null };
  const body = answerable
    ? <PublicFormView key={token} bounds={nativeFormSurfaceBounds} service={service} token={token} inApp onDefinition={setDefinition} />
    : <FormUnavailableNotice />;
  if (mode === "center") return <FormPreviewDialog {...actions} focusEntersWindow onClose={closeFormPeek}>{body}</FormPreviewDialog>;
  return <FormSidePeek title={definition?.form.title || t("formView:untitledForm")} actions={actions} itemId={token}>{body}</FormSidePeek>;
}

function FormSidePeek({ title, actions, itemId, children }: {
  title: string; actions: Parameters<typeof FormPreviewActions>[0]; itemId: string; children: ReactNode;
}) {
  const { t } = useTranslation("pagePeek");
  const panelRef = useRef<HTMLElement>(null);
  const leave = useSidePeekMotion(panelRef, true, itemId);
  const close = () => leave(closeFormPeek);
  const [entered, setEntered] = useState(false);
  const [width] = useState(storedRowPeekWidth);
  useOverlayLayer(true, { kind: "panel", ref: panelRef, onEscape: close });
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      setEntered(true);
      if (!panelRef.current?.contains(document.activeElement)) panelRef.current?.focus();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [itemId]);
  useSideRowPeekPlace(panelRef, true, width, itemId);
  const view = (
    <aside ref={panelRef} className={sheet.rowPeek} data-mode="side" data-form-peek data-motion-state={entered ? "open" : "opening"}
      style={{ "--row-peek-width": `${width}px` } as CSSProperties} role="dialog" aria-label={t("label", { title })} tabIndex={-1}>
      <button type="button" className={`${sheet.rowPeekIconAction} ${styles.close}`} data-form-peek-close
        aria-label={t("close")} title={t("close")} onClick={close}>
        <DoubleChevronRight size="regular" />
      </button>
      <FormPreviewActions {...actions} />
      <div className={`${sheet.rowPeekScroll} ${styles.scroll} nscroll`}>{children}</div>
    </aside>
  );
  return typeof document === "undefined" ? view : createPortal(view, document.body);
}
