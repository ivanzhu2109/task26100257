package fuzzyjoin

import (
	"math/rand"
	"testing"
)

// levFull 是不带截断的朴素全矩阵 Levenshtein，作为参照实现。
func levFull(a, b []rune) int {
	n, m := len(a), len(b)
	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = j
	}
	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			v := prev[j] + 1
			if curr[j-1]+1 < v {
				v = curr[j-1] + 1
			}
			if prev[j-1]+cost < v {
				v = prev[j-1] + cost
			}
			curr[j] = v
		}
		prev, curr = curr, prev
	}
	return prev[m]
}

func TestLevWithinBasic(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "ab", 1},
		{"ab", "abc", 1},
		{"kitten", "sitting", 3},
		{"中文匹配", "中文匹备", 1},
		{"\U0001F600abc", "\U0001F601abc", 1}, // 补充平面字符按单个码点计
		{"ab", "ba", 2},                       // 交换相邻码点算两次编辑
	}
	for _, c := range cases {
		got, ok := levWithin([]rune(c.a), []rune(c.b), 64)
		if !ok || got != c.want {
			t.Errorf("levWithin(%q,%q) = %d,%v; want %d", c.a, c.b, got, ok, c.want)
		}
	}
}

func TestLevWithinCutoff(t *testing.T) {
	if _, ok := levWithin([]rune("abcd"), []rune("wxyz"), 3); ok {
		t.Error("distance 4 should exceed k=3")
	}
	if d, ok := levWithin([]rune("abcd"), []rune("wxyz"), 4); !ok || d != 4 {
		t.Errorf("distance 4 within k=4, got %d,%v", d, ok)
	}
	if _, ok := levWithin([]rune("abcdef"), []rune("ab"), 3); ok {
		t.Error("length diff 4 should exceed k=3")
	}
}

func TestLevWithinMatchesFull(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	alphabet := []rune("abc中\U0001F600")
	for iter := 0; iter < 20000; iter++ {
		la := rng.Intn(12)
		lb := rng.Intn(12)
		a := make([]rune, la)
		b := make([]rune, lb)
		for i := range a {
			a[i] = alphabet[rng.Intn(len(alphabet))]
		}
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		full := levFull(a, b)
		for k := 0; k <= 12; k++ {
			got, ok := levWithin(a, b, k)
			if full <= k {
				if !ok || got != full {
					t.Fatalf("a=%q b=%q k=%d: got %d,%v want %d,true", string(a), string(b), k, got, ok, full)
				}
			} else if ok {
				t.Fatalf("a=%q b=%q k=%d: got ok but full=%d", string(a), string(b), k, full)
			}
		}
	}
}
