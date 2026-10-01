// Package fuzzyjoin 提供双来源模糊联接：按 Levenshtein 相似度找出全部跨来源记录对。
//
// 输入为两个静态来源，每条记录只有来源内唯一标识与匹配键（1 至 64 个
// Unicode 码点，不做任何规范化）。相似度定义为 1 - 编辑距离/较长键码点数，
// 编辑距离按 Levenshtein（插入、删除、替换各代价 1，相邻交换不算一次编辑）。
// Join 固定返回相似度不低于 0.85 的全部跨来源记录对，边界按精确数学值判断。
package fuzzyjoin

// 匹配键长度约束（码点数）。
const (
	minKeyLen = 1
	maxKeyLen = 64
)

// maxDist 返回相似度不低于 0.85 时，较长键为 L 码点所允许的最大编辑距离。
// 1 - d/L >= 17/20 当且仅当 20*d <= 3*L，用整数运算保证边界精确。
func maxDist(L int) int {
	return 3 * L / 20
}

// Record 是一条来源记录：来源内唯一标识 + 匹配键。
type Record struct {
	ID  string
	Key string
}

// Pair 是一对跨来源匹配结果，附带精确编辑距离。
type Pair struct {
	LeftID   string
	RightID  string
	Distance int
}

// Join 流式输出相似度不低于 0.85 的全部跨来源记录对。
//
// 每个符合条件的记录对恰好输出一次；相同键对应多条记录时按记录展开，
// 不做传递合并，也不限制每条记录的匹配数量。结果顺序不限。
// yield 返回 false 时立即停止本次联接，此后不再产生任何计算与输出。
// 输入须满足约束（键长 1 至 64 码点、来源内标识唯一），否则行为未定义。
func Join(left, right []Record, yield func(Pair) bool) {
	if len(left) == 0 || len(right) == 0 {
		return
	}
	lgroups := groupByKey(left)
	// 让右侧成为被索引的一侧；两侧角色对称，距离与长度对换不变。
	rgroups := groupByKey(right)
	idx := buildQgramIndex(rgroups)
	sc := newCandidateScratch(len(rgroups))

	emit := func(lg, rg keyGroup, dist int) bool {
		for _, lid := range lg.ids {
			for _, rid := range rg.ids {
				if !yield(Pair{LeftID: lid, RightID: rid, Distance: dist}) {
					return false
				}
			}
		}
		return true
	}

	// 完全相同键的两侧组先做哈希联接，距离为 0，无需候选过滤。
	rpos := make(map[string]int32, len(rgroups))
	for gi, g := range rgroups {
		rpos[g.key] = int32(gi)
	}
	for _, lg := range lgroups {
		gi, ok := rpos[lg.key]
		if !ok {
			continue
		}
		if !emit(lg, rgroups[gi], 0) {
			return
		}
	}

	// 近似候选：q-gram 必要条件过滤 + 带状 Levenshtein 精确确认。
	for _, lg := range lgroups {
		cont := idx.forEachCandidate(lg, sc, func(gb int32) bool {
			rg := rgroups[gb]
			if rg.key == lg.key {
				return true // 相同键已在精确联接中输出
			}
			L := len(lg.runes)
			if len(rg.runes) > L {
				L = len(rg.runes)
			}
			dist, ok := levWithin(lg.runes, rg.runes, maxDist(L))
			if !ok {
				return true
			}
			return emit(lg, rg, dist)
		})
		if !cont {
			return
		}
	}
}
