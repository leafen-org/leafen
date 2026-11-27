package app

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Formula arithmetic as Notion's formulas work it out, as the editor works
// it out (formulaDecimal.ts): two whole numbers add, subtract and multiply
// as JavaScript's numbers do, and so does a remainder by a whole number;
// with a fraction among them the sum, difference, product or remainder is
// worked out on the numbers' decimal digits, exactly, as big.js does, then
// read back as a number — 0.1 * 3 is 0.3. A quotient is always worked out
// so, to 20 decimal places, halves away from zero (1 / 3e21 is 0). A
// fraction's whole power is its digits' power (1.1 ^ 100 is
// 13780.61233982227), a negative one the quotient of 1 by it; any other
// power is JavaScript's (databaseFormulaJSPow). sum() adds every number so,
// mean() divides that sum, and median() of an even count halves the two
// middle numbers' sum. A number that is not finite has no digits: worked
// out so, it fails the formula ((1e308 * 10) * 1.5, sum(1e308 * 10, 1)),
// where two whole numbers' sum or product may be infinite and a whole
// remainder no number. A quotient or a remainder by 0 is empty, as Notion's
// (seen 2026-11-14: empty(1 / 0) is true, format(1 / 0) "").

// databaseFormulaDecimal is digits × 10^exponent.
type databaseFormulaDecimal struct {
	digits   *big.Int
	exponent int
}

const (
	databaseFormulaQuotientPlaces = 20
	// The whole powers big.js works out, and the digits a power keeps on its
	// way: past them a power's last digits are dropped, which keeps a near-1
	// fraction's millionth power from growing to millions of digits.
	databaseFormulaPowerLimit  = 1_000_000
	databaseFormulaPowerDigits = 1000
)

var errDatabaseFormulaPower = errors.New("a fraction's power must be a whole number up to a million")

// databaseFormulaFinite fails a formula that works out a number that is not
// finite on its digits.
func databaseFormulaFinite(numbers ...float64) error {
	for _, number := range numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return errDatabaseFormulaInvalid
		}
	}
	return nil
}

func databaseFormulaDecimalOf(value float64) databaseFormulaDecimal {
	mantissa, exponentText, _ := strings.Cut(databaseFormulaNumberText(value), "e")
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits, _ := new(big.Int).SetString(whole+fraction, 10)
	exponent, _ := strconv.Atoi(exponentText)
	return databaseFormulaDecimal{digits: digits, exponent: exponent - len(fraction)}
}

// number reads the decimal back as JavaScript's Number() reads its text.
func (value databaseFormulaDecimal) number() float64 {
	number, _ := strconv.ParseFloat(value.digits.String()+"e"+strconv.Itoa(value.exponent), 64)
	return number
}

func databaseFormulaTens(count int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(count)), nil)
}

func databaseFormulaAligned(left, right databaseFormulaDecimal) (*big.Int, *big.Int, int) {
	exponent := min(left.exponent, right.exponent)
	a := new(big.Int).Mul(left.digits, databaseFormulaTens(left.exponent-exponent))
	b := new(big.Int).Mul(right.digits, databaseFormulaTens(right.exponent-exponent))
	return a, b, exponent
}

func (left databaseFormulaDecimal) plus(right databaseFormulaDecimal) databaseFormulaDecimal {
	a, b, exponent := databaseFormulaAligned(left, right)
	return databaseFormulaDecimal{digits: a.Add(a, b), exponent: exponent}
}

func (left databaseFormulaDecimal) times(right databaseFormulaDecimal) databaseFormulaDecimal {
	return databaseFormulaDecimal{digits: new(big.Int).Mul(left.digits, right.digits), exponent: left.exponent + right.exponent}
}

func (value databaseFormulaDecimal) digitCount() int {
	return len(new(big.Int).Abs(value.digits).String())
}

// quotient is left / right to places decimal places, halves away from
// zero, or toward zero when toZero.
func (left databaseFormulaDecimal) quotient(right databaseFormulaDecimal, places int, toZero bool) databaseFormulaDecimal {
	shift := left.exponent - right.exponent + places
	top, bottom := new(big.Int).Abs(left.digits), new(big.Int).Abs(right.digits)
	if shift >= 0 {
		top.Mul(top, databaseFormulaTens(shift))
	} else {
		bottom.Mul(bottom, databaseFormulaTens(-shift))
	}
	digits, rest := new(big.Int).QuoRem(top, bottom, new(big.Int))
	if !toZero && rest.Lsh(rest, 1).Cmp(bottom) >= 0 {
		digits.Add(digits, big.NewInt(1))
	}
	if left.digits.Sign()*right.digits.Sign() < 0 {
		digits.Neg(digits)
	}
	return databaseFormulaDecimal{digits: digits, exponent: -places}
}

// remainder is the remainder of left / right with left's sign, as big.js's mod().
func (left databaseFormulaDecimal) remainder(right databaseFormulaDecimal) databaseFormulaDecimal {
	a, b, _ := databaseFormulaAligned(left, right)
	if new(big.Int).Abs(b).Cmp(new(big.Int).Abs(a)) > 0 {
		return left
	}
	whole := left.quotient(right, 0, true)
	return left.plus(databaseFormulaDecimal{digits: new(big.Int).Neg(new(big.Int).Mul(whole.digits, right.digits)), exponent: right.exponent})
}

