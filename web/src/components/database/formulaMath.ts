// A formula's pow (^), exp(), ln(), log10(), log2() and cbrt(), worked out
// the same way in every browser and on the server: as V8's portable code
// works them out (ieee754.cc: FDLIBM's pow, exp, log and log10, FreeBSD's
// log2 and cbrt), which is what Notion's stored values and API read (seen
// 2026-11-14: 10 ^ -5 is 0.000009999999999999999, cbrt(2.5)
// 1.3572088082974532). A browser's own Math lands a last bit away now and
// then (Chrome on a Mac: 10 ** -5 is 0.00001, log10(1.1)
// 0.04139268515822508), which would leave a cell, its filter and the server
// apart. The server works them out the same way (database_formula_math.go,
// database_formula_math_logs.go).

const scratch = new DataView(new ArrayBuffer(8));

function high(x: number) {
  scratch.setFloat64(0, x);
  return scratch.getInt32(0);
}

function low(x: number) {
  scratch.setFloat64(0, x);
  return scratch.getUint32(4);
}

function withHigh(x: number, word: number) {
  scratch.setFloat64(0, x);
  scratch.setInt32(0, word);
  return scratch.getFloat64(0);
}

function withLow(x: number, word: number) {
  scratch.setFloat64(0, x);
  scratch.setUint32(4, word);
  return scratch.getFloat64(0);
}

function bits(highWord: number, lowWord: number) {
  scratch.setUint32(0, highWord >>> 0);
  scratch.setUint32(4, lowWord >>> 0);
  return scratch.getFloat64(0);
}

const TWO53 = 9007199254740992;
const TWO54 = bits(0x43500000, 0);
const HUGE = 1.0e300;
const TINY = 1.0e-300;
const LN2_HI = bits(0x3FE62E42, 0xFEE00000);
const LN2_LO = bits(0x3DEA39EF, 0x35793C76);
const LG = [
  bits(0x3FE55555, 0x55555593), bits(0x3FD99999, 0x9997FA04), bits(0x3FD24924, 0x94229359), bits(0x3FCC71C5, 0x1D8E78AF),
  bits(0x3FC74664, 0x96CB03DE), bits(0x3FC39A09, 0xD078C69F), bits(0x3FC2F112, 0xDF3E5244),
];
const P = [bits(0x3FC55555, 0x5555553E), bits(0xBF66C16C, 0x16BEBD93), bits(0x3F11566A, 0xAF25DE2C), bits(0xBEBBBD41, 0xC5D26BF1), bits(0x3E663769, 0x72BEA4D0)];

/** A number times 2^n with one rounding, as FDLIBM's scalbn. */
function scaled(x: number, n: number) {
  return n < -1000 ? x * 2 ** (n + 60) * 2 ** -60 : x * 2 ** n;
}

