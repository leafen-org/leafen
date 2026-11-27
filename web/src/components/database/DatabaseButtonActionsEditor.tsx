import { useEffect, useRef, useState } from "react";
import type { GraphProperty } from "@/contentGraph";
import type { AutomationAction, DbProperty, DbView, Page } from "@/lib/types";
import { AutomationActionEditor } from "@/components/automation/AutomationActionEditor";
import { AutomationFloatingMenus } from "@/components/automation/AutomationActionMenus";
import { databaseButtonActions, databaseButtonConfig } from "@/lib/databaseButtonConfig";
import { TYPED_DRAFT_SAVE_MS } from "@/lib/useTypedDraftSave";
import { databaseAutomationEditorBounds } from "./databaseAutomationPanelModel";

/**
 * A button property's actions as they are set — from its column's menu, its
 * "자동화 편집" window, or its settings on a row page or a phone. What is typed
 * in an action's fields shows at once and the next key builds on it, as
 * Notion's keeps what is typed there: the actions are this editor's while it
 * is open, so what is read back meanwhile (a save's answer, or a read from
 * before it) never puts an older value under the next key (2026-11-13: a
 * webhook's address typed in saved as its last letter, each key saving the
 * property and the field drawn again from the read). The change saves onto
 * the property as it is then, as a field is left, once typing stops, as an
 * action is added or taken away, as the editor closes or the window is
 * hidden; a property no longer a button by then keeps what it became.
 */
export function DatabaseButtonActionsEditor({ property, current, onSave, properties, pages, views, newWhenEmpty }: {
  property: GraphProperty;
  /** The property as it is now, which the change is saved onto. */
  current(): GraphProperty | undefined;
  onSave(property: GraphProperty): void;
  properties: DbProperty[];
  pages: Page[];
  views?: DbView[];
  newWhenEmpty?: boolean;
}) {
  const [actions, setActions] = useState(() => databaseButtonActions(property));
  const draft = useRef({ actions, timer: 0, unsaved: false });
  const save = useRef(() => {});
  save.current = () => {
    const state = draft.current;
    window.clearTimeout(state.timer);
    if (!state.unsaved) return;
    state.unsaved = false;
    const live = current();
    if (live?.type === "button") onSave({ ...live, config: databaseButtonConfig(live, state.actions) });
  };
  useEffect(() => {
    const onHidden = () => { if (document.visibilityState === "hidden") save.current(); };
    const onPageHide = () => save.current();
    document.addEventListener("visibilitychange", onHidden);
    window.addEventListener("pagehide", onPageHide);
    return () => {
      document.removeEventListener("visibilitychange", onHidden);
      window.removeEventListener("pagehide", onPageHide);
      save.current();
    };
  }, []);
  function change(next: AutomationAction[]) {
    const state = draft.current;
    // An action added or taken away saves right after the press, not once typing stops, so its column's head
    // says at once whether one is unfinished (ButtonPropertyHeadMark, Notion's "버튼 ⚠", seen 2026-11-21).
    const counted = next.length !== state.actions.length;
    state.actions = next;
    state.unsaved = true;
    setActions(next);
    window.clearTimeout(state.timer);
    state.timer = window.setTimeout(() => save.current(), counted ? 0 : TYPED_DRAFT_SAVE_MS);
  }
  // Its own box is no box: the editor lays out in its place as before.
  return <div style={{ display: "contents" }} onBlur={() => save.current()}>
    <AutomationFloatingMenus.Provider value={true}>
      <AutomationActionEditor surface="database_button" bounds={databaseAutomationEditorBounds} actions={actions}
        properties={properties} pages={pages} views={views} newWhenEmpty={newWhenEmpty}
        readActions={() => draft.current.actions} onChange={change} />
    </AutomationFloatingMenus.Provider>
  </div>;
}
