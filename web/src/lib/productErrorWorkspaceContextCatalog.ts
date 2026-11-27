import type { ProductErrorContextBase } from "@/lib/productErrorContextCatalog";
import type { ProductErrorLogLevel, ProductLogOperation } from "@/lib/safeErrorLog";

function context(
  logOperation: ProductLogOperation = "api.request",
  level: ProductErrorLogLevel = "error",
): ProductErrorContextBase {
  return { level, logOperation };
}

export const WORKSPACE_ERROR_CONTEXTS = {
  "accountProfile.update": context(),
  "workspace.create": context(),
  "workspace.delete": context(),
  "workspace.member.add": context(),
  "workspace.member.revoke": context(),
  "workspace.settings.update": context(),
  "workspaceImport.cancel": context("workspace-import.request"),
  "workspaceImport.job": context("workspace-import.request"),
  "workspaceImport.start": context("workspace-import.request"),
  "workspaceImport.status": context("workspace-import.request"),
  "workspaceExport.cancel": context("workspace-export.request"),
  "workspaceExport.download": context("workspace-export.request"),
  "workspaceExport.job": context("workspace-export.request"),
  "workspaceExport.poll": context("workspace-export.request"),
  "workspaceExport.retry": context("workspace-export.request"),
  "workspaceExport.start": context("workspace-export.request"),
  "workspaceExport.status": context("workspace-export.request"),
} as const satisfies Record<string, ProductErrorContextBase>;
