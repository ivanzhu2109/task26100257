package fuzzyjoin

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// bruteForce 逐对完整计算编辑距离的标准结果，作为参照。
func bruteForce(left, right []Record) []Pair {
	var out []Pair
	for _, l := range left {
		lr := []rune(l.Key)
		for _, r := range right {
			rr := []rune(r.Key)
			L := len(lr)
			if len(rr) > L {
				L = len(rr)
			}
			d := levFull(lr, rr)
			if 20*d <= 3*L {
				out = append(out, Pair{LeftID: l.ID, RightID: r.ID, Distance: d})
			}
		}
	}
	return out
}

func collect(left, right []Record) []Pair {
	var out []Pair
	Join(left, right, func(p Pair) bool {
		out = append(out, p)
		return true
	})
	return out
}

func sortPairs(ps []Pair) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].LeftID != ps[j].LeftID {
			return ps[i].LeftID < ps[j].LeftID
		}
		if ps[i].RightID != ps[j].RightID {
			return ps[i].RightID < ps[j].RightID
		}
		return ps[i].Distance < ps[j].Distance
	})
}

func checkAgainstBruteForce(t *testing.T, left, right []Record) {
	t.Helper()
	got := collect(left, right)
	want := bruteForce(left, right)
	sortPairs(got)
	sortPairs(want)
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("mismatch\ngot : %v\nwant: %v", got, want)
	}
}

func TestJoinBoundary(t *testing.T) {
	mk := func(prefix string, n int) string {
		s := make([]rune, n)
		for i := range s {
			s[i] = rune('a' + i%26)
		}
		return prefix + string(s[:n-len([]rune(prefix))])
	}
	base := mk("", 20)
	variant3 := []rune(base) // 距离恰好 3：必须匹配（1-3/20 = 0.85）
	variant3[5] = 'X'
	variant3[9] = 'Y'
	variant3[14] = 'Z'
	variant4 := []rune(base) // 距离 4：必须排除
	variant4[1] = 'P'
	variant4[5] = 'X'
	variant4[9] = 'Y'
	variant4[14] = 'Z'
	indel := []rune(base) // 一次插入形成的匹配不得遗漏（21 码点，距离 1）
	indel = append(indel[:7], append([]rune{'Q'}, indel[7:]...)...)

	left := []Record{{ID: "L1", Key: base}}
	right := []Record{
		{ID: "R3", Key: string(variant3)},
		{ID: "R4", Key: string(variant4)},
		{ID: "RI", Key: string(indel)},
	}
	got := collect(left, right)
	seen := map[string]int{}
	for _, p := range got {
		seen[p.RightID] = p.Distance
	}
	if d, ok := seen["R3"]; !ok || d != 3 {
		t.Errorf("distance-3 pair must match with distance 3, got %v", seen)
	}
	if _, ok := seen["R4"]; ok {
		t.Errorf("distance-4 pair must be excluded, got %v", seen)
	}
	if d, ok := seen["RI"]; !ok || d != 1 {
		t.Errorf("indel pair must match with distance 1, got %v", seen)
	}
}

