import chromeStyles from "@/components/WorkspaceSettingsChrome.module.css";

/** What the last organization setting's save refused, and which save it was. */
export type EnterpriseActionResult = { action: string; error: string };

/**
 * A refusal and its reason where the form is, under the section whose action
 * it was (2026-10-16: a refused SAML setting said nothing, as every result
 * showed at the top of the section). An action is one of actions, or begins
 * with one ending in ":". A save that went through shows in its row.
 */
export function EnterpriseActionFeedback({ result, actions }: { result: EnterpriseActionResult; actions: readonly string[] }) {
  const mine = actions.some((action) => result.action === action || (action.endsWith(":") && result.action.startsWith(action))
    || result.action.startsWith(`${action}:`));
  if (!mine || !result.error) return null;
  return <div className={chromeStyles.notice} role="alert" data-enterprise-feedback="error">{result.error}</div>;
}
