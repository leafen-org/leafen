export type DurableJobMemoryBounds = {
  maxIdentityCharacters: number;
  maxStorageKeyCharacters: number;
  maxSerializedCharacters: number;
};

export type DurableJobMemoryDefinition<Value> = {
  bounds: DurableJobMemoryBounds;
  decode: (value: string) => Value | null;
  encode: (value: Value) => string | null;
  identityPartCount: number;
  storagePrefix: string;
};

export type DurableJobMemory<Value> = {
  clear(identity: readonly string[]): boolean;
  read(identity: readonly string[]): Value | null;
  remember(identity: readonly string[], value: Value): boolean;
  storageKey(identity: readonly string[]): string | null;
};

const STORAGE_PREFIX = /^leafen:[a-z][a-z0-9-]{0,63}$/u;

function positiveSafeInteger(value: number, name: string) {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${name} must be a positive safe integer.`);
  }
}

function assertDefinition<Value>(definition: DurableJobMemoryDefinition<Value>) {
  positiveSafeInteger(definition.identityPartCount, "identityPartCount");
  positiveSafeInteger(
    definition.bounds.maxIdentityCharacters,
    "maxIdentityCharacters",
  );
  positiveSafeInteger(
    definition.bounds.maxStorageKeyCharacters,
    "maxStorageKeyCharacters",
  );
  positiveSafeInteger(
    definition.bounds.maxSerializedCharacters,
    "maxSerializedCharacters",
  );
  if (!STORAGE_PREFIX.test(definition.storagePrefix)) {
    throw new RangeError("storagePrefix must be a bounded Leafen storage prefix.");
  }
}

function browserStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function removeStoredValue(storage: Storage, key: string) {
  try {
    storage.removeItem(key);
  } catch {
    // The durable server job remains authoritative when browser cleanup fails.
  }
}

export function createDurableJobMemory<Value>(
  definition: DurableJobMemoryDefinition<Value>,
): DurableJobMemory<Value> {
  assertDefinition(definition);

  const storageKey = (identity: readonly string[]) => {
    if (identity.length !== definition.identityPartCount) return null;
    let identityCharacters = 0;
    const encoded = [];
    for (const part of identity) {
      if (typeof part !== "string" || !part) return null;
      identityCharacters += part.length;
      if (identityCharacters > definition.bounds.maxIdentityCharacters) return null;
      try {
        encoded.push(encodeURIComponent(part));
      } catch {
        return null;
      }
    }
    const key = [definition.storagePrefix, ...encoded].join(":");
    return key.length <= definition.bounds.maxStorageKeyCharacters ? key : null;
  };

  return {
    clear(identity) {
      const key = storageKey(identity);
      const storage = key ? browserStorage() : null;
      if (!key || !storage) return false;
      try {
        storage.removeItem(key);
        return true;
      } catch {
        return false;
      }
    },

    read(identity) {
      const key = storageKey(identity);
      const storage = key ? browserStorage() : null;
      if (!key || !storage) return null;
      let serialized: string | null;
      try {
        serialized = storage.getItem(key);
      } catch {
        return null;
      }
      if (serialized === null) return null;
      if (
        serialized.length === 0
        || serialized.length > definition.bounds.maxSerializedCharacters
      ) {
        removeStoredValue(storage, key);
        return null;
      }
      try {
        const value = definition.decode(serialized);
        if (value !== null) return value;
      } catch {
        // Invalid optional reconnection memory is removed below.
      }
      removeStoredValue(storage, key);
      return null;
    },

    remember(identity, value) {
      const key = storageKey(identity);
      const storage = key ? browserStorage() : null;
      if (!key || !storage) return false;
      let serialized: string | null;
      try {
        serialized = definition.encode(value);
      } catch {
        return false;
      }
      if (
        typeof serialized !== "string"
        || serialized.length === 0
        || serialized.length > definition.bounds.maxSerializedCharacters
      ) {
        return false;
      }
      try {
        storage.setItem(key, serialized);
        return true;
      } catch {
        return false;
      }
    },

    storageKey,
  };
}
