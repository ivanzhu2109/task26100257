package fuzzyjoin

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// peakSampler tracks peak heap allocation in the background. Call stop
// before reading peak.
type peakSampler struct {
	peak    atomic.Uint64
	done    chan struct{}
	stopped chan struct{}
}

func newPeakSampler() *peakSampler {
	s := &peakSampler{done: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(s.stopped)
		var mem runtime.MemStats
		for {
			select {
			case <-s.done:
				return
			default:
				runtime.ReadMemStats(&mem)
				for {
					cur := s.peak.Load()
					if mem.Alloc <= cur || s.peak.CompareAndSwap(cur, mem.Alloc) {
						break
					}
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	return s
}

func (s *peakSampler) stop() uint64 {
	close(s.done)
	<-s.stopped
	return s.peak.Load()
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

func randString(rng *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rng.Intn(len(letterBytes))]
	}
	return string(b)
}

// genPerfBatch builds the standard performance batch: 50k records per
// source, keys of 28-36 code points, 80% sharing one 8-code-point prefix,
// and at least 5000 left records with a 1-4 edit variant on the right.
func genPerfBatch(rng *rand.Rand) (left, right []Record, wantPairs map[Pair]struct{}) {
	const n = 50000
	const variants = 6000
	prefix := randString(rng, 8)

	left = make([]Record, n)
	for i := range left {
		key := randString(rng, 28+rng.Intn(9))
		if rng.Float64() < 0.8 {
			key = prefix + key[8:]
		}
		left[i] = Record{ID: fmt.Sprintf("L%d", i), Key: key}
	}

	right = make([]Record, 0, n)
	wantPairs = make(map[Pair]struct{}, variants)
	for i := 0; i < variants; i++ {
		r := []rune(left[i].Key)
		edits := 1 + rng.Intn(4)
		for e := 0; e < edits; e++ {
			pos := rng.Intn(len(r))
			switch rng.Intn(3) {
			case 0: // substitute
				r[pos] = rune(letterBytes[rng.Intn(len(letterBytes))])
			case 1: // delete
				r = append(r[:pos], r[pos+1:]...)
			case 2: // insert
				r = append(r[:pos], append([]rune{rune(letterBytes[rng.Intn(len(letterBytes))])}, r[pos:]...)...)
			}
		}
		right = append(right, Record{ID: fmt.Sprintf("R%d", len(right)), Key: string(r)})
		wantPairs[Pair{LeftID: left[i].ID, RightID: right[len(right)-1].ID, Distance: -1}] = struct{}{}
	}
	for len(right) < n {
		key := randString(rng, 28+rng.Intn(9))
		if rng.Float64() < 0.8 {
			key = prefix + key[8:]
		}
		right = append(right, Record{ID: fmt.Sprintf("R%d", len(right)), Key: key})
	}
	return left, right, wantPairs
}

func TestPerfDenseSharedPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in -short mode")
	}
	if raceEnabled {
		t.Skip("timing and memory assertions are meaningless under -race")
	}
	rng := rand.New(rand.NewSource(7))
	left, right, wantPairs := genPerfBatch(rng)

	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	baseAlloc := mem.Alloc
	sampler := newPeakSampler()

	start := time.Now()
	count := 0
	found := 0
	byID := make(map[string]string, len(left)+len(right))
	for _, r := range left {
		byID["L"+r.ID[1:]] = r.Key
	}
	for _, r := range right {
		byID["R"+r.ID[1:]] = r.Key
	}
	err := Join(left, right, func(p Pair) bool {
		count++
		if _, ok := wantPairs[Pair{LeftID: p.LeftID, RightID: p.RightID, Distance: -1}]; ok {
			found++
		}
		lr, rr := []rune(byID[p.LeftID]), []rune(byID[p.RightID])
		m := len(lr)
		if len(rr) > m {
			m = len(rr)
		}
		if d := fullLevenshtein(lr, rr); d != p.Distance || d > maxDistance(m) {
			t.Errorf("pair %+v is not a real match (recomputed distance %d)", p, d)
		}
		return true
	})
	elapsed := time.Since(start)
	peak := sampler.stop()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("elapsed=%v pairs=%d peakAlloc=%.1fMiB baseAlloc=%.1fMiB", elapsed, count, float64(peak)/(1<<20), float64(baseAlloc)/(1<<20))
	if elapsed > 30*time.Second {
		t.Fatalf("join took %v, want <= 30s", elapsed)
	}
	if peak > 256<<20 {
		t.Fatalf("peak Alloc = %.1f MiB, want <= 256 MiB", float64(peak)/(1<<20))
	}
	if count > 100000 {
		t.Fatalf("produced %d pairs, test data should yield <= 100000", count)
	}
	if found < 5000 {
		t.Fatalf("only %d planted variant pairs found, want >= 5000", found)
	}
}

func TestEarlyStopHugeResult(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in -short mode")
	}
	const n = 10000
	key := "shared-prefix-" + randString(rand.New(rand.NewSource(3)), 18)
	left := make([]Record, n)
	right := make([]Record, n)
	for i := 0; i < n; i++ {
		left[i] = Record{ID: fmt.Sprintf("L%d", i), Key: key}
		right[i] = Record{ID: fmt.Sprintf("R%d", i), Key: key}
	}

	runtime.GC()
	sampler := newPeakSampler()

	start := time.Now()
	seen := make(map[Pair]struct{}, 1000)
	err := Join(left, right, func(p Pair) bool {
		if p.Distance != 0 {
			t.Errorf("pair %+v has distance %d, want 0", p, p.Distance)
		}
		if _, dup := seen[p]; dup {
			t.Errorf("duplicate pair delivered: %+v", p)
		}
		seen[p] = struct{}{}
		return len(seen) < 1000
	})
	elapsed := time.Since(start)
	peak := sampler.stop()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("elapsed=%v consumed=%d peakAlloc=%.1fMiB", elapsed, len(seen), float64(peak)/(1<<20))
	if len(seen) != 1000 {
		t.Fatalf("consumed %d pairs, want exactly 1000", len(seen))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("early-stopped join took %v, want <= 2s", elapsed)
	}
	if peak > 256<<20 {
		t.Fatalf("peak Alloc = %.1f MiB, want <= 256 MiB", float64(peak)/(1<<20))
	}
}
