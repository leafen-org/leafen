// The type of a window drawn as a phone's sheet (phoneWindowSheet.ts), as
// Notion's phone draws its windows' words in the phone's own text sizes: a
// window's title, words and the names over its fields take the phone's steps,
// while what a person types in or presses keeps the window's own type (seen
// 2026-11-18 on a Galaxy A90 in Chrome at 411, light, two tabs at two times,
// its "팀스페이스 만들기": the title 17/22 → 20/25 at 600, the words under
// it 14/20 → 17/22, the fields' names 12/16 → 15/20; the name and
// description fields 14/20 in a 32px box and 80px box, the "팀스페이스
// 만들기" button 14/16.8 at 500, its "팀스페이스에 대해 알아보기" 14/16.8, all
// as the desktop's). Each step: a title the title's, 14px to 16px words the
// phone's 17px, 12px and 13px the phone's 15px, so every window made a sheet
// follows one rule, words added later too.
//
// A window that marks itself typed throughout takes the phone's type in its
// fields and buttons too, a field at least 32px high in 17px words, as
// Notion's phone drew its new token window (seen once, 2026-11-15 at 402pt,
// iPhone Safari: its words 17px on 22px, a field's name 17px on 22px at 500,
// a note under it 15px on 20px, a field 32px high in 17px words).

type Step = { size: number; line: number };
const TITLE: Step & { weight: number } = { size: 20, line: 25, weight: 600 };
const BODY: Step = { size: 17, line: 22 };
const NOTE: Step = { size: 15, line: 20 };
const FIELD_HEIGHT = 32;
const TEXT_FIELDS = "input:not([type]), input[type='text'], input[type='email'], input[type='search'], input[type='url'], input[type='password'], input[type='number'], textarea";
// What a person presses keeps the window's own type, words and all.
const CONTROLS = "button, [role='button'], a[href], select, [role='combobox']";
// The marks of the windows typed throughout, on the window or what holds it.
const TYPED_THROUGHOUT = "[data-personal-access-token-dialog]";

/** The phone's step for a desktop size, or none for sizes it keeps (icons, a title's own, the phone's already). */
function stepFor(size: number): Step | null {
  if (size >= 14 && size <= 16) return BODY;
  if (size >= 12 && size < 14) return NOTE;
  return null;
}

function hasOwnWords(element: Element) {
  for (const node of element.childNodes) if (node.nodeType === Node.TEXT_NODE && node.textContent?.trim()) return true;
  return false;
}

/**
 * Sets the sheet's type by the phone's steps, again as its content changes;
 * returns what puts the window's own back.
 */
export function typePhoneSheet(root: HTMLElement): () => void {
  const touched = new Set<HTMLElement>();
  const throughout = Boolean(root.closest(TYPED_THROUGHOUT));
  const set = (element: HTMLElement, step: Step, weight?: number) => {
    element.style.setProperty("font-size", `${step.size}px`);
    element.style.setProperty("line-height", `${step.line}px`);
    if (weight) element.style.setProperty("font-weight", String(weight));
    element.dataset.phoneType = "true";
    touched.add(element);
  };
  const title = () => {
    const id = root.getAttribute("aria-labelledby");
    return id ? root.ownerDocument.getElementById(id) : root.querySelector<HTMLElement>("h1, h2, h3");
  };
  const keepsOwn = (element: Element) => {
    if (throughout) return false;
    const control = element.closest(CONTROLS);
    return element.matches(TEXT_FIELDS) || (control !== null && root.contains(control));
  };
  const apply = () => {
    const heading = title();
    if (heading && root.contains(heading) && !heading.dataset.phoneType) set(heading, TITLE, TITLE.weight);
    if (throughout) {
      for (const field of root.querySelectorAll<HTMLElement>(TEXT_FIELDS)) {
        if (field.dataset.phoneType) continue;
        if (field.tagName === "INPUT" && field.getBoundingClientRect().height < FIELD_HEIGHT) field.style.setProperty("height", `${FIELD_HEIGHT}px`);
        set(field, { size: BODY.size, line: BODY.line });
      }
    }
    for (const element of root.querySelectorAll<HTMLElement>("*")) {
      if (element.dataset.phoneType || heading?.contains(element) || !hasOwnWords(element) || keepsOwn(element)) continue;
      const step = stepFor(Math.round(parseFloat(getComputedStyle(element).fontSize)));
      if (step) set(element, step);
    }
  };
  apply();
  let frame = 0;
  const observer = new MutationObserver(() => {
    if (frame) return;
    frame = requestAnimationFrame(() => { frame = 0; apply(); });
  });
  observer.observe(root, { childList: true, subtree: true, characterData: true });
  return () => {
    observer.disconnect();
    if (frame) cancelAnimationFrame(frame);
    for (const element of touched) {
      for (const property of ["font-size", "line-height", "font-weight", "height"]) element.style.removeProperty(property);
      delete element.dataset.phoneType;
    }
  };
}
