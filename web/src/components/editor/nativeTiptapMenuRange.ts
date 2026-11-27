import type { Transaction } from "@tiptap/pm/state";
import { ReplaceStep } from "@tiptap/pm/transform";
import { nativeProjectionMeta } from "./nativeTiptapBinding";

/**
 * Where an open menu's typed words stand after a transaction. The page drawn
 * again over the same letters (a projection that changes only their marks, as
 * when suggested letters come back from the server marked) keeps a place
 * inside them where it was: mapped as an edit, it went to the end of what was
 * drawn again, and a "/" or "@" typed while the letter before it was still on
 * its way lost its menu as the two came back (2026-11-18: " /" typed at once
 * in suggesting mode, the menu gone 73 ms later).
 */
export function mapMenuRange(tr: Transaction, range: { from: number; to: number }) {
  if (!tr.getMeta(nativeProjectionMeta)) {
    range.from = tr.mapping.map(range.from, 1); range.to = tr.mapping.map(range.to, -1);
    return;
  }
  const through = (position: number, side: 1 | -1) => tr.steps.reduce((at, step, index) => {
    const doc = tr.docs[index];
    const redrawn = step instanceof ReplaceStep && at > step.from && at < step.to && step.slice.openStart === 0 && step.slice.openEnd === 0
      && step.slice.size === step.to - step.from
      && doc.textBetween(step.from, step.to, "\n", "￼") === step.slice.content.textBetween(0, step.slice.size, "\n", "￼");
    return redrawn ? at : step.getMap().map(at, side);
  }, position);
  range.from = through(range.from, 1); range.to = through(range.to, -1);
}
