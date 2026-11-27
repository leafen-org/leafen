import type { ReactNode } from "react";
import { PhoneSheetGroup } from "./PhoneSheet";
import parts from "./PhoneSheetRowParts.module.css";

/**
 * What a phone sheet's search that finds nothing shows where its list was:
 * a group holding one gray row, "결과 없음" — Notion's phone sheets alike
 * (seen 2026-11-13 at 402pt in "옮기기", and 2026-11-17 in a filter's
 * options on iPhone Safari at 402 and in Android Chrome on a Galaxy A90 at
 * 411, and its people there: 17px gray words 16px in, the group 45 high).
 */
export function PhoneSheetNoResults({ children }: { children: ReactNode }) {
  return (
    <PhoneSheetGroup>
      <p className={parts.noResults} role="status" data-phone-sheet-no-results="">{children}</p>
    </PhoneSheetGroup>
  );
}
