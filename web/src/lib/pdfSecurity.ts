import { aesCbcDecrypt, aesCbcEncryptBlocks, digest } from "./browserCrypto";

/**
 * Opens PDFs protected by the Standard security handler when their user
 * password is empty: such files open for anyone, and their owner password
 * only limits printing or copying. The file key is derived and checked as
 * in ISO 32000 (7.6.4, algorithms 2, 2.A, 2.B, 4, 5 and 6), and each stream
 * is decrypted with RC4, AES-128 or AES-256 (the browser's crypto, or script
 * on a page without it — browserCrypto.ts). A file that needs a user
 * password gets no decryptor.
 */

export type PdfCryptMethod = "none" | "rc4" | "aes128" | "aes256";

export type PdfSecurityParameters = {
  version: number;
  revision: number;
  keyBits: number;
  owner: Uint8Array;
  user: Uint8Array;
  userEncryptedKey: Uint8Array;
  permissions: number;
  encryptMetadata: boolean;
  streamMethod: PdfCryptMethod;
  firstId: Uint8Array;
};

export type PdfStreamDecryptor = {
  decrypt(data: Uint8Array, num: number, gen: number): Promise<Uint8Array>;
};

const PASSWORD_PADDING = Uint8Array.from([
  0x28, 0xbf, 0x4e, 0x5e, 0x4e, 0x75, 0x8a, 0x41, 0x64, 0x00, 0x4e, 0x56, 0xff, 0xfa, 0x01, 0x08,
  0x2e, 0x2e, 0x00, 0xb6, 0xd0, 0x68, 0x3e, 0x80, 0x2f, 0x0c, 0xa9, 0xfe, 0x64, 0x53, 0x69, 0x7a,
]);

const MD5_SHIFTS = [7, 12, 17, 22, 5, 9, 14, 20, 4, 11, 16, 23, 6, 10, 15, 21];
const MD5_CONSTANTS = Array.from({ length: 64 }, (_, index) => Math.floor(Math.abs(Math.sin(index + 1)) * 2 ** 32) | 0);