/** JavaScript's ** as FDLIBM's __ieee754_pow, NaN for ±1 to an infinite power. */
export function formulaPow(x: number, y: number): number {
  if (!Number.isFinite(y) && Math.abs(x) === 1 && !Number.isNaN(y)) return Number.NaN;
  const bp = [1.0, 1.5], dpH = [0.0, bits(0x3FE2B803, 0x40000000)], dpL = [0.0, bits(0x3E4CFDEB, 0x43CFD006)];
  const l1 = bits(0x3FE33333, 0x33333303), l2 = bits(0x3FDB6DB6, 0xDB6FABFF), l3 = bits(0x3FD55555, 0x518F264D);
  const l4 = bits(0x3FD17460, 0xA91D4101), l5 = bits(0x3FCD864A, 0x93C9DB65), l6 = bits(0x3FCA7E28, 0x4A454EEF);
  const lg2 = bits(0x3FE62E42, 0xFEFA39EF), lg2H = bits(0x3FE62E43, 0), lg2L = bits(0xBE205C61, 0x0CA86C39);
  const ovt = 8.0085662595372944372e-17;
  const cp = bits(0x3FEEC709, 0xDC3A03FD), cpH = bits(0x3FEEC709, 0xE0000000), cpL = bits(0xBE3E2FE0, 0x145B01F5);
  const ivH = bits(0x3FF71547, 0x60000000), ivL = bits(0x3E54AE0B, 0xF85DDF44), iv = bits(0x3FF71547, 0x652B82FE);
  const hx = high(x), lx = low(x), hy = high(y), ly = low(y);
  let ix = hx & 0x7fffffff;
  const iy = hy & 0x7fffffff;
  if ((iy | ly) === 0) return 1;
  if (ix > 0x7ff00000 || (ix === 0x7ff00000 && lx !== 0) || iy > 0x7ff00000 || (iy === 0x7ff00000 && ly !== 0)) return x + y;
  let yIsInt = 0;
  if (hx < 0) {
    if (iy >= 0x43400000) yIsInt = 2;
    else if (iy >= 0x3ff00000) {
      const k = (iy >> 20) - 0x3ff;
      if (k > 20) {
        const j = ly >>> (52 - k);
        if (((j << (52 - k)) >>> 0) === ly) yIsInt = 2 - (j & 1);
      } else if (ly === 0) {
        const j = iy >> (20 - k);
        if ((j << (20 - k)) === iy) yIsInt = 2 - (j & 1);
      }
    }
  }
  if (ly === 0) {
    if (iy === 0x7ff00000) {
      if (((ix - 0x3ff00000) | lx) === 0) return y - y;
      if (ix >= 0x3ff00000) return hy >= 0 ? y : 0;
      return hy < 0 ? -y : 0;
    }
    if (iy === 0x3ff00000) return hy < 0 ? 1 / x : x;
    if (hy === 0x40000000) return x * x;
    if (hy === 0x3fe00000 && hx >= 0) return Math.sqrt(x);
  }
  let ax = Math.abs(x);
  if (lx === 0 && (ix === 0x7ff00000 || ix === 0 || ix === 0x3ff00000)) {
    let z = ax;
    if (hy < 0) z = 1 / z;
    if (hx < 0) {
      if (((ix - 0x3ff00000) | yIsInt) === 0) z = (z - z) / (z - z);
      else if (yIsInt === 1) z = -z;
    }
    return z;
  }
  let n = (hx >>> 31) ^ 1;
  if ((n | yIsInt) === 0) return (x - x) / (x - x);
  let s = 1;
  if ((n | (yIsInt - 1)) === 0) s = -1;
  let t1: number, t2: number;
  if (iy > 0x41e00000) {
    if (iy > 0x43f00000) {
      if (ix <= 0x3fefffff) return hy < 0 ? HUGE * HUGE : TINY * TINY;
      if (ix >= 0x3ff00000) return hy > 0 ? HUGE * HUGE : TINY * TINY;
    }
    if (ix < 0x3fefffff) return hy < 0 ? s * HUGE * HUGE : s * TINY * TINY;
    if (ix > 0x3ff00000) return hy > 0 ? s * HUGE * HUGE : s * TINY * TINY;
    const t = ax - 1;
    const w = (t * t) * (0.5 - t * (0.3333333333333333333333 - t * 0.25));
    const u = ivH * t;
    const v = t * ivL - w * iv;
    t1 = withLow(u + v, 0);
    t2 = v - (t1 - u);
  } else {
    n = 0;
    if (ix < 0x00100000) {
      ax *= TWO53;
      n -= 53;
      ix = high(ax);
    }
    n += (ix >> 20) - 0x3ff;
    const j = ix & 0x000fffff;
    ix = j | 0x3ff00000;
    let k = 0;
    if (j <= 0x3988E) k = 0;
    else if (j < 0xBB67A) k = 1;
    else {
      k = 0;
      n += 1;
      ix -= 0x00100000;
    }
    ax = withHigh(ax, ix);
    let u = ax - bp[k];
    let v = 1 / (ax + bp[k]);
    const ss = u * v;
    const sH = withLow(ss, 0);
    let tH = withHigh(0, ((ix >> 1) | 0x20000000) + 0x00080000 + (k << 18));
    let tL = ax - (tH - bp[k]);
    const sL = v * ((u - sH * tH) - sH * tL);
    let s2 = ss * ss;
    let r = s2 * s2 * (l1 + s2 * (l2 + s2 * (l3 + s2 * (l4 + s2 * (l5 + s2 * l6)))));
    r += sL * (sH + ss);
    s2 = sH * sH;
    tH = withLow(3.0 + s2 + r, 0);
    tL = r - ((tH - 3.0) - s2);
    u = sH * tH;
    v = sL * tH + tL * ss;
    const pH = withLow(u + v, 0);
    const pL = v - (pH - u);
    const zH = cpH * pH;
    const zL = cpL * pH + pL * cp + dpL[k];
    const t = n;
    t1 = withLow(((zH + zL) + dpH[k]) + t, 0);
    t2 = zL - (((t1 - t) - dpH[k]) - zH);
  }
  const y1 = withLow(y, 0);
  const pL = (y - y1) * t1 + y * t2;
  let pH = y1 * t1;
  let z = pL + pH;
  let j = high(z);
  let i = low(z) | 0;
  if (j >= 0x40900000) {
    if (((j - 0x40900000) | i) !== 0 || pL + ovt > z - pH) return s * HUGE * HUGE;
  } else if ((j & 0x7fffffff) >= 0x4090cc00) {
    if ((((j - 0xc090cc00) | 0) | i) !== 0 || pL <= z - pH) return s * TINY * TINY;
  }
  i = j & 0x7fffffff;
  let k = (i >> 20) - 0x3ff;
  n = 0;
  if (i > 0x3fe00000) {
    n = j + (0x00100000 >> (k + 1));
    k = ((n & 0x7fffffff) >> 20) - 0x3ff;
    const t = withHigh(0, n & ~(0x000fffff >> k));
    n = ((n & 0x000fffff) | 0x00100000) >> (20 - k);
    if (j < 0) n = -n;
    pH -= t;
  }
  let t = withLow(pL + pH, 0);
  const u = t * lg2H;
  const v = (pL - (t - pH)) * lg2 + t * lg2L;
  z = u + v;
  const w = v - (z - u);
  t = z * z;
  t1 = z - t * (P[0] + t * (P[1] + t * (P[2] + t * (P[3] + t * P[4]))));
  const r = (z * t1) / (t1 - 2) - (w + z * w);
  z = 1 - (r - z);
  j = (high(z) + (n << 20)) | 0;
  z = (j >> 20) <= 0 ? scaled(z, n) : withHigh(z, j);
  return s * z;
}

