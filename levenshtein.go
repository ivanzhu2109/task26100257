package fuzzyjoin

// maxDistance returns the maximum edit distance allowed for a pair of keys
// whose longer side has maxLen code points: floor(0.15 * maxLen), computed
// with exact integer math (3*maxLen/20).
func maxDistance(maxLen int) int {
	return 3 * maxLen / 20
}

// levenshtein returns the exact Levenshtein distance between a and b if it
// is <= cutoff; otherwise it returns a value greater than cutoff.
// Operations: insert, delete, substitute one code point, each costing 1.
func levenshtein(a, b []rune, cutoff int) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	la, lb := len(a), len(b)
	if lb-la > cutoff {
		return cutoff + 1
	}
	if la == 0 {
		return lb
	}
	if cutoff > lb {
		cutoff = lb
	}

	// Banded DP: only cells with |row-col| <= cutoff can stay within cutoff.
	prev := make([]int, la+1)
	curr := make([]int, la+1)
	for j := 0; j <= la && j <= cutoff; j++ {
		prev[j] = j
	}
	for j := la; j > cutoff; j-- {
		prev[j] = cutoff + 1
	}

	for i := 1; i <= lb; i++ {
		lo := i - cutoff
		if lo < 1 {
			lo = 1
		}
		hi := i + cutoff
		if hi > la {
			hi = la
		}
		if lo > hi {
			return cutoff + 1
		}
		if lo > 1 {
			curr[lo-1] = cutoff + 1
		} else {
			curr[0] = i
		}
		rowMin := cutoff + 1
		ca := b[i-1]
		for j := lo; j <= hi; j++ {
			cost := 1
			if a[j-1] == ca {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			v := del
			if ins < v {
				v = ins
			}
			if sub < v {
				v = sub
			}
			if v > cutoff+1 {
				v = cutoff + 1
			}
			curr[j] = v
			if v < rowMin {
				rowMin = v
			}
		}
		if hi < la {
			curr[hi+1] = cutoff + 1
		}
		if rowMin > cutoff {
			return cutoff + 1
		}
		prev, curr = curr, prev
	}
	return prev[la]
}
