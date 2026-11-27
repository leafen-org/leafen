import { appPlatformErrorCodes } from "@/appPlatformWire.generated";
import { fetchAskingAgain, waitUnlessAborted } from "@/lib/serverBusy";

// An app's call or file save that the server turned away because the app is
// over its budget (app_platform_budget.go: 429 before any of it ran) goes
// again once the wait its Retry-After names has passed, as a busy server's
// refusal does (lib/serverBusy.ts), for so long in all; past that the app
// gets the answer as it is (2026-11-24: the Drive's Obsidian card ended its
// read of a 1,200-folder vault on the first refusal it met).

/** How long a call is asked again in all while the app's budget turns it away, in ms. */
const BUDGET_WAIT_MS = 60_000;

/** Sends an app's request, asked again while the server is busy or the app's budget turns it away; the last answer, whatever it is. */
export async function askServerForApp(send: () => Promise<Response>, signal?: AbortSignal | null): Promise<Response> {
  const started = Date.now();
  for (;;) {
    const response = await fetchAskingAgain(send, signal);
    if (response.status !== 429) return response;
    const body = await response.clone().json().catch(() => null) as { error?: { code?: unknown } } | null;
    const wait = Math.max(1, Number(response.headers.get("retry-after")) || 1) * 1000;
    if (body?.error?.code !== appPlatformErrorCodes.appBudgetLimited || Date.now() - started + wait > BUDGET_WAIT_MS) return response;
    await waitUnlessAborted(wait, signal);
  }
}
