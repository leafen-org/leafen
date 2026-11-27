import { useRef, useState } from "react";
import { ChevronUpDown } from "@/icons/leafen";
import { CellMenuPortal } from "@/components/database/PropertyCellMenu";
import { PlacePicker } from "@/components/database/PlacePicker";
import { placePropertySummary } from "@/components/database/structuredPropertyValues";
import type { PlaceSearchResponse } from "@/administratorWire.generated";
import type { PlacePropertyValue } from "@/lib/types";
import cellStyles from "@/components/database/databaseSheet";
import dropdownStyles from "./FormDropdown.module.css";

/** A place answer: a point with what was typed for it. */
function formPlaceAnswer(value: unknown): PlacePropertyValue | null {
  if (!value || typeof value !== "object") return null;
  const { lat, lon } = value as { lat?: unknown; lon?: unknown };
  return typeof lat === "number" && typeof lon === "number" ? value as PlacePropertyValue : null;
}

/**
 * A place question as Notion's form asks it (seen 2026-11-02: "응답자의
 * 답변" with ⇕, as a date's): the field holds the place as a value shows
 * it (placePropertySummary); opened, the place picker hangs under it, Leafen's — the
 * point picked on the map, its name and address typed, and a search by name
 * where the server has a place search provider and the form can ask it
 * (`search`, through the form's own token).
 */
export function FormPlaceField({ id, label, value, placeholder, disabled, search, onChange }: {
  id: string;
  label: string;
  value: unknown;
  placeholder: string;
  disabled: boolean;
  search?: (query: string, signal: AbortSignal) => Promise<PlaceSearchResponse>;
  onChange: (value: PlacePropertyValue | null) => void;
}) {
  const field = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const place = formPlaceAnswer(value);
  const shown = place ? placePropertySummary(place) : "";
  return <>
    <button ref={field} id={`form-answer-${id}`} type="button" className={dropdownStyles.field} aria-label={label} aria-haspopup="dialog"
      aria-expanded={open} disabled={disabled} data-form-place-field onClick={() => setOpen(!open)}>
      {shown ? <span className={dropdownStyles.fieldChips}>{shown}</span> : <span className={dropdownStyles.placeholder}>{placeholder}</span>}
      <ChevronUpDown size="small" className={dropdownStyles.fieldIcon} aria-hidden="true" />
    </button>
    {open ? <CellMenuPortal triggerRef={field} ariaLabel={label} className={cellStyles.selectMenu} backdropClassName={cellStyles.menuBackdrop}
      width={240} onClose={() => setOpen(false)}>
      <PlacePicker value={place} search={search} onChange={onChange} onRemove={() => { onChange(null); setOpen(false); }} />
    </CellMenuPortal> : null}
  </>;
}
