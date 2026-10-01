package fuzzyjoin

import (
	"fmt"
	"math/rand"
	"testing"
)

// fullLevenshtein is an independent, unbanded reference implementation.
func fullLevenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
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
	return prev[len(b)]
}

// bruteForce computes the standard result by exhaustive pairwise comparison.
func bruteForce(left, right []Record) []Pair {
	var out []Pair
	for _, l := range left {
		for _, r := range right {
			lr, rr := []rune(l.Key), []rune(r.Key)
			m := len(lr)
			if len(rr) > m {
				m = len(rr)
			}
			d := fullLevenshtein(lr, rr)
			if d <= maxDistance(m) {
				out = append(out, Pair{LeftID: l.ID, RightID: r.ID, Distance: d})
			}
		}
	}
	return out
}

func pairSet(pairs []Pair) map[Pair]int {
	m := make(map[Pair]int, len(pairs))
	for _, p := range pairs {
		m[p]++
	}
	return m
}

func checkAgainstBruteForce(t *testing.T, left, right []Record) {
	t.Helper()
	got, err := JoinAll(left, right)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	want := bruteForce(left, right)
	gs, ws := pairSet(got), pairSet(want)
	if len(gs) != len(ws) {
		t.Fatalf("pair count mismatch: got %d distinct, want %d\ngot=%v\nwant=%v", len(gs), len(ws), got, want)
	}
	for p, n := range ws {
		if gs[p] != n {
			t.Errorf("pair %+v: got %d occurrences, want %d", p, gs[p], n)
		}
	}
}