func (base databaseFormulaDecimal) power(exponent float64) (databaseFormulaDecimal, error) {
	if exponent != math.Trunc(exponent) || math.Abs(exponent) > databaseFormulaPowerLimit {
		return databaseFormulaDecimal{}, errDatabaseFormulaPower
	}
	kept := func(value databaseFormulaDecimal) databaseFormulaDecimal {
		if drop := value.digitCount() - databaseFormulaPowerDigits; drop > 0 {
			return databaseFormulaDecimal{digits: new(big.Int).Quo(value.digits, databaseFormulaTens(drop)), exponent: value.exponent + drop}
		}
		return value
	}
	result, factor := databaseFormulaDecimal{digits: big.NewInt(1)}, base
	for count := int(math.Abs(exponent)); ; {
		if count%2 == 1 {
			result = kept(result.times(factor))
		}
		count /= 2
		if count == 0 {
			break
		}
		factor = kept(factor.times(factor))
	}
	if exponent >= 0 {
		return result, nil
	}
	// 1 over a number of 10^22 or more is 0 to 20 places; over one below
	// 10^-400 it is past any number.
	switch size := result.digitCount() + result.exponent; {
	case size > 22:
		return databaseFormulaDecimal{digits: big.NewInt(0)}, nil
	case size < -400:
		return databaseFormulaDecimal{digits: big.NewInt(int64(result.digits.Sign())), exponent: 400}, nil
	}
	return databaseFormulaDecimal{digits: big.NewInt(1)}.quotient(result, databaseFormulaQuotientPlaces, false), nil
}

func databaseFormulaWhole(value float64) bool {
	return value == math.Trunc(value) && !math.IsInf(value, 0)
}

func databaseFormulaAdd(left, right float64) (float64, error) {
	if databaseFormulaWhole(left) && databaseFormulaWhole(right) {
		return left + right, nil
	}
	if err := databaseFormulaFinite(left, right); err != nil {
		return 0, err
	}
	return databaseFormulaDecimalOf(left).plus(databaseFormulaDecimalOf(right)).number(), nil
}

func databaseFormulaSubtract(left, right float64) (float64, error) {
	if databaseFormulaWhole(left) && databaseFormulaWhole(right) {
		return left - right, nil
	}
	if err := databaseFormulaFinite(left, right); err != nil {
		return 0, err
	}
	return databaseFormulaDecimalOf(left).plus(databaseFormulaDecimalOf(-right)).number(), nil
}

func databaseFormulaMultiply(left, right float64) (float64, error) {
	if databaseFormulaWhole(left) && databaseFormulaWhole(right) {
		return left * right, nil
	}
	if err := databaseFormulaFinite(left, right); err != nil {
		return 0, err
	}
	return databaseFormulaDecimalOf(left).times(databaseFormulaDecimalOf(right)).number(), nil
}

// databaseFormulaDivide is left / right; by 0 empty.
func databaseFormulaDivide(left, right float64) (any, error) {
	if right == 0 {
		return nil, nil
	}
	if err := databaseFormulaFinite(left, right); err != nil {
		return nil, err
	}
	return databaseFormulaDecimalOf(left).quotient(databaseFormulaDecimalOf(right), databaseFormulaQuotientPlaces, false).number(), nil
}

// databaseFormulaRemainder is the remainder of left / right; by 0 empty.
func databaseFormulaRemainder(left, right float64) (any, error) {
	if right == 0 {
		return nil, nil
	}
	if databaseFormulaWhole(right) {
		return math.Mod(left, right), nil
	}
	if err := databaseFormulaFinite(left, right); err != nil {
		return nil, err
	}
	return databaseFormulaDecimalOf(left).remainder(databaseFormulaDecimalOf(right)).number(), nil
}

// databaseFormulaPower is a power as Notion's formulas work it out: a
// fraction's whole power on its digits, any other as JavaScript's.
func databaseFormulaPower(base, exponent float64) (float64, error) {
	if !databaseFormulaWhole(base) && databaseFormulaWhole(exponent) {
		if err := databaseFormulaFinite(base); err != nil {
			return 0, err
		}
		result, err := databaseFormulaDecimalOf(base).power(exponent)
		if err != nil {
			return 0, err
		}
		return result.number(), nil
	}
	return databaseFormulaJSPow(base, exponent), nil
}

func databaseFormulaSum(numbers []float64) (databaseFormulaDecimal, error) {
	total := databaseFormulaDecimal{digits: big.NewInt(0)}
	if err := databaseFormulaFinite(numbers...); err != nil {
		return total, err
	}
	for _, number := range numbers {
		total = total.plus(databaseFormulaDecimalOf(number))
	}
	return total, nil
}

func databaseFormulaMean(numbers []float64) (float64, error) {
	total, err := databaseFormulaSum(numbers)
	if err != nil {
		return 0, err
	}
	return total.quotient(databaseFormulaDecimalOf(float64(len(numbers))), databaseFormulaQuotientPlaces, false).number(), nil
}

func databaseFormulaMiddle(left, right float64) (float64, error) {
	total, err := databaseFormulaSum([]float64{left, right})
	if err != nil {
		return 0, err
	}
	return total.quotient(databaseFormulaDecimalOf(2), databaseFormulaQuotientPlaces, false).number(), nil
}
