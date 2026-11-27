import { generateLocalDataKey, type LocalDataKey } from "./browserCrypto";
import { claimLocalKeyValue } from "./localKeyDatabase";
import {
  createLocalSecretBox,
  isLocalSecretBoxKey,
  type LocalSecretBox,
} from "./localSecretBox";

const LOCAL_DEVICE_SECRET_KEY = "primary";

export interface LocalDeviceSecretBoxOptions {
  beforeSchemaMigration?: (version: number) => void;
  factory?: IDBFactory;
  maxDatabaseNameCharacters: number;
}

export interface LocalDeviceSecretBoxResult {
  box: LocalSecretBox;
  created: boolean;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

export async function createLocalDeviceSecretBox(
  name: string,
  options: LocalDeviceSecretBoxOptions,
): Promise<LocalDeviceSecretBoxResult | null> {
  assertPositiveSafeInteger(
    options.maxDatabaseNameCharacters,
    "maxDatabaseNameCharacters",
  );
  if (!name) return null;
  const databaseName = `${name}::keys`;
  if (databaseName.length > options.maxDatabaseNameCharacters) {
    throw new RangeError("Local device-key database name exceeds its configured bound.");
  }
  try {
    // The browser's key, or its bytes where the page has no browser crypto (browserCrypto.ts).
    const claim = await claimLocalKeyValue<LocalDataKey>({
      ...(options.beforeSchemaMigration
        ? { beforeSchemaMigration: options.beforeSchemaMigration }
        : {}),
      createValue: generateLocalDataKey,
      databaseName,
      ...(options.factory === undefined ? {} : { factory: options.factory }),
      isValid: isLocalSecretBoxKey,
      key: LOCAL_DEVICE_SECRET_KEY,
    });
    return claim
      ? { box: createLocalSecretBox(claim.value), created: claim.created }
      : null;
  } catch {
    return null;
  }
}