function concat(...parts: Uint8Array[]) {
  const out = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

export function md5(data: Uint8Array) {
  const padded = new Uint8Array(((data.length + 72) >>> 6) << 6);
  padded.set(data);
  padded[data.length] = 0x80;
  const view = new DataView(padded.buffer);
  view.setUint32(padded.length - 8, (data.length * 8) >>> 0, true);
  view.setUint32(padded.length - 4, Math.floor(data.length / 0x20000000), true);
  let a0 = 0x67452301;
  let b0 = 0xefcdab89 | 0;
  let c0 = 0x98badcfe | 0;
  let d0 = 0x10325476;
  for (let offset = 0; offset < padded.length; offset += 64) {
    let a = a0;
    let b = b0;
    let c = c0;
    let d = d0;
    for (let index = 0; index < 64; index += 1) {
      let f: number;
      let word: number;
      if (index < 16) {
        f = (b & c) | (~b & d);
        word = index;
      } else if (index < 32) {
        f = (d & b) | (~d & c);
        word = (5 * index + 1) % 16;
      } else if (index < 48) {
        f = b ^ c ^ d;
        word = (3 * index + 5) % 16;
      } else {
        f = c ^ (b | ~d);
        word = (7 * index) % 16;
      }
      f = (f + a + MD5_CONSTANTS[index]! + view.getInt32(offset + word * 4, true)) | 0;
      a = d;
      d = c;
      c = b;
      const shift = MD5_SHIFTS[(index >> 4) * 4 + (index % 4)]!;
      b = (b + ((f << shift) | (f >>> (32 - shift)))) | 0;
    }
    a0 = (a0 + a) | 0;
    b0 = (b0 + b) | 0;
    c0 = (c0 + c) | 0;
    d0 = (d0 + d) | 0;
  }
  const out = new Uint8Array(16);
  const result = new DataView(out.buffer);
  [a0, b0, c0, d0].forEach((word, index) => result.setInt32(index * 4, word, true));
  return out;
}

export function rc4(key: Uint8Array, data: Uint8Array) {
  const state = Uint8Array.from({ length: 256 }, (_, index) => index);
  for (let index = 0, j = 0; index < 256; index += 1) {
    j = (j + state[index]! + key[index % key.length]!) & 255;
    [state[index], state[j]] = [state[j]!, state[index]!];
  }
  const out = new Uint8Array(data.length);
  for (let index = 0, i = 0, j = 0; index < data.length; index += 1) {
    i = (i + 1) & 255;
    j = (j + state[i]!) & 255;
    [state[i], state[j]] = [state[j]!, state[i]!];
    out[index] = data[index]! ^ state[(state[i]! + state[j]!) & 255]!;
  }
  return out;
}

/** Algorithm 2.B: the revision 6 hash of a password with a salt. */
async function revision6Hash(password: Uint8Array, salt: Uint8Array, userData: Uint8Array) {
  let key = await digest("SHA-256", concat(password, salt, userData));
  for (let round = 0; ; round += 1) {
    const block = concat(password, key, userData);
    const repeated = new Uint8Array(block.length * 64);
    for (let copy = 0; copy < 64; copy += 1) repeated.set(block, copy * block.length);
    const encrypted = await aesCbcEncryptBlocks(key.subarray(0, 16), key.subarray(16, 32), repeated);
    const remainder = encrypted.subarray(0, 16).reduce((sum, byte) => sum + byte, 0) % 3;
    key = await digest(remainder === 0 ? "SHA-256" : remainder === 1 ? "SHA-384" : "SHA-512", encrypted);
    if (round >= 63 && encrypted[encrypted.length - 1]! <= round + 1 - 32) break;
    if (round > 2048) break;
  }
  return key.subarray(0, 32);
}

function equalBytes(left: Uint8Array, right: Uint8Array) {
  return left.length === right.length && left.every((byte, index) => byte === right[index]);
}

async function revision4FileKey(parameters: PdfSecurityParameters) {
  const length = parameters.revision === 2 ? 5 : Math.max(5, Math.min(16, parameters.keyBits / 8));
  const permissions = new Uint8Array(4);
  new DataView(permissions.buffer).setInt32(0, parameters.permissions, true);
  let key = md5(concat(PASSWORD_PADDING, parameters.owner.subarray(0, 32), permissions, parameters.firstId,
    parameters.revision >= 4 && !parameters.encryptMetadata ? Uint8Array.of(255, 255, 255, 255) : new Uint8Array()));
  if (parameters.revision >= 3) for (let round = 0; round < 50; round += 1) key = md5(key.subarray(0, length));
  key = key.subarray(0, length);
  if (parameters.revision === 2) return equalBytes(rc4(key, PASSWORD_PADDING), parameters.user.subarray(0, 32)) ? key : null;
  let check = rc4(key, md5(concat(PASSWORD_PADDING, parameters.firstId)));
  for (let round = 1; round <= 19; round += 1) check = rc4(key.map((byte) => byte ^ round), check);
  return equalBytes(check.subarray(0, 16), parameters.user.subarray(0, 16)) ? key : null;
}

async function revision6FileKey(parameters: PdfSecurityParameters) {
  const { user } = parameters;
  if (user.length < 48 || parameters.userEncryptedKey.length < 32) return null;
  const empty = new Uint8Array();
  const hash = parameters.revision === 5
    ? (salt: Uint8Array) => digest("SHA-256", salt)
    : (salt: Uint8Array) => revision6Hash(empty, salt, empty);
  if (!equalBytes(await hash(user.subarray(32, 40)), user.subarray(0, 32))) return null;
  return aesCbcDecrypt(await hash(user.subarray(40, 48)), new Uint8Array(16), parameters.userEncryptedKey.subarray(0, 32), false);
}

/** The stream decryptor for an empty user password, or null when one is needed. */
export async function createPdfStreamDecryptor(parameters: PdfSecurityParameters): Promise<PdfStreamDecryptor | null> {
  if (parameters.revision < 2 || parameters.revision > 6) return null;
  const fileKey = parameters.revision >= 5 ? await revision6FileKey(parameters) : await revision4FileKey(parameters);
  if (!fileKey) return null;
  const method = parameters.streamMethod;
  return {
    async decrypt(data, num, gen) {
      if (method === "none") return data;
      if (method === "aes256") return aesCbcDecrypt(fileKey, data.subarray(0, 16), data.subarray(16), true);
      const objectKey = md5(concat(fileKey, Uint8Array.of(num & 255, (num >> 8) & 255, (num >> 16) & 255, gen & 255, (gen >> 8) & 255),
        method === "aes128" ? Uint8Array.of(0x73, 0x41, 0x6c, 0x54) : new Uint8Array()))
        .subarray(0, Math.min(fileKey.length + 5, 16));
      if (method === "rc4") return rc4(objectKey, data);
      return aesCbcDecrypt(objectKey, data.subarray(0, 16), data.subarray(16), true);
    },
  };
}
