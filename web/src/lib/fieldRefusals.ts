// A field a form can't be sent without, or whose value it doesn't take, says
// so in Leafen's words just under it, not in the browser's own bubble ("이
// 입력란을 작성하세요.", seen 2026-11-06 on a legal hold's reason and some
// forty more fields). One boundary for every form: the browser's `invalid`
// event is stopped before its bubble, the field is marked aria-invalid, the
// first refused field of a send takes the focus, and a note stands under it
// until it takes a value (or the person leaves it, where it floats).

import { i18next } from "@/i18n";

const FIELD = "input, textarea, select";
const NOTE_GAP = 4;

let note: HTMLParagraphElement | null = null;
// A note in a field stack takes its own line under the field; one by a field
// in a row floats under it (2026-11-07: floating, a legal hold's "사유" note
// sat on the "범위" name under it).
let inFlow = false;
let owner: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement | null = null;
let watching = false;
// The refusals of one send arrive together: the first takes the focus.
let focusedThisTurn = false;

type Field = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;

function words(field: Field) {
  const t = (key: string, values?: Record<string, unknown>) => i18next.t(`common:fieldRefusal.${key}`, values);
  const validity = field.validity;
  const type = field instanceof HTMLInputElement ? field.type : "";
  if (validity.valueMissing) {
    return field instanceof HTMLSelectElement || type === "checkbox" || type === "radio" ? t("choose") : t("fill");
  }
  if (validity.typeMismatch && type === "email") return t("email");
  if (validity.typeMismatch && type === "url") return t("url");
  if (validity.rangeUnderflow && field instanceof HTMLInputElement) return t("atLeast", { min: field.min });
  if (validity.rangeOverflow && field instanceof HTMLInputElement) return t("atMost", { max: field.max });
  if (validity.tooShort && !(field instanceof HTMLSelectElement)) return t("tooShort", { count: field.minLength });
  return t("check");
}

function place() {
  if (!note || !owner || inFlow) return;
  const box = owner.getBoundingClientRect();
  if (box.width === 0 && box.height === 0) {
    clear();
    return;
  }
  note.style.left = `${Math.round(box.left)}px`;
  note.style.top = `${Math.round(box.bottom + NOTE_GAP)}px`;
  note.style.maxWidth = `${Math.max(160, Math.round(box.width))}px`;
}

function clear() {
  owner?.removeAttribute("aria-invalid");
  owner?.removeAttribute("aria-describedby");
  owner = null;
  note?.remove();
  note = null;
  inFlow = false;
}

// Whether the field's box lays its children one under another.
function stacked(field: Field) {
  const parent = field.parentElement;
  if (!parent) return false;
  const style = getComputedStyle(parent);
  return style.display === "block" || style.display === "grid" || style.display === "inline-grid"
    || (style.display.includes("flex") && style.flexDirection.startsWith("column"));
}

function show(field: Field) {
  clear();
  owner = field;
  note = document.createElement("p");
  note.id = "leafen-field-refusal";
  note.setAttribute("role", "alert");
  note.dataset.fieldRefusal = "true";
  note.textContent = words(field);
  inFlow = stacked(field);
  Object.assign(note.style, inFlow ? {
    margin: `${NOTE_GAP}px 0 0`, color: "var(--danger)", fontSize: "13px", lineHeight: "18px", fontWeight: "400",
  } : {
    position: "fixed", margin: "0", zIndex: "2147483000", pointerEvents: "none",
    color: "var(--danger)", fontSize: "13px", lineHeight: "18px",
  });
  if (inFlow) field.insertAdjacentElement("afterend", note);
  else document.body.append(note);
  field.setAttribute("aria-invalid", "true");
  field.setAttribute("aria-describedby", note.id);
  place();
}

export function watchFieldRefusals() {
  if (watching || typeof document === "undefined") return;
  watching = true;
  document.addEventListener("invalid", (event) => {
    const field = event.target;
    if (!(field instanceof Element) || !field.matches(FIELD)) return;
    event.preventDefault();
    if (focusedThisTurn) return;
    focusedThisTurn = true;
    queueMicrotask(() => { focusedThisTurn = false; });
    const target = field as Field;
    target.focus({ preventScroll: false });
    show(target);
  }, true);
  // The note goes once its field takes a value or the person moves on.
  document.addEventListener("input", (event) => {
    if (owner && event.target === owner && owner.validity.valid) clear();
  }, true);
  document.addEventListener("change", (event) => {
    if (owner && event.target === owner && owner.validity.valid) clear();
  }, true);
  // A floating note goes as the person leaves its field; one on its own line
  // stays, saying what is still to fill, until the field takes a value.
  document.addEventListener("focusout", (event) => {
    if (owner && event.target === owner && !inFlow) clear();
  }, true);
  window.addEventListener("scroll", place, true);
  window.addEventListener("resize", place);
}
