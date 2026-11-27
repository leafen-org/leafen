package app

import "math"

// databaseFormulaJSPow is JavaScript's Math.pow (and **) as V8's portable
// code works it out, as Notion's stored values and API read it and as the
// editor does (formulaMath.ts formulaPow): FDLIBM's __ieee754_pow, with
// JavaScript's NaN for ±1 to an infinite power. Go's math.Pow multiplies
// whole powers out and drifts (10^300 is 1.0000000000000006e300 there, 1e300
// in Notion; 10^-5 is 0.000009999999999999999 in Notion). Every product
// added to is rounded on its own (float64()), as FDLIBM's C does, so no
// fused multiply-add changes a bit.
func databaseFormulaJSPow(x, y float64) float64 {
	if math.IsInf(y, 0) && math.Abs(x) == 1 {
		return math.NaN()
	}
	const two53 = 9007199254740992.0
	var (
		huge = 1.0e300
		tiny = 1.0e-300

		bp   = [2]float64{1.0, 1.5}
		dpH  = [2]float64{0.0, math.Float64frombits(0x3FE2B80340000000)}
		dpL  = [2]float64{0.0, math.Float64frombits(0x3E4CFDEB43CFD006)}
		l1   = math.Float64frombits(0x3FE3333333333303)
		l2   = math.Float64frombits(0x3FDB6DB6DB6FABFF)
		l3   = math.Float64frombits(0x3FD55555518F264D)
		l4   = math.Float64frombits(0x3FD17460A91D4101)
		l5   = math.Float64frombits(0x3FCD864A93C9DB65)
		l6   = math.Float64frombits(0x3FCA7E284A454EEF)
		p1   = math.Float64frombits(0x3FC555555555553E)
		p2   = math.Float64frombits(0xBF66C16C16BEBD93)
		p3   = math.Float64frombits(0x3F11566AAF25DE2C)
		p4   = math.Float64frombits(0xBEBBBD41C5D26BF1)
		p5   = math.Float64frombits(0x3E66376972BEA4D0)
		lg2  = math.Float64frombits(0x3FE62E42FEFA39EF)
		lg2H = math.Float64frombits(0x3FE62E4300000000)
		lg2L = math.Float64frombits(0xBE205C610CA86C39)
		ovt  = 8.0085662595372944372e-17
		cp   = math.Float64frombits(0x3FEEC709DC3A03FD)
		cpH  = math.Float64frombits(0x3FEEC709E0000000)
		cpL  = math.Float64frombits(0xBE3E2FE0145B01F5)
		ivH  = math.Float64frombits(0x3FF7154760000000)
		ivL  = math.Float64frombits(0x3E54AE0BF85DDF44)
		iv   = math.Float64frombits(0x3FF71547652B82FE)
	)
	hx, lx := databaseFormulaHigh(x), databaseFormulaLow(x)
	hy, ly := databaseFormulaHigh(y), databaseFormulaLow(y)
	ix, iy := hx&0x7fffffff, hy&0x7fffffff

	// y is 0: x**0 is 1.
	if iy|int32(ly) == 0 {
		return 1
	}
	// NaN in, NaN out.
	if ix > 0x7ff00000 || (ix == 0x7ff00000 && lx != 0) || iy > 0x7ff00000 || (iy == 0x7ff00000 && ly != 0) {
		return x + y
	}
	// Whether y is an odd (1) or even (2) whole number when x < 0.
	yIsInt := 0
	if hx < 0 {
		if iy >= 0x43400000 {
			yIsInt = 2
		} else if iy >= 0x3ff00000 {
			k := (iy >> 20) - 0x3ff
			if k > 20 {
				j := ly >> (52 - uint(k))
				if j<<(52-uint(k)) == ly {
					yIsInt = 2 - int(j&1)
				}
			} else if ly == 0 {
				j := iy >> (20 - uint(k))
				if j<<(20-uint(k)) == iy {
					yIsInt = 2 - int(j&1)
				}
			}
		}
	}
	// y is ±inf, ±1, 2 or 0.5.
	if ly == 0 {
		if iy == 0x7ff00000 {
			switch {
			case (ix-0x3ff00000)|int32(lx) == 0:
				return y - y
			case ix >= 0x3ff00000:
				if hy >= 0 {
					return y
				}
				return 0
			default:
				if hy < 0 {
					return -y
				}
				return 0
			}
		}
		if iy == 0x3ff00000 {
			if hy < 0 {
				return 1 / x
			}
			return x
		}
		if hy == 0x40000000 {
			return x * x
		}
		if hy == 0x3fe00000 && hx >= 0 {
			return math.Sqrt(x)
		}
	}
	ax := math.Abs(x)
	// x is ±0, ±inf or ±1.
	if lx == 0 && (ix == 0x7ff00000 || ix == 0 || ix == 0x3ff00000) {
		z := ax
		if hy < 0 {
			z = 1 / z
		}
		if hx < 0 {
			if (ix-0x3ff00000)|int32(yIsInt) == 0 {
				z = (z - z) / (z - z)
			} else if yIsInt == 1 {
				z = -z
			}
		}
		return z
	}
	n := int32(uint32(hx)>>31) ^ 1
	// A negative x to a fraction is NaN.
	if n|int32(yIsInt) == 0 {
		return (x - x) / (x - x)
	}
	s := 1.0
	if n|int32(yIsInt-1) == 0 {
		s = -1
	}
	var t1, t2 float64
	if iy > 0x41e00000 {
		// |y| is past 2**31.
		if iy > 0x43f00000 {
			if ix <= 0x3fefffff {
				if hy < 0 {
					return huge * huge
				}
				return tiny * tiny
			}
			if ix >= 0x3ff00000 {
				if hy > 0 {
					return huge * huge
				}
				return tiny * tiny
			}
		}
		if ix < 0x3fefffff {
			if hy < 0 {
				return s * huge * huge
			}
			return s * tiny * tiny
		}
		if ix > 0x3ff00000 {
			if hy > 0 {
				return s * huge * huge
			}
			return s * tiny * tiny
		}
		t := ax - 1
		w := float64(t*t) * (0.5 - float64(t*(0.3333333333333333333333-float64(t*0.25))))
		u := float64(ivH * t)
		v := float64(t*ivL) - float64(w*iv)
		t1 = databaseFormulaWithLow(u+v, 0)
		t2 = v - (t1 - u)
	} else {
		n = 0
		if ix < 0x00100000 {
			ax *= two53
			n -= 53
			ix = databaseFormulaHigh(ax)
		}
		n += (ix >> 20) - 0x3ff
		j := ix & 0x000fffff
		ix = j | 0x3ff00000
		k := 0
		switch {
		case j <= 0x3988E:
			k = 0
		case j < 0xBB67A:
			k = 1
		default:
			k = 0
			n++
			ix -= 0x00100000
		}
		ax = databaseFormulaWithHigh(ax, ix)
		u := ax - bp[k]
		v := 1 / (ax + bp[k])
		ss := float64(u * v)
		sH := databaseFormulaWithLow(ss, 0)
		tH := databaseFormulaWithHigh(0, ((ix>>1)|0x20000000)+0x00080000+int32(k<<18))
		tL := ax - (tH - bp[k])
		sL := v * ((u - float64(sH*tH)) - float64(sH*tL))
		s2 := float64(ss * ss)
		r := float64(s2*s2) * (l1 + float64(s2*(l2+float64(s2*(l3+float64(s2*(l4+float64(s2*(l5+float64(s2*l6))))))))))
		r += float64(sL * (sH + ss))
		s2 = float64(sH * sH)
		tH = databaseFormulaWithLow(3.0+s2+r, 0)
		tL = r - ((tH - 3.0) - s2)
		u = float64(sH * tH)
		v = float64(sL*tH) + float64(tL*ss)
		pH := databaseFormulaWithLow(u+v, 0)
		pL := v - (pH - u)
		zH := float64(cpH * pH)
		zL := float64(cpL*pH) + float64(pL*cp) + dpL[k]
		t := float64(n)
		t1 = databaseFormulaWithLow(((zH+zL)+dpH[k])+t, 0)
		t2 = zL - (((t1 - t) - dpH[k]) - zH)
	}
	// (y1+y2)*(t1+t2), y split into its high and low parts.
	y1 := databaseFormulaWithLow(y, 0)
	pL := float64((y-y1)*t1) + float64(y*t2)
	pH := float64(y1 * t1)
	z := pL + pH
	j := databaseFormulaHigh(z)
	i := int32(databaseFormulaLow(z))
	if j >= 0x40900000 {
		if (j-0x40900000)|i != 0 || pL+ovt > z-pH {
			return s * huge * huge
		}
	} else if j&0x7fffffff >= 0x4090cc00 {
		if (j-int32(-0x3f6f3400))|i != 0 || pL <= z-pH {
			return s * tiny * tiny
		}
	}
	// 2**(pH+pL).
	i = j & 0x7fffffff
	k := (i >> 20) - 0x3ff
	n = 0
	if i > 0x3fe00000 {
		n = j + (0x00100000 >> uint(k+1))
		k = ((n & 0x7fffffff) >> 20) - 0x3ff
		t := databaseFormulaWithHigh(0, n&^(0x000fffff>>uint(k)))
		n = ((n & 0x000fffff) | 0x00100000) >> uint(20-k)
		if j < 0 {
			n = -n
		}
		pH -= t
	}
	t := databaseFormulaWithLow(pL+pH, 0)
	u := float64(t * lg2H)
	v := float64((pL-(t-pH))*lg2) + float64(t*lg2L)
	z = u + v
	w := v - (z - u)
	t = float64(z * z)
	t1 = z - float64(t*(p1+float64(t*(p2+float64(t*(p3+float64(t*(p4+float64(t*p5)))))))))
	r := float64(z*t1)/(t1-2) - (w + float64(z*w))
	z = 1 - (r - z)
	j = databaseFormulaHigh(z) + n<<20
	if j>>20 <= 0 {
		z = math.Ldexp(z, int(n))
	} else {
		z = databaseFormulaWithHigh(z, j)
	}
	return s * z
}

func databaseFormulaHigh(x float64) int32 { return int32(math.Float64bits(x) >> 32) }

func databaseFormulaLow(x float64) uint32 { return uint32(math.Float64bits(x)) }

func databaseFormulaWithLow(x float64, low uint32) float64 {
	return math.Float64frombits(math.Float64bits(x)&^0xffffffff | uint64(low))
}

func databaseFormulaWithHigh(x float64, high int32) float64 {
	return math.Float64frombits(uint64(uint32(high))<<32 | uint64(databaseFormulaLow(x)))
}
