package fuzzyjoin

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

const denseAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

var densePrefix = []rune("qk7z2mx9") // 8 码点公共前缀

// genDenseBatch 生成性能验收分布：键长 28-36 码点，80% 共享同一 8 码点前缀，
// 其余码点在字母数字中均匀分布；right 中注入 left 的 1-4 次编辑变体。
func genDenseBatch(seed int64, n, numVariants int) (left, right []Record) {
	rng := rand.New(rand.NewSource(seed))
	mkKey := func() string {
		k := 28 + rng.Intn(9) // 28..36
		s := make([]rune, k)
		start := 0
		if rng.Intn(5) < 4 { // 80%
			copy(s, densePrefix)
			start = len(densePrefix)
		}
		for i := start; i < k; i++ {
			s[i] = rune(denseAlphabet[rng.Intn(len(denseAlphabet))])
		}
		return string(s)
	}
	left = make([]Record, n)
	for i := range left {
		left[i] = Record{ID: fmt.Sprintf("L%d", i), Key: mkKey()}
	}
	right = make([]Record, 0, n)
	for i := 0; i+1 < n-numVariants || (numVariants == 0 && i < n); i++ {
		right = append(right, Record{ID: fmt.Sprintf("R%d", len(right)), Key: mkKey()})
	}
	// 注入变体：对 left 中若干记录做 1-4 次编辑后放入 right。
	for v := 0; v < numVariants; v++ {
		src := []rune(left[rng.Intn(len(left))].Key)
		edits := 1 + rng.Intn(4)
		for e := 0; e < edits; e++ {
			pos := rng.Intn(len(src))
			switch rng.Intn(3) {
			case 0: // 替换
				c := denseAlphabet[rng.Intn(len(denseAlphabet))]
				for rune(c) == src[pos] {
					c = denseAlphabet[rng.Intn(len(denseAlphabet))]
				}
				src[pos] = rune(c)
			case 1: // 插入
				c := rune(denseAlphabet[rng.Intn(len(denseAlphabet))])
				src = append(src[:pos], append([]rune{c}, src[pos:]...)...)
			default: // 删除
				src = append(src[:pos], src[pos+1:]...)
			}
		}
		right = append(right, Record{ID: fmt.Sprintf("R%d", len(right)), Key: string(src)})
	}
	return left, right
}

// peakSampler 后台采样进程堆内存，返回峰值（字节）。
func peakSampler(interval time.Duration) (stop func() uint64) {
	done := make(chan struct{})
	var peak uint64
	go func() {
		var ms runtime.MemStats
		for {
			select {
			case <-done:
				return
			default:
				runtime.ReadMemStats(&ms)
				if ms.HeapInuse+ms.StackInuse > peak {
					peak = ms.HeapInuse + ms.StackInuse
				}
				time.Sleep(interval)
			}
		}
	}()
	return func() uint64 { close(done); return peak }
}

// TestPerfDensePrefixBatch 标准性能批次：每源 5 万条，30 秒与 256 MiB 上限。
// 同时校验结果完整（注入变体全部找到）且每对距离真实。
func TestPerfDensePrefixBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping dense perf batch in short mode")
	}
	left, right := genDenseBatch(20261002, 50000, 5000)
	runtime.GC()
	stop := peakSampler(2 * time.Millisecond)
	start := time.Now()
	count := 0
	verified := 0
	Join(left, right, func(p Pair) bool {
		count++
		if verified < 20000 { // 抽样逐对复核距离与阈值
			lr := []rune(left[atoi(p.LeftID)].Key)
			rr := []rune(right[atoi(p.RightID)].Key)
			L := len(lr)
			if len(rr) > L {
				L = len(rr)
			}
			if d := levFull(lr, rr); d != p.Distance || 20*d > 3*L {
				t.Fatalf("pair %v failed verification", p)
			}
			verified++
		}
		return true
	})
	elapsed := time.Since(start)
	peak := stop()
	t.Logf("pairs=%d elapsed=%v peakHeap=%.1fMiB", count, elapsed, float64(peak)/(1<<20))
	if count < 5000 {
		t.Errorf("expected at least 5000 injected variant pairs, got %d", count)
	}
	if count > 100000 {
		t.Errorf("expected at most 100000 pairs, got %d", count)
	}
	if elapsed > 30*time.Second {
		t.Errorf("elapsed %v exceeds 30s", elapsed)
	}
	if peak > 256<<20 {
		t.Errorf("peak heap %.1fMiB exceeds 256MiB", float64(peak)/(1<<20))
	}
}

// TestDenseBatchMatchesBruteForce 缩小版密集批次与逐对参照完全一致。
func TestDenseBatchMatchesBruteForce(t *testing.T) {
	left, right := genDenseBatch(99, 1500, 300)
	checkAgainstBruteForce(t, left, right)
}

// TestPerfEarlyStopHugeResult 两侧各 1 万条相同键，潜在结果 1 亿对；
// 只消费前 1000 对，须在 2 秒内结束，且已消费对真实、互不重复。
func TestPerfEarlyStopHugeResult(t *testing.T) {
	key := "客户名册对齐专用匹配键"
	left := make([]Record, 10000)
	right := make([]Record, 10000)
	for i := range left {
		left[i] = Record{ID: fmt.Sprintf("L%d", i), Key: key}
		right[i] = Record{ID: fmt.Sprintf("R%d", i), Key: key}
	}
	leftIDs := make(map[string]bool, 10000)
	rightIDs := make(map[string]bool, 10000)
	for _, r := range left {
		leftIDs[r.ID] = true
	}
	for _, r := range right {
		rightIDs[r.ID] = true
	}
	type pk struct{ l, r string }
	seen := make(map[pk]bool, 1000)
	start := time.Now()
	count := 0
	Join(left, right, func(p Pair) bool {
		count++
		if p.Distance != 0 || !leftIDs[p.LeftID] || !rightIDs[p.RightID] {
			t.Fatalf("consumed pair not real: %v", p)
		}
		k := pk{p.LeftID, p.RightID}
		if seen[k] {
			t.Fatalf("duplicate pair: %v", p)
		}
		seen[k] = true
		return count < 1000
	})
	elapsed := time.Since(start)
	t.Logf("consumed=%d elapsed=%v", count, elapsed)
	if count != 1000 {
		t.Fatalf("expected exactly 1000 consumed pairs, got %d", count)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("call did not finish within 2s: %v", elapsed)
	}
}

func atoi(s string) int {
	n := 0
	for i := 1; i < len(s); i++ { // 跳过首字母 L/R
		n = n*10 + int(s[i]-'0')
	}
	return n
}
