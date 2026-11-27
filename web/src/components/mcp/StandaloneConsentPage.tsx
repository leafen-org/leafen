import { Suspense } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { ProductLoadingScreen } from "@/components/loading/ProductLoadingScreen";
import type { StandaloneConsent } from "@/lib/standaloneConsent";
import type { Session } from "@/types";

// The pages a signed-in person answers on their own, outside the workspace
// (lib/standaloneConsent.ts): a program's standard sign-in asking to connect.

const MCPAuthorizationPage = lazyScreen(() => import("./MCPAuthorizationPage").then((module) => ({ default: module.MCPAuthorizationPage })), { entry: true });
const NotionAuthorizationPage = lazyScreen(() => import("./NotionAuthorizationPage").then((module) => ({ default: module.NotionAuthorizationPage })), { entry: true });

export function StandaloneConsentPage({ consent, session }: { consent: StandaloneConsent; session: Session }) {
  return (
    <Suspense fallback={<ProductLoadingScreen source="workspace" />}>
      {consent.kind === "mcp" ? <MCPAuthorizationPage authorizationRequest={consent.value} session={session} />
        : <NotionAuthorizationPage authorizationRequest={consent.value} session={session} />}
    </Suspense>
  );
}
