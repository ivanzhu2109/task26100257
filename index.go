package fuzzyjoin

import "sort"

// gramQuantum is the q-gram length used by the inverted index.
const gramQuantum = 3

// keyGroup groups all records sharing one distinct key.
type keyGroup struct {
	key   string
	runes []rune
	ids   []int32
}

// packGram packs three code points (each <= 0x10FFFF, 21 bits) into an
// exact, collision-free 63-bit key.
func packGram(a, b, c rune) int64 {
	return int64(a)<<42 | int64(b)<<21 | int64(c)
}

// gramCount is a distinct trigram and its multiplicity in one key.
type gramCount struct {
	gram int64
	mult int32
}

// trigramCounts returns the sorted distinct trigrams of a key with their
// multiplicities. Keys shorter than gramQuantum yield nil.
func trigramCounts(runes []rune) []gramCount {
	n := len(runes) - gramQuantum + 1
	if n <= 0 {
		return nil
	}
	grams := make([]int64, n)
	for i := 0; i < n; i++ {
		grams[i] = packGram(runes[i], runes[i+1], runes[i+2])
	}
	sort.Slice(grams, func(i, j int) bool { return grams[i] < grams[j] })
	out := make([]gramCount, 0, n)
	for _, g := range grams {
		if m := len(out); m > 0 && out[m-1].gram == g {
			out[m-1].mult++
		} else {
			out = append(out, gramCount{gram: g, mult: 1})
		}
	}
	return out
}

// gramEntry is one trigram occurrence in one left-side distinct key.
type gramEntry struct {
	gram int64
	key  int32
}

// gramIndex is a trigram inverted index over the left source's distinct
// keys, stored as a sorted slice to keep memory flat.
type gramIndex struct {
	entries []gramEntry     // sorted by (gram, key)
	byLen   map[int][]int32 // distinct-key indices grouped by rune length
}

func buildGramIndex(keys []keyGroup) *gramIndex {
	total := 0
	for i := range keys {
		if n := len(keys[i].runes) - gramQuantum + 1; n > 0 {
			total += n
		}
	}
	idx := &gramIndex{
		entries: make([]gramEntry, 0, total),
		byLen:   make(map[int][]int32),
	}
	for i := range keys {
		r := keys[i].runes
		idx.byLen[len(r)] = append(idx.byLen[len(r)], int32(i))
		if len(r) < gramQuantum {
			continue
		}
		for j := 0; j+gramQuantum <= len(r); j++ {
			idx.entries = append(idx.entries, gramEntry{
				gram: packGram(r[j], r[j+1], r[j+2]),
				key:  int32(i),
			})
		}
	}
	sort.Slice(idx.entries, func(i, j int) bool {
		if idx.entries[i].gram != idx.entries[j].gram {
			return idx.entries[i].gram < idx.entries[j].gram
		}
		return idx.entries[i].key < idx.entries[j].key
	})
	return idx
}

// postings returns the entry range [start, end) holding one trigram.
func (idx *gramIndex) postings(gram int64) (int, int) {
	start := sort.Search(len(idx.entries), func(i int) bool {
		return idx.entries[i].gram >= gram
	})
	end := start
	for end < len(idx.entries) && idx.entries[end].gram == gram {
		end++
	}
	return start, end
}

// counter accumulates per-key trigram overlap counts for one query,
// using generation stamps to avoid clearing the arrays per query.
type counter struct {
	stamp   []uint32
	count   []int32
	gen     uint32
	touched []int32
}

func newCounter(n int) *counter {
	return &counter{
		stamp: make([]uint32, n),
		count: make([]int32, n),
	}
}

func (c *counter) reset() {
	c.gen++
	c.touched = c.touched[:0]
}

func (c *counter) add(key int32, delta int32) {
	if c.stamp[key] != c.gen {
		c.stamp[key] = c.gen
		c.count[key] = delta
		c.touched = append(c.touched, key)
	} else {
		c.count[key] += delta
	}
}

func (c *counter) get(key int32) int32 {
	if c.stamp[key] != c.gen {
		return 0
	}
	return c.count[key]
}
