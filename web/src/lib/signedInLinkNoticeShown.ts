// Whether the notice about a link this signed-in browser didn't use is up
// (SignedInLinkNotice.tsx): what the app would open by itself meanwhile, as
// the window asking someone without a workspace to make one, waits for it,
// so the notice isn't hidden under it.

let shown = false;
const listeners = new Set<() => void>();

export function signedInLinkNoticeShown() {
  return shown;
}

export function subscribeSignedInLinkNotice(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function setSignedInLinkNoticeShown(next: boolean) {
  if (shown === next) return;
  shown = next;
  for (const listener of listeners) listener();
}
