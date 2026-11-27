import type { ProductErrorLogLevel, ProductLogOperation } from "@/lib/safeErrorLog";
import { AUTOMATION_ERROR_CONTEXTS } from "@/lib/productErrorAutomationContextCatalog";
import { WORKSPACE_ERROR_CONTEXTS } from "@/lib/productErrorWorkspaceContextCatalog";
import { ADMINISTRATOR_ERROR_CONTEXTS } from "@/lib/productErrorAdministratorContextCatalog";
// Error texts live in the productErrors catalog so each reader sees them in
// their language; a text names its catalog key.
export type ProductErrorText = { readonly key: string };

export type ProductErrorContextBase = {
  level: ProductErrorLogLevel;
  logOperation: ProductLogOperation;
  transportRetry?: "safe-read";
};

export type ProductErrorContextSpec = ProductErrorContextBase & {
  deniedEffect: ProductErrorText;
  operation: ProductErrorText;
  stateCheckAction: ProductErrorText;
  uncertainEffect: ProductErrorText;
};

function context(
  logOperation: ProductLogOperation = "api.request",
  level: ProductErrorLogLevel = "error",
): ProductErrorContextBase {
  return { level, logOperation };
}

function safeReadContext(): ProductErrorContextBase {
  return { ...context("api.request", "warn"), transportRetry: "safe-read" };
}

// A context's operation, effects and state check are catalogued under
// contexts.<context name>.
function describeContexts<T extends Record<string, ProductErrorContextBase>>(
  bases: T,
): { [K in keyof T]: ProductErrorContextSpec } {
  return Object.fromEntries(Object.entries(bases).map(([name, base]) => [name, {
    ...base,
    deniedEffect: { key: `contexts.${name}.deniedEffect` },
    operation: { key: `contexts.${name}.operation` },
    stateCheckAction: { key: `contexts.${name}.stateCheckAction` },
    uncertainEffect: { key: `contexts.${name}.uncertainEffect` },
  }])) as { [K in keyof T]: ProductErrorContextSpec };
}

export const PRODUCT_ERROR_CONTEXTS = describeContexts({
  "accountPasskeys.read": safeReadContext(),
  "accountPasskeys.update": context(),
  "auth.passkey": context("auth.request", "warn"),
  "auth.deviceCode": context("auth.request", "warn"),
  "accountEmails.read": safeReadContext(),
  "accountEmails.update": context(),
  "accountProfile.read": safeReadContext(),
  "accountNotifications.read": safeReadContext(),
  "accountNotifications.update": context(),
  "accountSecurity.read": safeReadContext(),
  "accountSecurity.password": context(),
  "accountSecurity.mfa": context(),
  "accountSecurity.recovery": context(),
  "accountSecurity.sessionRevoke": context(),
  "auth.bootstrap": context("auth.request", "warn"),
  "auth.mfa": context("auth.request", "warn"),
  "auth.emailVerification": context("auth.request", "warn"),
  "auth.passwordResetConfirm": context("auth.request", "warn"),
  "auth.passwordResetRequest": context("auth.request", "warn"),
  "auth.providerExchange": context("auth.request", "warn"),
  "auth.serverSetup": context("auth.request", "warn"),
  "auth.signIn": context("auth.request", "warn"),
  "auth.signUp": context("auth.request", "warn"),
  "contentGraph.read": context(),
  "contentGraph.update": context(),
  "file.delete": context(),
  "file.upload": context(),
  "enterpriseDlp.policyUpdate": context(),
  "enterpriseSaml.domainVerification": context(),
  "page.bootstrap": context(),
  "runtimeConfig.read": safeReadContext(),
  "page.create": context(),
  "page.title.update": context(),
  "pageMutation.admission": context("outbox.persistence"),
  "pageMutation.legacy": context("outbox.persistence"),
  "pageMutation.refresh": context(),
  "pageMutation.replay": context("outbox.persistence"),
  "publicForm.definitionRead": context("public-form.request", "warn"),
  "publicForm.authRestore": context("public-form.request", "warn"),
  "publicForm.optionsRead": context("public-form.request", "warn"),
  "publicForm.configurationWrite": context("public-form.request", "warn"),
  "publicForm.responseSubmit": context("public-form.request", "warn"),
  "syncStatus.pendingRead": context("sync-status.request", "warn"),
  "syncStatus.flush": context("sync-status.request", "warn"),
  "syncStatus.retry": context("sync-status.request", "warn"),
  "importedPeople.scan": context("imported-people.request", "warn"),
  "importedPeople.apply": context("imported-people.request", "warn"),
  "importedPeople.refresh": context("imported-people.request", "warn"),
  "pageAnalytics.analyticsRead": context("page-analytics.request", "warn"),
  "pageAnalytics.preferencesRead": context("page-analytics.request", "warn"),
  "pageAnalytics.preferencesUpdate": context("page-analytics.request", "warn"),
  "pageAnalytics.policyUpdate": context("page-analytics.request", "warn"),
  "notionAdmin.list": context("notion-admin-api.request", "warn"),
  "notionAdmin.create": context("notion-admin-api.request", "warn"),
  "notionAdmin.revoke": context("notion-admin-api.request", "warn"),
  "notionAdmin.clipboard": context("notion-admin-api.request", "warn"),
  "publicContent.sharedPage": context("api.request", "warn"),
  "publicContent.site": context("api.request", "warn"),
  "publicContent.search": context("api.request", "warn"),
  ...AUTOMATION_ERROR_CONTEXTS,
  ...WORKSPACE_ERROR_CONTEXTS,
  ...ADMINISTRATOR_ERROR_CONTEXTS,
} satisfies Record<string, ProductErrorContextBase>);

export type ProductErrorContext = keyof typeof PRODUCT_ERROR_CONTEXTS;
