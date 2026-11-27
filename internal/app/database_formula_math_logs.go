package app

import "math"

// exp(), ln(), log10(), log2() and cbrt() of a formula as V8's portable code
// works them out (ieee754.cc: FDLIBM's exp, log and log10, FreeBSD's log2
// and cbrt), as the editor does (formulaMath.ts), where
// Go's math package takes other routes or fuses multiply-adds on arm64 and
// lands a bit away (Notion: log2(3) is 1.584962500721156 and cbrt(2.5)
// 1.3572088082974532, Go 1.5849625007211563 and 1.3572088082974534). Every
// product added to is rounded on its own (float64()), as the C does.

var (
	databaseFormulaTwo54   = math.Float64frombits(0x4350000000000000)
	databaseFormulaLn2Hi   = math.Float64frombits(0x3FE62E42FEE00000)
	databaseFormulaLn2Lo   = math.Float64frombits(0x3DEA39EF35793C76)
	databaseFormulaLogTerm = [7]float64{
		math.Float64frombits(0x3FE5555555555593), math.Float64frombits(0x3FD999999997FA04),
		math.Float64frombits(0x3FD2492494229359), math.Float64frombits(0x3FCC71C51D8E78AF),
		math.Float64frombits(0x3FC7466496CB03DE), math.Float64frombits(0x3FC39A09D078C69F),
		math.Float64frombits(0x3FC2F112DF3E5244),
	}
)

// databaseFormulaJSExp is FDLIBM's __ieee754_exp.
func databaseFormulaJSExp(x float64) float64 {
	var (
		half      = [2]float64{0.5, -0.5}
		huge      = 1.0e300
		twom1000  = math.Float64frombits(0x0170000000000000)
		overflow  = math.Float64frombits(0x40862E42FEFA39EF)
		underflow = math.Float64frombits(0xC0874910D52D3051)
		ln2HI     = [2]float64{databaseFormulaLn2Hi, -databaseFormulaLn2Hi}
		ln2LO     = [2]float64{databaseFormulaLn2Lo, -databaseFormulaLn2Lo}
		invln2    = math.Float64frombits(0x3FF71547652B82FE)
		p1        = math.Float64frombits(0x3FC555555555553E)
		p2        = math.Float64frombits(0xBF66C16C16BEBD93)
		p3        = math.Float64frombits(0x3F11566AAF25DE2C)
		p4        = math.Float64frombits(0xBEBBBD41C5D26BF1)
		p5        = math.Float64frombits(0x3E66376972BEA4D0)
	)
	hx := uint32(databaseFormulaHigh(x))
	sign := int(hx >> 31)
	hx &= 0x7fffffff
	if hx >= 0x40862E42 {
		if hx >= 0x7ff00000 {
			if (hx&0xfffff)|databaseFormulaLow(x) != 0 {
				return x + x
			}
			if sign == 0 {
				return x
			}
			return 0
		}
		if x > overflow {
			return huge * huge
		}
		if x < underflow {
			return twom1000 * twom1000
		}
	}
	var hi, lo float64
	k := 0
	switch {
	case hx > 0x3fd62e42:
		if hx < 0x3FF0A2B2 {
			// V8 gives exp(1) as E, which this route misses by a bit.
			if x == 1 {
				return math.E
			}
			hi, lo, k = x-ln2HI[sign], ln2LO[sign], 1-sign-sign
		} else {
			k = int(float64(invln2*x) + half[sign])
			t := float64(k)
			hi = x - float64(t*ln2HI[0])
			lo = float64(t * ln2LO[0])
		}
		x = hi - lo
	case hx < 0x3e300000:
		if huge+x > 1 {
			return 1 + x
		}
	}
	t := float64(x * x)
	c := x - float64(t*(p1+float64(t*(p2+float64(t*(p3+float64(t*(p4+float64(t*p5)))))))))
	if k == 0 {
		return 1 - (float64(x*c)/(c-2.0) - x)
	}
	y := 1 - ((lo - float64(x*c)/(2.0-c)) - hi)
	if k >= -1021 {
		return databaseFormulaWithHigh(y, databaseFormulaHigh(y)+int32(k<<20))
	}
	return databaseFormulaWithHigh(y, databaseFormulaHigh(y)+int32((k+1000)<<20)) * twom1000
}