func TestLevenshteinBasic(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "abcd", 1},
		{"abcd", "abc", 1},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"ab", "ba", 2}, // adjacent swap is NOT one edit
		{"北京上海", "北京广州", 2},
		{"😀😁😂", "😀😁", 1},
		{"a😀b", "a😃b", 1},
	}
	for _, c := range cases {
		ar, br := []rune(c.a), []rune(c.b)
		if got := levenshtein(ar, br, 64); got != c.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := fullLevenshtein(ar, br); got != c.want {
			t.Errorf("fullLevenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLevenshteinCutoff(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []rune("abcd好😀")
	for i := 0; i < 2000; i++ {
		mk := func(n int) []rune {
			r := make([]rune, n)
			for j := range r {
				r[j] = alphabet[rng.Intn(len(alphabet))]
			}
			return r
		}
		a, b := mk(rng.Intn(20)), mk(rng.Intn(20))
		cutoff := rng.Intn(8)
		got := levenshtein(a, b, cutoff)
		want := fullLevenshtein(a, b)
		if want <= cutoff && got != want {
			t.Fatalf("levenshtein(%q,%q,cut=%d) = %d, want exact %d", string(a), string(b), cutoff, got, want)
		}
		if want > cutoff && got <= cutoff {
			t.Fatalf("levenshtein(%q,%q,cut=%d) = %d, want > %d", string(a), string(b), cutoff, got, cutoff)
		}
	}
}

func TestBoundaryLength20(t *testing.T) {
	base := []rune("aaaaaaaaaaaaaaaaaaaa") // 20 code points
	mk := func(mutate ...int) string {
		r := append([]rune(nil), base...)
		for _, i := range mutate {
			r[i] = 'b'
		}
		return string(r)
	}
	left := []Record{{ID: "L", Key: string(base)}}
	// distance 3 of 20 -> similarity 0.85 exactly -> must match
	checkAgainstBruteForce(t, left, []Record{{ID: "R3", Key: mk(0, 1, 2)}})
	got, err := JoinAll(left, []Record{{ID: "R3", Key: mk(0, 1, 2)}})
	if err != nil || len(got) != 1 || got[0].Distance != 3 {
		t.Fatalf("distance-3 pair: got %+v err=%v, want exactly one pair with Distance=3", got, err)
	}
	// distance 4 of 20 -> similarity 0.80 -> must be excluded
	got, err = JoinAll(left, []Record{{ID: "R4", Key: mk(0, 1, 2, 3)}})
	if err != nil || len(got) != 0 {
		t.Fatalf("distance-4 pair: got %+v err=%v, want no pairs", got, err)
	}
}

func TestIndelMatches(t *testing.T) {
	// 20 vs 19 code points via one deletion: distance 1, must be found.
	left := []Record{{ID: "L", Key: "abcdefghijklmnopqrst"}}
	right := []Record{
		{ID: "del", Key: "abcdefghijklmnpqrst"},   // deleted 'o'? keep valid: 19 chars
		{ID: "ins", Key: "abcdefghijk-lmnopqrst"}, // 21 chars, one insertion
		{ID: "sub3", Key: "abcXefghijYlmnoZqrst"}, // 3 substitutions
	}
	got, err := JoinAll(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v, want 3 pairs (del, ins, sub3)", got)
	}
	checkAgainstBruteForce(t, left, right)
}

func TestDuplicateKeysAndMultiCandidate(t *testing.T) {
	left := []Record{
		{ID: "L1", Key: "sharedkey"},
		{ID: "L2", Key: "sharedkey"},
		{ID: "L3", Key: "sharedkez"},
	}
	right := []Record{
		{ID: "R1", Key: "sharedkey"},
		{ID: "R2", Key: "sharedkey"},
		{ID: "R3", Key: "sharedkey"},
	}
	got, err := JoinAll(left, right)
	if err != nil {
		t.Fatal(err)
	}
	// sharedkey: 2x3 = 6 pairs at distance 0; sharedkez vs sharedkey: 1x3 = 3 pairs.
	if len(got) != 9 {
		t.Fatalf("got %d pairs, want 9: %+v", len(got), got)
	}
	counts := pairSet(got)
	if counts[Pair{LeftID: "L1", RightID: "R1", Distance: 0}] != 1 ||
		counts[Pair{LeftID: "L3", RightID: "R2", Distance: 1}] != 1 {
		t.Fatalf("unexpected pair multiplicities: %+v", got)
	}
	checkAgainstBruteForce(t, left, right)
}

func TestUnicodeAndShortKeys(t *testing.T) {
	left := []Record{
		{ID: "L1", Key: "北京"},
		{ID: "L2", Key: "北京"},
		{ID: "L3", Key: "😀"},
		{ID: "L4", Key: "𠀀𠀁𠀂"}, // supplementary plane
		{ID: "L5", Key: "x"},
	}
	right := []Record{
		{ID: "R1", Key: "北京"},
		{ID: "R2", Key: "北平"},
		{ID: "R3", Key: "😀"},
		{ID: "R4", Key: "𠀀𠀁𠀃"},
		{ID: "R5", Key: "y"},
		{ID: "R6", Key: "😀😀"},
	}
	checkAgainstBruteForce(t, left, right)
}

func TestRandomizedAgainstBruteForce(t *testing.T) {
	alphabets := [][]rune{
		[]rune("ab"),
		[]rune("abcde"),
		[]rune("abc北京😀𠀀"),
		[]rune("abcdefghijklmnopqrstuvwxyz0123456789"),
	}
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 60; trial++ {
		alphabet := alphabets[trial%len(alphabets)]
		mkKey := func() string {
			n := 1 + rng.Intn(24)
			r := make([]rune, n)
			for j := range r {
				r[j] = alphabet[rng.Intn(len(alphabet))]
			}
			return string(r)
		}
		mkSide := func(prefix string, n int) []Record {
			recs := make([]Record, n)
			for i := range recs {
				recs[i] = Record{ID: fmt.Sprintf("%s%d", prefix, i), Key: mkKey()}
			}
			// inject duplicates of an existing key
			if n >= 4 {
				recs[1].Key = recs[0].Key
				recs[2].Key = recs[0].Key
			}
			return recs
		}
		left := mkSide("L", 1+rng.Intn(40))
		right := mkSide("R", 1+rng.Intn(40))
		// inject near-copies of left keys into the right side
		for i := 0; i < len(left) && i < len(right); i += 3 {
			r := []rune(left[i].Key)
			if len(r) > 2 {
				r[rng.Intn(len(r))] = alphabet[rng.Intn(len(alphabet))]
				right[i].Key = string(r)
			}
		}
		checkAgainstBruteForce(t, left, right)
	}
}

func TestEarlyStopSmall(t *testing.T) {
	left := []Record{{ID: "L1", Key: "aaaaaaaaaa"}, {ID: "L2", Key: "aaaaaaaaaa"}}
	right := []Record{{ID: "R1", Key: "aaaaaaaaaa"}, {ID: "R2", Key: "aaaaaaaaab"}}
	var got []Pair
	err := Join(left, right, func(p Pair) bool {
		got = append(got, p)
		return len(got) < 2
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d pairs after early stop, want exactly 2", len(got))
	}
	if pairSet(got)[got[0]] != 1 || got[0] == got[1] {
		t.Fatalf("delivered pairs must be real and distinct: %+v", got)
	}
}

func TestValidation(t *testing.T) {
	long := make([]rune, 65)
	for i := range long {
		long[i] = 'a'
	}
	cases := []struct {
		name        string
		left, right []Record
	}{
		{"empty key", []Record{{ID: "a", Key: ""}}, nil},
		{"too long", []Record{{ID: "a", Key: string(long)}}, nil},
		{"dup id left", []Record{{ID: "a", Key: "x"}, {ID: "a", Key: "y"}}, nil},
		{"dup id right", nil, []Record{{ID: "a", Key: "x"}, {ID: "a", Key: "z"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := JoinAll(c.left, c.right); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
	if err := Join(nil, nil, nil); err == nil {
		t.Fatal("nil yield: want error, got nil")
	}
	// Empty inputs are valid and produce no pairs.
	got, err := JoinAll(nil, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty inputs: got %+v, err=%v", got, err)
	}
}

func TestResultOrderIrrelevantButComplete(t *testing.T) {
	left := []Record{
		{ID: "L1", Key: "hello world this is key one"},
		{ID: "L2", Key: "hello world this is key two"},
	}
	right := []Record{
		{ID: "R1", Key: "hello world this is key one"},
		{ID: "R2", Key: "hello world this is key three"},
	}
	got, err := JoinAll(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %+v, want 4 pairs", got)
	}
	checkAgainstBruteForce(t, left, right)
}
