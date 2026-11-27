/** How one side of a list card's marks fits: the marks it shows, then a grey "+n" for the rest (none when 0). */
export interface AutomationMarkSide {
  shown: number;
  more: number;
}

/**
 * How many of an automation's trigger and action marks its list card shows,
 * Notion's rule (its CollectionSettingsAutomationsView, read 2026-11-18, and
 * seen that day in Chrome at 1440 on a page added with five actions:
 * "+ → 🔔 ≡ + +2"). The card holds six slots, the "→" one of them when both
 * sides have marks; the triggers get the smaller half and the actions the
 * larger, and a side with fewer marks than its half lends the rest to the
 * other. A side with more marks than its slots shows one fewer and a "+n"
 * box for the rest — just "+n" when it has one slot.
 */
export function fitAutomationMarks(triggerCount: number, actionCount: number, slots = 6) {
  const both = triggerCount > 0 && actionCount > 0;
  const room = Math.max(slots, 3) - (both ? 1 : 0);
  let triggerRoom = Math.max(Math.floor(room / 2), 1);
  let actionRoom = Math.max(Math.ceil(room / 2), 1);
  if (triggerCount < triggerRoom && actionCount > actionRoom) {
    actionRoom += triggerRoom - triggerCount;
    triggerRoom = triggerCount;
  } else if (actionCount < actionRoom && triggerCount > triggerRoom) {
    triggerRoom += actionRoom - actionCount;
    actionRoom = actionCount;
  }
  const side = (count: number, fits: number): AutomationMarkSide =>
    count > fits ? { shown: fits - 1, more: count - (fits - 1) } : { shown: count, more: 0 };
  return { triggers: side(triggerCount, triggerRoom), actions: side(actionCount, actionRoom) };
}
