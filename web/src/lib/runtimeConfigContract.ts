import type { PublicRuntimeConfig } from "../runtimeConfigWire.generated";

export type { PublicRuntimeConfig } from "../runtimeConfigWire.generated";

export type RuntimeConfigTransport = () => Promise<PublicRuntimeConfig>;

export function runtimeEmailRecoveryAvailable(value: unknown): boolean {
  return Boolean(
    value
    && typeof value === "object"
    && (value as { emailRecoveryAvailable?: unknown }).emailRecoveryAvailable === true,
  );
}
