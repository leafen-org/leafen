import { useMemo, useState } from "react";
import type { PageSuggestingControl } from "../RowMenu";

/**
 * A database page's suggesting mode, turned on from its … menu by someone
 * who can comment on its body. It turns off when another row's page opens.
 */
export function useRowPageSuggesting(rowId: string, offered: boolean) {
  const [suggestingRowId, setSuggestingRowId] = useState<string | null>(null);
  const [bodyCanSuggest, setBodyCanSuggest] = useState(false);
  const available = offered && bodyCanSuggest;
  const active = available && suggestingRowId === rowId;
  const control = useMemo<PageSuggestingControl | undefined>(() => (available
    ? { active, onChange: (on) => setSuggestingRowId(on ? rowId : null) }
    : undefined), [active, available, rowId]);
  return { active, control, onBodyCanSuggest: setBodyCanSuggest };
}
