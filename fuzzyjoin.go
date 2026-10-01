// Package fuzzyjoin finds all cross-source record pairs whose match keys
// are similar (Levenshtein similarity >= 0.85) between two static sources
// held in process memory.
package fuzzyjoin

import "fmt"

// maxKeyLen is the maximum match-key length in Unicode code points.
const maxKeyLen = 64

// Record is a single input row from one source.
type Record struct {
	ID  string
	Key string
}

// Pair is one qualifying cross-source record pair.
type Pair struct {
	LeftID   string
	RightID  string
	Distance int
}

// Join emits every cross-source record pair whose keys have similarity
// >= 0.85. yield is called once per pair; returning false stops the join
// early and Join returns nil immediately.
func Join(left, right []Record, yield func(Pair) bool) error {
	if yield == nil {
		return fmt.Errorf("fuzzyjoin: yield function must not be nil")
	}
	lkeys, err := groupKeys("left", left)
	if err != nil {
		return err
	}
	rkeys, err := groupKeys("right", right)
	if err != nil {
		return err
	}
	idx := buildGramIndex(lkeys)
	cnt := newCounter(len(lkeys))
	skipCap := len(lkeys) / 16
	if skipCap < 64 {
		skipCap = 64
	}
	for ri := range rkeys {
		if !joinOne(lkeys, left, right, idx, cnt, skipCap, &rkeys[ri], yield) {
			return nil
		}
	}
	return nil
}

// groupKeys validates one source and groups its records by distinct key.
func groupKeys(side string, recs []Record) ([]keyGroup, error) {
	groups := make([]keyGroup, 0, len(recs))
	byKey := make(map[string]int32, len(recs))
	seenID := make(map[string]struct{}, len(recs))
	for i, r := range recs {
		if _, dup := seenID[r.ID]; dup {
			return nil, fmt.Errorf("fuzzyjoin: %s source: duplicate record ID %q", side, r.ID)
		}
		seenID[r.ID] = struct{}{}
		n := 0
		for range r.Key {
			n++
		}
		if n == 0 || n > maxKeyLen {
			return nil, fmt.Errorf("fuzzyjoin: %s source: record %q has key of %d code points, want 1..%d", side, r.ID, n, maxKeyLen)
		}
		if gi, ok := byKey[r.Key]; ok {
			groups[gi].ids = append(groups[gi].ids, int32(i))
			continue
		}
		byKey[r.Key] = int32(len(groups))
		groups = append(groups, keyGroup{
			key:   r.Key,
			runes: []rune(r.Key),
			ids:   []int32{int32(i)},
		})
	}
	return groups, nil
}

// joinOne finds every left key similar to one right key and emits all
// record pairs. It reports whether the join should continue.
func joinOne(lkeys []keyGroup, left, right []Record, idx *gramIndex, cnt *counter, skipCap int, rb *keyGroup, yield func(Pair) bool) bool {
	lb := len(rb.runes)
	grams := trigramCounts(rb.runes)

	// Accumulate trigram overlap counts, skipping overly frequent grams
	// (their multiplicity in b is subtracted from the threshold instead).
	cnt.reset()
	skipped := int32(0)
	for _, g := range grams {
		start, end := idx.postings(g.gram)
		if n := end - start; n == 0 || n > skipCap {
			skipped += g.mult
			continue
		}
		for e := start; e < end; {
			k := idx.entries[e].key
			ma := int32(1)
			for e+1 < end && idx.entries[e+1].key == k {
				e++
				ma++
			}
			e++
			if ma < g.mult {
				cnt.add(k, ma)
			} else {
				cnt.add(k, g.mult)
			}
		}
	}

	// If the adjusted threshold can drop to zero for some compatible
	// length, untouched keys may qualify: fall back to exhaustive verify.
	fallback := grams == nil
	if !fallback {
		for l := 1; l <= maxKeyLen; l++ {
			if _, ok := idx.byLen[l]; !ok {
				continue
			}
			m := l
			if lb > m {
				m = lb
			}
			if abs(l-lb) > maxDistance(m) {
				continue
			}
			if int32(m-gramQuantum+1-gramQuantum*maxDistance(m))-skipped <= 0 {
				fallback = true
				break
			}
		}
	}

	emit := func(lk *keyGroup, d int) bool {
		for _, li := range lk.ids {
			for _, ri := range rb.ids {
				if !yield(Pair{
					LeftID:   left[li].ID,
					RightID:  right[ri].ID,
					Distance: d,
				}) {
					return false
				}
			}
		}
		return true
	}
	verify := func(ki int32) bool {
		lk := &lkeys[ki]
		m := len(lk.runes)
		if lb > m {
			m = lb
		}
		dmax := maxDistance(m)
		if abs(len(lk.runes)-lb) > dmax {
			return true
		}
		if d := levenshtein(lk.runes, rb.runes, dmax); d <= dmax {
			return emit(lk, d)
		}
		return true
	}

	if fallback {
		for l := 1; l <= maxKeyLen; l++ {
			bucket, ok := idx.byLen[l]
			if !ok {
				continue
			}
			m := l
			if lb > m {
				m = lb
			}
			if abs(l-lb) > maxDistance(m) {
				continue
			}
			for _, ki := range bucket {
				if !verify(ki) {
					return false
				}
			}
		}
		return true
	}
	for _, ki := range cnt.touched {
		lk := &lkeys[ki]
		m := len(lk.runes)
		if lb > m {
			m = lb
		}
		threshold := int32(m-gramQuantum+1-gramQuantum*maxDistance(m)) - skipped
		if cnt.get(ki) < threshold {
			continue
		}
		if !verify(ki) {
			return false
		}
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// JoinAll collects all qualifying pairs into a slice.
func JoinAll(left, right []Record) ([]Pair, error) {
	var out []Pair
	err := Join(left, right, func(p Pair) bool {
		out = append(out, p)
		return true
	})
	return out, err
}
