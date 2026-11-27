import { isPackagedClient } from "./packagedOrigins";

/**
 * Of a sentence's two keys, the one that names the window it is read in as
 * the person sees it: Leafen's own app ("… 이 앱이 로그인됩니다") in the
 * desktop and phone apps' window — the packaged client's page
 * (isPackagedClient, the origins the server's isPackagedClientOrigin reads
 * to name the app's requests "macOS · 리펜 앱") — and the browser ("… 이
 * 브라우저가 로그인됩니다") everywhere else. Every sentence that can show in
 * the app's window and calls the window "this browser" — sign-in, the local
 * lock — picks its words here (2026-11-23: the app's "AI로 로그인" step told
 * the person their browser would sign in). Sentences only a browser shows
 * (web push, a home-screen hint) keep their own words.
 */
export function thisWindowKey<Key extends string>(browser: Key, app: Key): Key {
  return isPackagedClient() ? app : browser;
}
