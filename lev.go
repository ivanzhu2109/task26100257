package fuzzyjoin

// levWithin 计算 a 与 b 之间的 Levenshtein 距离（插入/删除/替换一个码点代价均为 1）。
// 若真实距离不超过 k，返回 (距离, true)；否则返回 (0, false)。
// 只计算 |i-j| <= k 的带状区域，复杂度 O((2k+1) * len(a))。
func levWithin(a, b []rune, k int) (int, bool) {
	n, m := len(a), len(b)
	if n > m {
		a, b = b, a
		n, m = m, n
	}
	if m-n > k {
		return 0, false
	}
	if n == 0 {
		return m, true
	}

	// prev[j] 表示上一行在列 j 的值，只维护 j 在 [i-k, i+k] 的带。
	// 用完整长度切片避免下标换算，带外填大数（远离溢出）。
	const inf = 1 << 30
	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = j
	}
	for i := 1; i <= n; i++ {
		lo := i - k
		if lo < 1 {
			lo = 1
		}
		hi := i + k
		if hi > m {
			hi = m
		}
		if lo > 1 {
			curr[lo-1] = inf
		}
		if lo == 1 {
			curr[0] = i
		}
		rowMin := inf
		for j := lo; j <= hi; j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
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
			curr[j] = v
			if v < rowMin {
				rowMin = v
			}
		}
		if hi < m {
			curr[hi+1] = inf
		}
		if rowMin > k {
			return 0, false
		}
		prev, curr = curr, prev
	}
	if prev[m] > k {
		return 0, false
	}
	return prev[m], true
}
