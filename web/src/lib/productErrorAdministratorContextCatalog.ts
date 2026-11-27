import type { ProductErrorContextBase } from "@/lib/productErrorContextCatalog";

function context(level: ProductErrorContextBase["level"] = "error"): ProductErrorContextBase {
  return { level, logOperation: "api.request" };
}

function safeReadContext(): ProductErrorContextBase {
  return { ...context("warn"), transportRetry: "safe-read" };
}

export const ADMINISTRATOR_ERROR_CONTEXTS = {
  "administratorSettings.read": safeReadContext(),
  "administratorSettings.save": context(),
  "administratorReport.read": safeReadContext(),
  "administratorReport.refresh": context(),
  "administratorBackup.status": safeReadContext(),
  "administratorBackup.create": context(),
  "administratorBackup.retry": context(),
  "administratorBackup.cancel": context(),
  "administratorBackup.download": safeReadContext(),
  "administratorScheduledBackup.read": safeReadContext(),
  "administratorScheduledBackup.save": context(),
  "administratorScheduledBackup.restore": context(),
  "administratorRequestLog.read": safeReadContext(),
  "administratorRequestLog.save": context(),
  "administratorNetworkProxy.read": safeReadContext(),
  "administratorNetworkProxy.save": context(),
} as const satisfies Record<string, ProductErrorContextBase>;