/** Math.exp as FDLIBM's __ieee754_exp, exp(1) being E, as V8's. */
export function formulaExp(x: number): number {
  const half = [0.5, -0.5];
  const twom1000 = bits(0x01700000, 0);
  const ln2HI = [LN2_HI, -LN2_HI], ln2LO = [LN2_LO, -LN2_LO];
  const invln2 = bits(0x3FF71547, 0x652B82FE);
  let hx = high(x) >>> 0;
  const sign = hx >>> 31;
  hx &= 0x7fffffff;
  if (hx >= 0x40862E42) {
    if (hx >= 0x7ff00000) {
      if (((hx & 0xfffff) | low(x)) !== 0) return x + x;
      return sign === 0 ? x : 0;
    }
    if (x > bits(0x40862E42, 0xFEFA39EF)) return HUGE * HUGE;
    if (x < bits(0xC0874910, 0xD52D3051)) return twom1000 * twom1000;
  }
  let hi = 0, lo = 0, k = 0;
  if (hx > 0x3fd62e42) {
    if (hx < 0x3FF0A2B2) {
      if (x === 1) return Math.E;
      hi = x - ln2HI[sign];
      lo = ln2LO[sign];
      k = 1 - sign - sign;
    } else {
      k = Math.trunc(invln2 * x + half[sign]);
      const t = k;
      hi = x - t * ln2HI[0];
      lo = t * ln2LO[0];
    }
    x = hi - lo;
  } else if (hx < 0x3e300000) {
    if (HUGE + x > 1) return 1 + x;
  }
  const t = x * x;
  const c = x - t * (P[0] + t * (P[1] + t * (P[2] + t * (P[3] + t * P[4]))));
  if (k === 0) return 1 - ((x * c) / (c - 2.0) - x);
  const y = 1 - ((lo - (x * c) / (2.0 - c)) - hi);
  if (k >= -1021) return withHigh(y, (high(y) + (k << 20)) | 0);
  return withHigh(y, (high(y) + ((k + 1000) << 20)) | 0) * twom1000;
}

/** Math.log as FDLIBM's __ieee754_log. */
export function formulaLn(x: number): number {
  let hx = high(x);
  const lx = low(x);
  let k = 0;
  if (hx < 0x00100000) {
    if (((hx & 0x7fffffff) | lx) === 0) return -Infinity;
    if (hx < 0) return Number.NaN;
    k -= 54;
    x *= TWO54;
    hx = high(x);
  }
  if (hx >= 0x7ff00000) return x + x;
  k += (hx >> 20) - 1023;
  hx &= 0x000fffff;
  let i = (hx + 0x95f64) & 0x100000;
  x = withHigh(x, hx | (i ^ 0x3ff00000));
  k += i >> 20;
  const f = x - 1.0;
  const dk = k;
  if ((0x000fffff & (2 + hx)) < 3) {
    if (f === 0) return k === 0 ? 0 : dk * LN2_HI + dk * LN2_LO;
    const r = f * f * (0.5 - 0.33333333333333333 * f);
    return k === 0 ? f - r : dk * LN2_HI - ((r - dk * LN2_LO) - f);
  }
  const s = f / (2.0 + f);
  const z = s * s;
  i = hx - 0x6147a;
  const w = z * z;
  const j = 0x6b851 - hx;
  const t1 = w * (LG[1] + w * (LG[3] + w * LG[5]));
  const t2 = z * (LG[0] + w * (LG[2] + w * (LG[4] + w * LG[6])));
  i |= j;
  const r = t2 + t1;
  if (i > 0) {
    const hfsq = 0.5 * f * f;
    return k === 0 ? f - (hfsq - s * (hfsq + r)) : dk * LN2_HI - ((hfsq - (s * (hfsq + r) + dk * LN2_LO)) - f);
  }
  return k === 0 ? f - s * (f - r) : dk * LN2_HI - ((s * (f - r) - dk * LN2_LO) - f);
}

