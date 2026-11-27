// Formula arithmetic as Notion's formulas work it out (read in Notion's
// formula functions and seen 2026-11-14 under 한지테스트페이지): two whole
// numbers add, subtract and multiply as JavaScript's numbers do, and so does
// a remainder by a whole number; with a fraction among them the sum,
// difference, product or remainder is worked out on the numbers' decimal
// digits, exactly, as big.js does, then read back as a number — 0.1 * 3 is
// 0.3, not 0.30000000000000004. A quotient is always worked out so, to 20
// decimal places, halves away from zero (big.js's own settings): 1 / 3e21 is
// 0. A fraction's whole power is its digits' power (1.1 ^ 100 is
// 13780.61233982227, not 13780.61233982238), a negative one the quotient of 1
// by it; any other power is JavaScript's (formulaMath.ts formulaPow).
// sum() adds every number so, mean() divides that sum, and median() of an
// even count halves the two middle numbers' sum. A number that is not finite
// has no digits: worked out so, it fails the formula ((1e308 * 10) * 1.5,
// sqrt(-1) + 0.5, sum(1e308 * 10, 1)), where two whole numbers' sum or
// product may be infinite (1e308 + 1e308) and a whole remainder no number
// ((1e308 * 10) % 2). A quotient or a remainder by 0 is empty (1 / 0, 0 / 0,
// 1 % 0), as Notion's (seen 2026-11-14 under 한지테스트페이지: empty(1 / 0)
// is true, format(1 / 0) ""). The server works them out the same way
// (database_formula_decimal.go).

import { formulaPow } from "./formulaMath";
import { FormulaWorkError } from "./formulaRunError";

/** A decimal number: digits × 10^exponent. */
type Decimal = { digits: bigint; exponent: number };

const ONE: Decimal = { digits: 1n, exponent: 0 };
const ZERO: Decimal = { digits: 0n, exponent: 0 };
const QUOTIENT_PLACES = 20;
// The whole powers big.js works out, and the digits a power keeps on its way:
// past them a power's last digits are dropped, which keeps a near-1 fraction's
// millionth power from growing to millions of digits; the number it is read
// back as is the same.
const POWER_LIMIT = 1e6;
const POWER_DIGITS = 1000;

function decimalOf(value: number): Decimal {
  if (!Number.isFinite(value)) throw new FormulaWorkError("A number that is not finite has no digits");
  const [mantissa, exponent = "0"] = String(value).split("e");
  const [whole, fraction = ""] = mantissa.split(".");
  return { digits: BigInt(whole + fraction), exponent: Number(exponent) - fraction.length };
}

function numberOf(value: Decimal): number {
  return Number(`${value.digits}e${value.exponent}`);
}

const tens = (count: number) => 10n ** BigInt(count);

function aligned(left: Decimal, right: Decimal): [bigint, bigint, number] {
  const exponent = Math.min(left.exponent, right.exponent);
  return [left.digits * tens(left.exponent - exponent), right.digits * tens(right.exponent - exponent), exponent];
}

function plus(left: Decimal, right: Decimal): Decimal {
  const [a, b, exponent] = aligned(left, right);
  return { digits: a + b, exponent };
}

function times(left: Decimal, right: Decimal): Decimal {
  return { digits: left.digits * right.digits, exponent: left.exponent + right.exponent };
}

const magnitude = (digits: bigint) => (digits < 0n ? -digits : digits);
const digitCount = (value: Decimal) => magnitude(value.digits).toString().length;

/** left / right to `places` decimal places, halves away from zero, or toward zero when `toZero`. */
function quotient(left: Decimal, right: Decimal, places: number, toZero = false): Decimal {
  const shift = left.exponent - right.exponent + places;
  let top = magnitude(left.digits), bottom = magnitude(right.digits);
  if (shift >= 0) top *= tens(shift);
  else bottom *= tens(-shift);
  let digits = top / bottom;
  if (!toZero && 2n * (top % bottom) >= bottom) digits += 1n;
  return { digits: (left.digits < 0n) !== (right.digits < 0n) ? -digits : digits, exponent: -places };
}

/** The remainder of left / right with left's sign, as big.js's mod(). */
function remainder(left: Decimal, right: Decimal): Decimal {
  const [a, b] = aligned(left, right);
  if (magnitude(b) > magnitude(a)) return left;
  const whole = quotient(left, right, 0, true);
  return plus(left, { digits: -whole.digits * right.digits, exponent: right.exponent });
}

function power(base: Decimal, exponent: number): Decimal {
  if (!Number.isInteger(exponent) || Math.abs(exponent) > POWER_LIMIT) throw new FormulaWorkError("A fraction's power must be a whole number up to a million");
  let result = ONE, factor = base;
  const kept = (value: Decimal) => {
    const drop = digitCount(value) - POWER_DIGITS;
    return drop > 0 ? { digits: value.digits / tens(drop), exponent: value.exponent + drop } : value;
  };
  for (let count = Math.abs(exponent); ; ) {
    if (count % 2 === 1) result = kept(times(result, factor));
    count = Math.floor(count / 2);
    if (!count) break;
    factor = kept(times(factor, factor));
  }
  if (exponent >= 0) return result;
  // 1 over a number of 10^22 or more is 0 to 20 places; over one below
  // 10^-400 it is past any number.
  const size = digitCount(result) + result.exponent;
  if (size > 22) return ZERO;
  if (size < -400) return { digits: result.digits < 0n ? -1n : 1n, exponent: 400 };
  return quotient(ONE, result, QUOTIENT_PLACES);
}

const whole = Number.isInteger;

export function formulaAdd(left: number, right: number) {
  return whole(left) && whole(right) ? left + right : numberOf(plus(decimalOf(left), decimalOf(right)));
}

export function formulaSubtract(left: number, right: number) {
  return whole(left) && whole(right) ? left - right : numberOf(plus(decimalOf(left), decimalOf(-right)));
}

export function formulaMultiply(left: number, right: number) {
  return whole(left) && whole(right) ? left * right : numberOf(times(decimalOf(left), decimalOf(right)));
}

/** left / right; by 0 empty. */
export function formulaDivide(left: number, right: number): number | null {
  return right === 0 ? null : numberOf(quotient(decimalOf(left), decimalOf(right), QUOTIENT_PLACES));
}

/** The remainder of left / right; by 0 empty. */
export function formulaRemainder(left: number, right: number): number | null {
  if (right === 0) return null;
  return whole(right) ? left % right : numberOf(remainder(decimalOf(left), decimalOf(right)));
}

export function formulaPower(base: number, exponent: number) {
  return !whole(base) && whole(exponent) ? numberOf(power(decimalOf(base), exponent)) : formulaPow(base, exponent);
}

/** The sum of numbers, added on their digits. */
export function formulaSum(numbers: readonly number[]) {
  return numberOf(numbers.reduce((total, value) => plus(total, decimalOf(value)), ZERO));
}

/** The mean of numbers: their sum on their digits over their count. */
export function formulaMean(numbers: readonly number[]) {
  const total = numbers.reduce((sum, value) => plus(sum, decimalOf(value)), ZERO);
  return numberOf(quotient(total, decimalOf(numbers.length), QUOTIENT_PLACES));
}

/** Half the sum of two numbers, on their digits. */
export function formulaMiddle(left: number, right: number) {
  return numberOf(quotient(plus(decimalOf(left), decimalOf(right)), decimalOf(2), QUOTIENT_PLACES));
}