func TestJoinDuplicateKeysAndMultiCandidate(t *testing.T) {
	left := []Record{
		{ID: "L1", Key: "客户名称甲乙丙丁"},
		{ID: "L2", Key: "客户名称甲乙丙丁"}, // 重复键，记录身份须保留
		{ID: "L3", Key: "客户名称甲乙丙戊"},
	}
	right := []Record{
		{ID: "R1", Key: "客户名称甲乙丙丁"},
		{ID: "R2", Key: "客户名称甲乙丙丁"},
		{ID: "R3", Key: "客户名称甲乙丙己"},
	}
	got := collect(left, right)
	// 相同键 2x2 = 4 对距离 0；L3 与 R1/R2 距离 1；L1/L2 与 R3 距离 1；L3 与 R3 距离 1。
	type k struct{ l, r string }
	count := map[k]int{}
	dist := map[k]int{}
	for _, p := range got {
		count[k{p.LeftID, p.RightID}]++
		dist[k{p.LeftID, p.RightID}] = p.Distance
	}
	wantDist := map[k]int{
		{"L1", "R1"}: 0, {"L1", "R2"}: 0, {"L2", "R1"}: 0, {"L2", "R2"}: 0,
		{"L3", "R1"}: 1, {"L3", "R2"}: 1, {"L1", "R3"}: 1, {"L2", "R3"}: 1,
		{"L3", "R3"}: 1,
	}
	if len(got) != len(wantDist) {
		t.Fatalf("got %d pairs, want %d: %v", len(got), len(wantDist), got)
	}
	for key, d := range wantDist {
		if count[key] != 1 || dist[key] != d {
			t.Errorf("pair %v: count=%d dist=%d, want count=1 dist=%d", key, count[key], dist[key], d)
		}
	}
}

func TestJoinUnicodeAndShortKeys(t *testing.T) {
	left := []Record{
		{ID: "L1", Key: "中文客户名册对齐测试"},
		{ID: "L2", Key: "\U0001F600\U0001F601\U0001F602\U0001F603\U0001F604\U0001F605\U0001F606"},
		{ID: "L3", Key: "a"},
		{ID: "L4", Key: "ab"},
	}
	right := []Record{
		{ID: "R1", Key: "中文客户名册对齐测试"},   // 距离 1，L=10，阈值 1
		{ID: "R2", Key: "中文客户名册完全不一致"}, // 距离 4，排除
		{ID: "R3", Key: "\U0001F600\U0001F601\U0001F602\U0001F603\U0001F604\U0001F605\U0001F607"}, // 距离 1，L=7，阈值 1
		{ID: "R4", Key: "\U0001F600\U0001F601\U0001F602X\U0001F604\U0001F605\U0001F606\U0001F608"}, // 距离 2，L=8，排除
		{ID: "R5", Key: "a"},
		{ID: "R6", Key: "b"},
		{ID: "R7", Key: "abc"},
	}
	checkAgainstBruteForce(t, left, right)
}

func TestJoinMatchesBruteForceRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabets := [][]rune{
		[]rune("abcde"),
		[]rune("中文客户名册甲乙丙丁"),
		[]rune("a中\U0001F600b\U0001F961"),
	}
	for trial := 0; trial < 60; trial++ {
		alphabet := alphabets[trial%len(alphabets)]
		mkKey := func() string {
			n := 1 + rng.Intn(24)
			s := make([]rune, n)
			for i := range s {
				s[i] = alphabet[rng.Intn(len(alphabet))]
			}
			return string(s)
		}
		var left, right []Record
		for i := 0; i < 12; i++ {
			left = append(left, Record{ID: fmt.Sprintf("L%d", i), Key: mkKey()})
			right = append(right, Record{ID: fmt.Sprintf("R%d", i), Key: mkKey()})
		}
		// 注入重复键与近邻键
		if trial%3 == 0 && len(left) > 2 {
			left[1].Key = left[0].Key
			right[1].Key = left[0].Key
		}
		if trial%4 == 0 {
			r := []rune(right[0].Key)
			if len(r) > 2 {
				r[1] = 'Ω'
				left[2].Key = string(r)
			}
		}
		checkAgainstBruteForce(t, left, right)
	}
}

func TestJoinEarlyStop(t *testing.T) {
	left := []Record{{ID: "L1", Key: "提前结束测试键"}, {ID: "L2", Key: "提前结束测试键"}}
	right := []Record{{ID: "R1", Key: "提前结束测试键"}, {ID: "R2", Key: "提前结束测试键"}}
	count := 0
	Join(left, right, func(p Pair) bool {
		count++
		return false // 立即结束
	})
	if count != 1 {
		t.Fatalf("yield returned false after first pair, but got %d pairs", count)
	}
}
