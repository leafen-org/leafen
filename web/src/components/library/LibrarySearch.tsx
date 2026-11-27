import { useEffect, useRef } from "react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import { Search, X } from "@/icons/leafen";
import panelStyles from "./LibraryPanel.module.css";

/**
 * The tab row's "검색", as Notion's library (seen 2026-10-07): a magnifier
 * that opens a field in its place, with ×; Escape or × closes it and lets
 * what was typed go. The field takes the focus within the press itself, so a
 * phone's keyboard comes up with it (iOS Safari raises it only then).
 */
export function LibrarySearch({ open, value, placeholder, onOpen, onChange, onClose }: {
  open: boolean;
  value: string;
  placeholder: string;
  onOpen: () => void;
  onChange: (value: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("library");
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (open) field.current?.focus({ preventScroll: true });
  }, [open]);
  if (!open) {
    return (
      <button type="button" className={panelStyles.tabAction} aria-label={t("search")} title={t("search")} data-library-search-open
        onClick={() => {
          flushSync(onOpen);
          field.current?.focus({ preventScroll: true });
        }}>
        <Search size="small" aria-hidden="true" />
      </button>
    );
  }
  return (
    <label className={panelStyles.searchBox} data-library-search>
      <Search size="small" aria-hidden="true" />
      <input ref={field} value={value} placeholder={placeholder} aria-label={t("search")}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          event.preventDefault();
          onClose();
        }} />
      <button type="button" className={panelStyles.panelIconButton} aria-label={t("closeSearch")} onClick={onClose}>
        <X size="tiny" aria-hidden="true" />
      </button>
    </label>
  );
}
