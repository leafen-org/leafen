import { useRef, useState } from "react";
import { Search } from "@/icons/leafen";
import chipStyles from "./SettingsFilterChip.module.css";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

/**
 * A settings table's search, as Notion's (seen 2026-10-27 on 사람, its 그룹
 * and 팀스페이스): a 28px magnifier that opens a borderless 120×20 field
 * after it. Escape empties and closes the field before the window; a click
 * elsewhere closes it only while it is empty.
 */
export function SettingsSearchToggle({ query, onQueryChange, placeholder, data }: {
  query: string;
  onQueryChange(query: string): void;
  placeholder: string;
  data?: Record<`data-${string}`, string>;
}) {
  const [open, setOpen] = useState(false);
  const fieldRef = useRef<HTMLInputElement>(null);
  return (
    <>
      <button type="button" className={chipStyles.iconButton} aria-label={placeholder} aria-expanded={open}
        onClick={() => { if (open) fieldRef.current?.focus(); else setOpen(true); }}>
        <Search size="regular" aria-hidden="true" />
      </button>
      {open ? (
        <input ref={fieldRef} autoFocus className={chipStyles.searchField} value={query} placeholder={placeholder}
          aria-label={placeholder} autoComplete="off" {...data}
          onChange={(event) => onQueryChange(event.target.value)}
          onBlur={() => { if (!query) setOpen(false); }}
          onKeyDown={(event) => {
            if (!fieldEscape(event)) return;
            event.preventDefault();
            onQueryChange("");
            setOpen(false);
          }} />
      ) : null}
    </>
  );
}
