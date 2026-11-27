import { LocationArrowIcon } from "@/icons/leafen";
import { PropertyMark } from "@/components/database/PropertyIcon";
import { libraryColumnMark } from "./libraryFilters";
import type { LibraryColumn } from "./libraryModel";

/**
 * A library column's mark before its name, as Notion's (seen 2026-10-07),
 * filled: a page for the name, a person for who made or last edited it, an
 * arrow for where it lives and a clock for its times — the marks its filter
 * and sort lists draw too (libraryColumnMark).
 */
export function LibraryColumnIcon({ column }: { column: LibraryColumn }) {
  if (column === "source") return <LocationArrowIcon size="page" aria-hidden="true" />;
  return <PropertyMark mark={libraryColumnMark(column)} size="page" />;
}