// databaseFormulaJSLog is FDLIBM's __ieee754_log.
func databaseFormulaJSLog(x float64) float64 {
	lg := databaseFormulaLogTerm
	hx, lx := databaseFormulaHigh(x), databaseFormulaLow(x)
	k := int32(0)
	if hx < 0x00100000 {
		if (hx&0x7fffffff)|int32(lx) == 0 {
			return math.Inf(-1)
		}
		if hx < 0 {
			return math.NaN()
		}
		k -= 54
		x *= databaseFormulaTwo54
		hx = databaseFormulaHigh(x)
	}
	if hx >= 0x7ff00000 {
		return x + x
	}
	k += (hx >> 20) - 1023
	hx &= 0x000fffff
	i := (hx + 0x95f64) & 0x100000
	x = databaseFormulaWithHigh(x, hx|(i^0x3ff00000))
	k += i >> 20
	f := x - 1.0
	dk := float64(k)
	if (0x000fffff & (2 + hx)) < 3 {
		if f == 0 {
			if k == 0 {
				return 0
			}
			return float64(dk*databaseFormulaLn2Hi) + float64(dk*databaseFormulaLn2Lo)
		}
		r := float64(f*f) * (0.5 - float64(0.33333333333333333*f))
		if k == 0 {
			return f - r
		}
		return float64(dk*databaseFormulaLn2Hi) - ((r - float64(dk*databaseFormulaLn2Lo)) - f)
	}
	s := f / (2.0 + f)
	z := float64(s * s)
	i = hx - 0x6147a
	w := float64(z * z)
	j := 0x6b851 - hx
	t1 := w * (lg[1] + float64(w*(lg[3]+float64(w*lg[5]))))
	t2 := z * (lg[0] + float64(w*(lg[2]+float64(w*(lg[4]+float64(w*lg[6]))))))
	i |= j
	r := float64(t2) + float64(t1)
	if i > 0 {
		hfsq := float64(float64(0.5*f) * f)
		if k == 0 {
			return f - (hfsq - float64(s*(hfsq+r)))
		}
		return float64(dk*databaseFormulaLn2Hi) - ((hfsq - (float64(s*(hfsq+r)) + float64(dk*databaseFormulaLn2Lo))) - f)
	}
	if k == 0 {
		return f - float64(s*(f-r))
	}
	return float64(dk*databaseFormulaLn2Hi) - ((float64(s*(f-r)) - float64(dk*databaseFormulaLn2Lo)) - f)
}

// databaseFormulaLog1pTerm is FreeBSD's k_log1p: log(1+f) - f + f*f/2 for
// 1+f in [sqrt(2)/2, sqrt(2)].
func databaseFormulaLog1pTerm(f float64) float64 {
	lg := databaseFormulaLogTerm
	s := f / (2.0 + f)
	z := float64(s * s)
	w := float64(z * z)
	t1 := float64(w * (lg[1] + float64(w*(lg[3]+float64(w*lg[5])))))
	t2 := float64(z * (lg[0] + float64(w*(lg[2]+float64(w*(lg[4]+float64(w*lg[6])))))))
	r := t2 + t1
	hfsq := float64(float64(0.5*f) * f)
	return float64(s * (hfsq + r))
}

