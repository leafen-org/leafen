import { useEffect } from "react";
import { forgetNativeEditorHistories } from "@/components/editor/nativeEditorUndoScopes";

/**
 * A row's window — its side or center peek — ends the page's history as it
 * opens, as Notion's (forgetNativeEditorHistories): ⌘Z in it, or on the page
 * once it has closed, takes back nothing done before it opened, and what was
 * done in it stays with it. The row's own page keeps its history. Only the
 * opening ends it: another row shown in the same window leaves alone what was
 * done behind it meanwhile.
 */
export function useRowWindowEndsHistoryBehind(windowed: boolean, rowId: string) {
  useEffect(() => {
    if (windowed) forgetNativeEditorHistories((scope) => scope.split("/")[1] === rowId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowed]);
}
