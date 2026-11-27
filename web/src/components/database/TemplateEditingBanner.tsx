import { type ComponentProps, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import type { DatabaseToolbarTemplateMetadataWire, DatabaseToolbarTemplateRepeatWire } from "@/databaseToolbarConfigWire.generated";
import { ChevronDown, DatabaseCylinderIcon } from "@/icons/leafen";
import { hasExplicitPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { screenKind } from "@/lib/screenKind";
import { DatabaseTemplateRepeatChooser, templateRepeatLabel } from "./DatabaseTemplateRepeatChooser";
import { templateRepeats } from "./templateRepeatModel";
import { databaseViewLabels } from "./databaseViewLabels";
import styles from "./databaseSheet";

// A file of its own, apart from the template's window (DatabaseTemplateEditor),
// so a page under a template draws its band (PageTemplateBands) without
// bringing the database's views, which the window reaches, into the
// signed-in product's first screen.

/**
 * The band over a template being edited: "다음에서 템플릿을 편집 중입니다.", the
 * database it is a template of, and its "복제 주기 ⌄" where its reader may
 * change it — over its window, and over one of its own databases opened
 * alone (RowPageDatabaseAlone), as Notion's (seen 2026-11-15). One who may
 * not change it reads "다음에서 템플릿을 보고 있습니다." without the repeat, as
 * Notion's reader of its database (seen 2026-11-15). On a touch screen, a
 * phone's as a tablet's, it names where the template lives, "〈DB〉 내 템플릿",
 * from its start and without the repeat, as Notion's iPad (seen 2026-11-15 and
 * 2026-11-16 at 834: the mark 20 in, 4 before the name, 8 before "내
 * 템플릿") and its phone (seen 2026-11-25 on a Galaxy A90's Chrome at 411,
 * two tabs); its "?" is a Help Center link, left out.
 */
export function TemplateEditingBanner({ databaseTitle, databaseIcon, repeat, viewing = false }: {
  databaseTitle: string;
  databaseIcon?: ComponentProps<typeof PageIconGlyph>["page"];
  viewing?: boolean;
  repeat?: { template: DatabaseToolbarTemplateMetadataWire; onSave(repeat: DatabaseToolbarTemplateRepeatWire | null): void };
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("databaseView");
  const icon = databaseIcon && hasExplicitPageIcon(databaseIcon) ? databaseIcon : null;
  const touchBand = !viewing && screenKind() !== "desktop";
  const [before = "", after = ""] = touchBand ? t("templateInDatabase", { name: "\u0001" }).split("\u0001").map((part) => part.trim()) : [];
  return (
    <div className={styles.templateEditorBanner} data-database-template-editor data-touch-band={touchBand || undefined}>
      {touchBand ? before ? <span>{before}</span> : null : <span>{viewing ? labels.viewingTemplateIn : labels.editingTemplateIn}</span>}
      <span className={styles.templateEditorBannerSource}>
        {/* A database with no icon of its own is Notion's "collection", a cylinder with one band (seen 2026-11-15, 20 in the icons' gray). */}
        {icon ? <span className={styles.templateEditorBannerIcon} aria-hidden="true"><PageIconGlyph page={icon} size={18} fallback="none" /></span> : <DatabaseCylinderIcon size="regular" aria-hidden="true" />}
        <span className={styles.templateEditorBannerName}>{databaseTitle}</span>
      </span>
      {after ? <span>{after}</span> : null}
      {repeat && !touchBand ? <TemplateRepeatButton template={repeat.template} label={templateRepeats(repeat.template)
        ? `${t("templateMenu.repeat")} ${templateRepeatLabel(repeat.template, t)}` : t("templateMenu.repeat")} onSave={repeat.onSave} /> : null}
    </div>
  );
}

/** The banner's "복제 주기 ⌄" ("복제 주기 3 주 ⌄" while it repeats, as Notion's), which opens the template's repeat below it. */
function TemplateRepeatButton({ template, label, onSave }: {
  template: DatabaseToolbarTemplateMetadataWire; label: string; onSave(repeat: DatabaseToolbarTemplateRepeatWire | null): void;
}) {
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), panelRef = useRef<HTMLDivElement>(null);
  // Over the page under the button, not cut off by the editor's edge.
  const place = usePopoverPlacement(panelRef, open && buttonRef.current
    ? (size, viewport) => placeAround(popoverAnchorOf(buttonRef.current!), size, viewport, { align: "start", gap: 4 }) : null,
  // Escape and a press outside are the chooser's, which asks before dropping a change.
  [open], undefined, false);
  return (
    <span className={styles.templateBannerRepeat}>
      <button ref={buttonRef} type="button" className={styles.templateBannerDuplicate} aria-haspopup="dialog" aria-expanded={open}
        data-active={templateRepeats(template) ? "true" : undefined} onClick={() => setOpen((value) => !value)}>
        {label}<ChevronDown size="small" aria-hidden="true" />
      </button>
      {open ? createPortal(
        <DatabaseTemplateRepeatChooser template={template} boxRef={panelRef} placement={{ position: "fixed", ...popoverPlacementStyle(place) }}
          onClose={() => setOpen(false)} onSave={(repeat) => { onSave(repeat); setOpen(false); }} />, document.body) : null}
    </span>
  );
}