// databaseFormulaJSLog2 is FreeBSD's log2.
func databaseFormulaJSLog2(x float64) float64 {
	hx, lx := databaseFormulaHigh(x), databaseFormulaLow(x)
	k := int32(0)
	if hx < 0x00100000 {
		if (hx&0x7fffffff)|int32(lx) == 0 {
			return math.Inf(-1)
		}
		if hx < 0 {
			return math.NaN()
		}
		k -= 54
		x *= databaseFormulaTwo54
		hx = databaseFormulaHigh(x)
		lx = databaseFormulaLow(x)
	}
	if hx >= 0x7ff00000 {
		return x + x
	}
	if hx == 0x3ff00000 && lx == 0 {
		return 0
	}
	k += (hx >> 20) - 1023
	hx &= 0x000fffff
	i := (hx + 0x95f64) & 0x100000
	x = databaseFormulaWithHigh(x, hx|(i^0x3ff00000))
	k += i >> 20
	y := float64(k)
	f := x - 1.0
	hfsq := float64(float64(0.5*f) * f)
	r := databaseFormulaLog1pTerm(f)
	hi := databaseFormulaWithLow(f-hfsq, 0)
	lo := ((f - hi) - hfsq) + r
	ivln2hi := math.Float64frombits(0x3FF7154765200000)
	ivln2lo := math.Float64frombits(0x3DE705FC2EEFA200)
	valHi := float64(hi * ivln2hi)
	valLo := float64((lo+hi)*ivln2lo) + float64(lo*ivln2hi)
	w := y + valHi
	valLo += (y - w) + valHi
	return valLo + w
}

// databaseFormulaJSLog10 is FDLIBM's __ieee754_log10.
func databaseFormulaJSLog10(x float64) float64 {
	hx, lx := databaseFormulaHigh(x), databaseFormulaLow(x)
	k := int32(0)
	if hx < 0x00100000 {
		if (hx&0x7fffffff)|int32(lx) == 0 {
			return math.Inf(-1)
		}
		if hx < 0 {
			return math.NaN()
		}
		k -= 54
		x *= databaseFormulaTwo54
		hx = databaseFormulaHigh(x)
	}
	if hx >= 0x7ff00000 {
		return x + x
	}
	k += (hx >> 20) - 1023
	i := int32(uint32(k) >> 31)
	hx = (hx & 0x000fffff) | ((0x3ff - i) << 20)
	y := float64(k + i)
	x = databaseFormulaWithHigh(x, hx)
	z := float64(y*math.Float64frombits(0x3D59FEF311F12B36)) + float64(math.Float64frombits(0x3FDBCB7B1526E50E)*databaseFormulaJSLog(x))
	return z + float64(y*math.Float64frombits(0x3FD34413509F6000))
}

// databaseFormulaJSCbrt is FreeBSD's cbrt.
func databaseFormulaJSCbrt(x float64) float64 {
	const (
		b1 = 715094163
		b2 = 696219795
	)
	var (
		c0 = math.Float64frombits(0x3FFE03E60F61E692)
		c1 = math.Float64frombits(0xBFFE28E092F02420)
		c2 = math.Float64frombits(0x3FF9F1604A49D6C2)
		c3 = math.Float64frombits(0xBFE844CBBEE751D9)
		c4 = math.Float64frombits(0x3FC2B000D4E4EDD7)
	)
	hx := uint32(databaseFormulaHigh(x))
	low := databaseFormulaLow(x)
	sign := hx & 0x80000000
	hx ^= sign
	if hx >= 0x7ff00000 {
		return x + x
	}
	var t float64
	if hx < 0x00100000 {
		if hx|low == 0 {
			return x
		}
		t = databaseFormulaWithHigh(0, 0x43500000) * x
		high := uint32(databaseFormulaHigh(t))
		t = math.Float64frombits(uint64(sign|((high&0x7fffffff)/3+b2)) << 32)
	} else {
		t = math.Float64frombits(uint64(sign|(hx/3+b1)) << 32)
	}
	r := float64(t*t) * (t / x)
	t = t * ((c0 + float64(r*(c1+float64(r*c2)))) + float64(float64(float64(r*r)*r)*(c3+float64(r*c4))))
	t = math.Float64frombits((math.Float64bits(t) + 0x80000000) & 0xffffffffc0000000)
	s := float64(t * t)
	r = x / s
	w := t + t
	r = (r - t) / (w + r)
	return t + float64(t*r)
}
