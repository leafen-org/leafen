import { i18next } from "@/i18n";
import type { GraphOperation } from "@/contentGraph";
import { graphWireErrorCodes } from "@/contentGraphWire.generated";

/**
 * Why the server refused a change to a database's properties that the app
 * showed at once, in the words its screens use for the same rule, or "" when
 * it named none the person can act on. The value goes back as the server
 * holds it; the notice says why rather than only that the change stopped.
 * Notion's ID rules (database_unique_id_rules.go) refuse a prefix another
 * data source took in the meantime (409) and a second ID property (400),
 * which the prefix editor and the type menus ask about first but a change
 * elsewhere can overtake.
 */
export function propertyWriteRefusalReason(error: unknown, operations?: readonly GraphOperation[]): string {
  const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
  const status = error && typeof error === "object" ? Reflect.get(error, "status") : undefined;
  if (code !== graphWireErrorCodes.changeInvalid) return "";
  const ids = (operations ?? []).flatMap((operation) => (
    operation.kind === "upsertProperty" && operation.property.type === "unique_id" ? [operation.property] : []
  ));
  if (status === 409) {
    // It names the prefix it refused, which the change held.
    const prefix = ids.map((property) => property.config.idPrefix).find((value) => typeof value === "string" && value.trim());
    return typeof prefix === "string"
      ? i18next.t("propertyTypeConfig:idPrefixTaken", { prefix: prefix.trim().toUpperCase() })
      : "";
  }
  // A kept change refused later is known by its answer alone: a change's 400
  // under this code is the ID count's.
  return status === 400 && (!operations || ids.length) ? i18next.t("tableView:dimmedTypeTips.unique_id") : "";
}
