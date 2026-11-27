import type { ProductErrorContextBase } from "@/lib/productErrorContextCatalog";

export const AUTOMATION_ERROR_CONTEXTS = {
  "databaseAutomation.write": { logOperation: "api.request", level: "error" },
  "interactiveAutomation.execute": { logOperation: "api.request", level: "error" },
} as const satisfies Record<string, ProductErrorContextBase>;
