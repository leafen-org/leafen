export type LiveLeafenSourceAuthority = {
  baseUrl: string;
  authorization: string;
  credentials: RequestCredentials;
};

type LiveLeafenAuthorityError = Error & {
  code: "LEAFEN_LIVE_TRANSFER_SOURCE_AUTH_REQUIRED" | "LEAFEN_LIVE_TRANSFER_SOURCE_INVALID";
  stage: "source-authority";
  status: 400 | 401;
};

function authorityError(
  code: LiveLeafenAuthorityError["code"],
  status: LiveLeafenAuthorityError["status"],
  message: string,
): LiveLeafenAuthorityError {
  return Object.assign(new Error(message), {
    code,
    stage: "source-authority" as const,
    status,
  });
}

function httpOrigin(value: string, base?: string) {
  let parsed: URL;
  try {
    parsed = new URL(value, base);
  } catch {
    throw authorityError(
      "LEAFEN_LIVE_TRANSFER_SOURCE_INVALID",
      400,
      "The source Leafen server address is not valid.",
    );
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw authorityError(
      "LEAFEN_LIVE_TRANSFER_SOURCE_INVALID",
      400,
      "The source Leafen server must be an HTTP or HTTPS address.",
    );
  }
  return parsed.origin;
}

export function resolveLiveLeafenSourceAuthority(input: {
  appOrigin: string;
  sourceUrl: string;
  currentToken: string;
  sourceToken: string;
}): LiveLeafenSourceAuthority {
  const appOrigin = httpOrigin(input.appOrigin);
  const baseUrl = httpOrigin(input.sourceUrl.trim() || appOrigin, appOrigin);
  const explicitToken = input.sourceToken.trim();
  if (baseUrl === appOrigin) {
    const token = input.currentToken.trim();
    if (!token) {
      throw authorityError(
        "LEAFEN_LIVE_TRANSFER_SOURCE_AUTH_REQUIRED",
        401,
        "The current Leafen session has no token.",
      );
    }
    return {
      baseUrl,
      authorization: `Bearer ${token}`,
      credentials: "same-origin",
    };
  }
  if (!explicitToken) {
    throw authorityError(
      "LEAFEN_LIVE_TRANSFER_SOURCE_AUTH_REQUIRED",
      401,
      "Another Leafen server needs the source server's access token.",
    );
  }
  return {
    baseUrl,
    authorization: `Bearer ${explicitToken}`,
    credentials: "omit",
  };
}

type LiveLeafenStreamError = Error & {
  code: "LEAFEN_LIVE_TRANSFER_STREAM_INVALID";
  stage: "source-download" | "target-upload";
  status: 502;
};

function streamError(
  stage: LiveLeafenStreamError["stage"],
  message: string,
): LiveLeafenStreamError {
  return Object.assign(new Error(message), {
    code: "LEAFEN_LIVE_TRANSFER_STREAM_INVALID" as const,
    stage,
    status: 502 as const,
  });
}

export async function streamLiveLeafenArchiveParts(input: {
  archiveBytes: number;
  body: ReadableStream<Uint8Array> | null;
  nextPart: number;
  partBytes: number;
  receivedBytes: number;
  digestPart: (bytes: Uint8Array<ArrayBuffer>) => Promise<string>;
  putPart: (
    partNumber: number,
    bytes: Uint8Array<ArrayBuffer>,
    sha256: string,
  ) => Promise<void>;
  onProgress?: (receivedBytes: number, archiveBytes: number) => void;
  signal?: AbortSignal;
}) {
  const expectedOffset = Math.min(input.nextPart * input.partBytes, input.archiveBytes);
  if (
    !Number.isSafeInteger(input.archiveBytes)
    || input.archiveBytes <= 0
    || !Number.isSafeInteger(input.partBytes)
    || input.partBytes <= 0
    || !Number.isSafeInteger(input.nextPart)
    || input.nextPart < 0
    || input.receivedBytes !== expectedOffset
    || input.receivedBytes > input.archiveBytes
  ) {
    throw streamError("target-upload", "The resume position does not match an archive boundary.");
  }
  if (input.receivedBytes === input.archiveBytes) return input.receivedBytes;
  if (!input.body) {
    throw streamError("source-download", "The source Leafen server did not return an archive stream.");
  }

  const reader = input.body.getReader();
  let sourceChunk: Uint8Array<ArrayBufferLike> = new Uint8Array(0);
  let sourceOffset = 0;
  let receivedBytes = input.receivedBytes;
  let partNumber = input.nextPart;
  try {
    while (receivedBytes < input.archiveBytes) {
      if (input.signal?.aborted) throw new DOMException("Aborted", "AbortError");
      const expectedPartBytes = Math.min(input.partBytes, input.archiveBytes - receivedBytes);
      const part = new Uint8Array(expectedPartBytes);
      let partOffset = 0;
      while (partOffset < expectedPartBytes) {
        if (sourceOffset >= sourceChunk.byteLength) {
          const next = await reader.read();
          if (next.done) {
            throw streamError("source-download", "The source Leafen archive ended early.");
          }
          sourceChunk = next.value;
          sourceOffset = 0;
          if (sourceChunk.byteLength === 0) continue;
        }
        const copyBytes = Math.min(
          expectedPartBytes - partOffset,
          sourceChunk.byteLength - sourceOffset,
        );
        part.set(sourceChunk.subarray(sourceOffset, sourceOffset + copyBytes), partOffset);
        sourceOffset += copyBytes;
        partOffset += copyBytes;
      }
      const digest = await input.digestPart(part);
      await input.putPart(partNumber, part, digest);
      receivedBytes += part.byteLength;
      partNumber += 1;
      input.onProgress?.(receivedBytes, input.archiveBytes);
    }
    if (sourceOffset < sourceChunk.byteLength) {
      throw streamError("source-download", "The source Leafen archive is larger than its known size.");
    }
    for (;;) {
      const tail = await reader.read();
      if (tail.done) break;
      if (tail.value.byteLength > 0) {
        throw streamError("source-download", "The source Leafen archive is larger than its known size.");
      }
    }
    return receivedBytes;
  } finally {
    reader.releaseLock();
  }
}
