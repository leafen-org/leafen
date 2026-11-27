import { i18next } from "@/i18n";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { isServerUnreachable } from "@/lib/settingsErrors";

const contexts = {
  contentGraphRead: "contentGraph.read",
  pageCreate: "page.create",
  pageTitleUpdate: "page.title.update",
  contentGraphUpdate: "contentGraph.update",
} as const;

export type AuthenticatedProductErrorContext = keyof typeof contexts;

// With the server unreachable, a page that couldn't open or be made, or a
// change not saved, is said in one line: the page has no connection bar
// yet to say it, and the reference is the log's (it was logged where the
// request failed).
const unreachable: Record<AuthenticatedProductErrorContext, string> = {
  contentGraphRead: "settingsErrors:unreachableOpenPage",
  pageCreate: "settingsErrors:unreachableCreatePage",
  pageTitleUpdate: "settingsErrors:unreachableSave",
  contentGraphUpdate: "settingsErrors:unreachableSave",
};

export function authenticatedProductErrorMessage(
  error: unknown,
  kind: AuthenticatedProductErrorContext,
): string {
  if (isServerUnreachable(error)) return i18next.t(unreachable[kind]);
  return productErrorMessage(error, contexts[kind]);
}
