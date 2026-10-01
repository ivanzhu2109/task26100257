package fuzzyjoin

// keyGroup 是相同匹配键的聚合：一个键对应来源内多条记录。
type keyGroup struct {
	key   string
	runes []rune
	ids   []string
}

// groupByKey 按匹配键聚合记录，保持记录身份与首次出现顺序。
func groupByKey(recs []Record) []keyGroup {
	pos := make(map[string]int, len(recs))
	groups := make([]keyGroup, 0, len(recs))
	for _, r := range recs {
		if i, ok := pos[r.Key]; ok {
			groups[i].ids = append(groups[i].ids, r.ID)
			continue
		}
		pos[r.Key] = len(groups)
		groups = append(groups, keyGroup{key: r.Key, runes: []rune(r.Key), ids: []string{r.ID}})
	}
	return groups
}

// q-gram 过滤参数：q=3；posting list 超过 stopCap 的 gram 视为停用 gram 不计入。
const (
	qgramQ  = 3
	stopCap = 512
)

// qgramIndex 是右侧不同键上的 q-gram 倒排索引。
type qgramIndex struct {
	groups []keyGroup
	posts  map[[qgramQ]rune][]int32 // 非停用 gram -> 组下标（按出现次数重复）
	stop   map[[qgramQ]rune]struct{}
	byLen  map[int][]int32 // 码点长度 -> 组下标
}

func buildQgramIndex(groups []keyGroup) *qgramIndex {
	idx := &qgramIndex{
		groups: groups,
		posts:  make(map[[qgramQ]rune][]int32),
		stop:   make(map[[qgramQ]rune]struct{}),
		byLen:  make(map[int][]int32),
	}
	for gi, g := range groups {
		idx.byLen[len(g.runes)] = append(idx.byLen[len(g.runes)], int32(gi))
		for i := 0; i+qgramQ <= len(g.runes); i++ {
			var gram [qgramQ]rune
			copy(gram[:], g.runes[i:i+qgramQ])
			if _, isStop := idx.stop[gram]; isStop {
				continue
			}
			lst := append(idx.posts[gram], int32(gi))
			if len(lst) > stopCap {
				delete(idx.posts, gram)
				idx.stop[gram] = struct{}{}
			} else {
				idx.posts[gram] = lst
			}
		}
	}
	return idx
}

// candidateScratch 是候选计数的可复用暂存，避免每次分配。
type candidateScratch struct {
	counts  []int32
	touched []int32
}

func newCandidateScratch(numGroups int) *candidateScratch {
	return &candidateScratch{counts: make([]int32, numGroups)}
}

// forEachCandidate 对左键 a 枚举所有通过 q-gram 计数过滤的右侧组下标。
// 过滤条件是编辑距离不超过 maxDist(L) 的必要条件，故不漏真候选；
// 调用方须再用 levWithin 精确确认。fn 返回 false 时立即中止并返回 false。
func (idx *qgramIndex) forEachCandidate(a keyGroup, sc *candidateScratch, fn func(gb int32) bool) bool {
	runes := a.runes
	n := len(runes)
	sc.touched = sc.touched[:0]
	stopCount := 0
	for i := 0; i+qgramQ <= n; i++ {
		var gram [qgramQ]rune
		copy(gram[:], runes[i:i+qgramQ])
		if _, isStop := idx.stop[gram]; isStop {
			stopCount++
			continue
		}
		for _, gi := range idx.posts[gram] {
			if sc.counts[gi] == 0 {
				sc.touched = append(sc.touched, gi)
			}
			sc.counts[gi]++
		}
	}
	// 必要条件 20*|n-m| <= 3*max(n,m) 限定可能匹配的长度范围。
	loM := (17*n + 19) / 20
	hiM := 20 * n / 17
	if hiM > maxKeyLen {
		hiM = maxKeyLen
	}
	ok := true
	for m := loM; m <= hiM && ok; m++ {
		bucket := idx.byLen[m]
		if len(bucket) == 0 {
			continue
		}
		L := n
		if m > L {
			L = m
		}
		// 距离 <= d 的两串至少共享 L-q+1-q*d 个 q-gram（多重集意义）；
		// 再减去 a 中被跳过的停用 gram 数，得到稀有 gram 计数阈值。
		t := L - qgramQ + 1 - qgramQ*maxDist(L) - stopCount
		if t <= 0 {
			for _, gi := range bucket {
				if !fn(gi) {
					ok = false
					break
				}
			}
		} else {
			for _, gi := range bucket {
			if sc.counts[gi] >= int32(t) && !fn(gi) {
					ok = false
					break
				}
			}
		}
	}
	for _, gi := range sc.touched {
		sc.counts[gi] = 0
	}
	return ok
}