/** Math.log10 as FDLIBM's __ieee754_log10. */
export function formulaLog10(x: number): number {
  let hx = high(x);
  const lx = low(x);
  let k = 0;
  if (hx < 0x00100000) {
    if (((hx & 0x7fffffff) | lx) === 0) return -Infinity;
    if (hx < 0) return Number.NaN;
    k -= 54;
    x *= TWO54;
    hx = high(x);
  }
  if (hx >= 0x7ff00000) return x + x;
  k += (hx >> 20) - 1023;
  const i = k >>> 31;
  hx = (hx & 0x000fffff) | ((0x3ff - i) << 20);
  const y = k + i;
  x = withHigh(x, hx);
  const z = y * bits(0x3D59FEF3, 0x11F12B36) + bits(0x3FDBCB7B, 0x1526E50E) * formulaLn(x);
  return z + y * bits(0x3FD34413, 0x509F6000);
}

/** Math.log2 as FreeBSD's log2. */
export function formulaLog2(x: number): number {
  let hx = high(x);
  let lx = low(x);
  let k = 0;
  if (hx < 0x00100000) {
    if (((hx & 0x7fffffff) | lx) === 0) return -Infinity;
    if (hx < 0) return Number.NaN;
    k -= 54;
    x *= TWO54;
    hx = high(x);
    lx = low(x);
  }
  if (hx >= 0x7ff00000) return x + x;
  if (hx === 0x3ff00000 && lx === 0) return 0;
  k += (hx >> 20) - 1023;
  hx &= 0x000fffff;
  const i = (hx + 0x95f64) & 0x100000;
  x = withHigh(x, hx | (i ^ 0x3ff00000));
  k += i >> 20;
  const y = k;
  const f = x - 1.0;
  const hfsq = 0.5 * f * f;
  const s = f / (2.0 + f), z = s * s, w = z * z;
  const r = s * (hfsq + (z * (LG[0] + w * (LG[2] + w * (LG[4] + w * LG[6]))) + w * (LG[1] + w * (LG[3] + w * LG[5]))));
  const hi = withLow(f - hfsq, 0);
  const lo = (f - hi) - hfsq + r;
  const ivln2hi = bits(0x3FF71547, 0x65200000), ivln2lo = bits(0x3DE705FC, 0x2EEFA200);
  const valHi = hi * ivln2hi;
  let valLo = (lo + hi) * ivln2lo + lo * ivln2hi;
  const sum = y + valHi;
  valLo += (y - sum) + valHi;
  return valLo + sum;
}

/** Math.cbrt as FreeBSD's cbrt. */
export function formulaCbrt(x: number): number {
  let hx = high(x) >>> 0;
  const lowWord = low(x);
  const sign = hx & 0x80000000;
  hx = (hx ^ sign) >>> 0;
  if (hx >= 0x7ff00000) return x + x;
  let t: number;
  if (hx < 0x00100000) {
    if ((hx | lowWord) === 0) return x;
    t = withHigh(0, 0x43500000) * x;
    const highWord = high(t) >>> 0;
    t = bits((sign | (Math.floor((highWord & 0x7fffffff) / 3) + 696219795)) >>> 0, 0);
  } else {
    t = bits((sign | (Math.floor(hx / 3) + 715094163)) >>> 0, 0);
  }
  let r = (t * t) * (t / x);
  t = t * ((bits(0x3FFE03E6, 0x0F61E692) + r * (bits(0xBFFE28E0, 0x92F02420) + r * bits(0x3FF9F160, 0x4A49D6C2)))
    + ((r * r) * r) * (bits(0xBFE844CB, 0xBEE751D9) + r * bits(0x3FC2B000, 0xD4E4EDD7)));
  // Round t away from zero to 23 bits: add 2^31 to its low 32 bits and clear its low 30.
  const roundedLow = low(t) + 0x80000000;
  t = bits((high(t) + (roundedLow > 0xffffffff ? 1 : 0)) >>> 0, 0);
  t = withLow(t, (roundedLow >>> 0) & 0xc0000000);
  const s = t * t;
  r = x / s;
  const w = t + t;
  r = (r - t) / (w + r);
  return t + t * r;
}
