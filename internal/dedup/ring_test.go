package dedup

import (
	"fmt"
	"strconv"
	"testing"
)

// ---------------------------------------------------------------------------
// Functional tests
// ---------------------------------------------------------------------------

func TestContainsOrInsert_NewEntry(t *testing.T) {
	var r Ring

	if r.ContainsOrInsert(100, "hash_aaa") {
		t.Fatal("first insert must return false (new entry)")
	}
	if r.ContainsOrInsert(200, "hash_bbb") {
		t.Fatal("second distinct insert must return false")
	}
	if r.ContainsOrInsert(300, "hash_ccc") {
		t.Fatal("third distinct insert must return false")
	}
}

func TestContainsOrInsert_Duplicate(t *testing.T) {
	var r Ring

	if r.ContainsOrInsert(100, "hash_aaa") {
		t.Fatal("first insert must return false")
	}
	if !r.ContainsOrInsert(100, "hash_aaa") {
		t.Fatal("duplicate must return true")
	}
	// Insert something else, then re-check the original
	if r.ContainsOrInsert(200, "hash_bbb") {
		t.Fatal("distinct insert must return false")
	}
	if !r.ContainsOrInsert(100, "hash_aaa") {
		t.Fatal("original must still be found as duplicate")
	}
}

func TestContainsOrInsert_SameLtDifferentHash(t *testing.T) {
	var r Ring

	if r.ContainsOrInsert(100, "hash_aaa") {
		t.Fatal("first insert must return false")
	}
	// Same lt, different txHash -- must be treated as distinct
	if r.ContainsOrInsert(100, "hash_bbb") {
		t.Fatal("same lt but different txHash must return false (new entry)")
	}
}

func TestContainsOrInsert_DifferentLtSameHash(t *testing.T) {
	var r Ring

	if r.ContainsOrInsert(100, "hash_aaa") {
		t.Fatal("first insert must return false")
	}
	// Different lt, same txHash -- must be treated as distinct
	if r.ContainsOrInsert(200, "hash_aaa") {
		t.Fatal("different lt but same txHash must return false (new entry)")
	}
}

func TestContainsOrInsert_Eviction(t *testing.T) {
	var r Ring

	// Fill exactly ringSize entries: lt=1..4096
	for i := uint64(1); i <= ringSize; i++ {
		hash := "tx_" + strconv.FormatUint(i, 10)
		if r.ContainsOrInsert(i, hash) {
			t.Fatalf("insert %d must return false", i)
		}
	}

	// All ringSize entries should be present
	for i := uint64(1); i <= ringSize; i++ {
		hash := "tx_" + strconv.FormatUint(i, 10)
		if !r.ContainsOrInsert(i, hash) {
			t.Fatalf("entry %d must be found as duplicate before eviction", i)
		}
	}

	// Insert one more -- this should evict entry #1 (oldest, at ringIdx=0)
	if r.ContainsOrInsert(ringSize+1, "tx_4097") {
		t.Fatal("new entry after full ring must return false")
	}

	// Entry #1 should now be evicted (it occupied ringIdx=0, which was overwritten)
	// Check by probing -- it must not be found. But we cannot call ContainsOrInsert
	// because that would re-insert it and evict entry #2. Instead, verify that
	// the most recent entries are still present and one more insert evicts the next.

	// Entry #2 should still be present (it is at ringIdx=1, not yet evicted)
	if !r.ContainsOrInsert(2, "tx_2") {
		t.Fatal("entry 2 must still be present")
	}

	// Entry at ringSize+1 should be found (just inserted)
	if !r.ContainsOrInsert(ringSize+1, "tx_4097") {
		t.Fatal("entry 4097 must be found as duplicate")
	}

	// Insert another to evict entry #2 (now oldest at ringIdx=1)
	if r.ContainsOrInsert(ringSize+2, "tx_4098") {
		t.Fatal("new entry must return false")
	}

	// Now entry #3 should still be present, but entry #2 should be evicted.
	// Verify entry #3 is present.
	if !r.ContainsOrInsert(3, "tx_3") {
		t.Fatal("entry 3 must still be present after entry 2 eviction")
	}
}

func TestContainsOrInsert_EvictionCycle(t *testing.T) {
	var r Ring

	// Insert 2 * ringSize entries, forcing a full eviction cycle
	for i := range 2 * ringSize {
		hash := "tx_" + strconv.FormatUint(uint64(i)+1, 10)
		r.ContainsOrInsert(uint64(i)+1, hash)
	}

	// First half should all be evicted
	for i := range ringSize {
		hash := "tx_" + strconv.FormatUint(uint64(i)+1, 10)
		if r.ContainsOrInsert(uint64(i)+1, hash) {
			t.Fatalf("entry %d should have been evicted after full cycle", uint64(i)+1)
		}
	}
}

func TestContainsOrInsert_ManyEntries(t *testing.T) {
	var r Ring

	// Fill to capacity and verify all are found
	for i := range ringSize {
		hash := fmt.Sprintf("hash_%016x", i)
		if r.ContainsOrInsert(uint64(i), hash) {
			t.Fatalf("insert %d must return false", i)
		}
	}

	// Verify all entries are present
	for i := range ringSize {
		hash := fmt.Sprintf("hash_%016x", i)
		if !r.ContainsOrInsert(uint64(i), hash) {
			t.Fatalf("entry %d must be found as duplicate", i)
		}
	}
}

func TestContainsOrInsert_WrapAroundMultipleTimes(t *testing.T) {
	var r Ring

	// Run 3x the ring size to exercise multiple wrap-arounds
	total := uint64(3 * ringSize)
	for i := uint64(1); i <= total; i++ {
		hash := "tx_" + strconv.FormatUint(i, 10)
		r.ContainsOrInsert(i, hash)
	}

	// Only the last ringSize entries should be present
	start := total - ringSize + 1
	for i := start; i <= total; i++ {
		hash := "tx_" + strconv.FormatUint(i, 10)
		if !r.ContainsOrInsert(i, hash) {
			t.Fatalf("entry %d (in last ring window) must be found", i)
		}
	}

	// Entries before the window should be gone
	for i := uint64(1); i < start; i++ {
		hash := "tx_" + strconv.FormatUint(i, 10)
		if r.ContainsOrInsert(i, hash) {
			t.Fatalf("entry %d (evicted) must not be found as duplicate", i)
		}
	}
}

func TestContainsOrInsert_EvictionVerifiesAbsence(t *testing.T) {
	// Use a dedicated ring to verify that the oldest entry is truly gone
	// without mutating state by re-inserting it.
	var r Ring

	// Fill the ring
	for i := range ringSize {
		r.ContainsOrInsert(uint64(i)+1, "tx_"+strconv.FormatUint(uint64(i)+1, 10))
	}

	// Insert one more to evict entry 1
	r.ContainsOrInsert(ringSize+1, "tx_evict")

	// Probe the hash table directly for entry 1's hash.
	// This is a white-box test: we look up the hash without calling ContainsOrInsert.
	h := fnv1a(1, "tx_1")
	idx := uint32(h) & slotMask
	found := false
	for {
		s := r.slots[idx]
		if !s.occupied {
			break
		}
		if s.lt == 1 && s.hashKey == h {
			found = true
			break
		}
		idx = (idx + 1) & slotMask
	}
	if found {
		t.Fatal("entry 1 must have been evicted from hash table, but was found")
	}
}

// ---------------------------------------------------------------------------
// FNV-1a tests
// ---------------------------------------------------------------------------

func TestFnv1a_Consistency(t *testing.T) {
	h1 := fnv1a(42, "hello")
	h2 := fnv1a(42, "hello")
	if h1 != h2 {
		t.Fatalf("same input must produce same hash: %d != %d", h1, h2)
	}
}

func TestFnv1a_Distribution(t *testing.T) {
	const n = 10000
	seen := make(map[uint64]struct{}, n)
	collisions := 0
	for i := range n {
		h := fnv1a(uint64(i), "tx_"+strconv.FormatUint(uint64(i), 10))
		if _, ok := seen[h]; ok {
			collisions++
		}
		seen[h] = struct{}{}
	}
	// With 64-bit hash and 10k entries, we expect zero collisions.
	// Allow up to 1 as statistical margin.
	if collisions > 1 {
		t.Fatalf("too many FNV-1a collisions: %d out of %d", collisions, n)
	}
}

func TestFnv1a_DifferentInputs(t *testing.T) {
	h1 := fnv1a(1, "aaa")
	h2 := fnv1a(2, "aaa")
	h3 := fnv1a(1, "bbb")
	if h1 == h2 {
		t.Fatal("different lt must produce different hashes")
	}
	if h1 == h3 {
		t.Fatal("different txHash must produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// slotBetween tests
// ---------------------------------------------------------------------------

func TestSlotBetween(t *testing.T) {
	tests := []struct {
		lo, mid, hi uint32
		want        bool
	}{
		// Non-wrapping: (2, 5]
		{2, 3, 5, true},
		{2, 5, 5, true},
		{2, 2, 5, false}, // lo == mid, exclusive on lo
		{2, 6, 5, false},
		{2, 1, 5, false},
		// Wrapping: lo > hi means range wraps around
		{5, 6, 2, true},  // 6 is after 5
		{5, 1, 2, true},  // 1 is in wrapped range
		{5, 2, 2, true},  // hi inclusive
		{5, 5, 2, false}, // lo exclusive
		{5, 3, 2, false}, // 3 is between hi+1 and lo
	}
	for _, tt := range tests {
		got := slotBetween(tt.lo, tt.mid, tt.hi)
		if got != tt.want {
			t.Errorf("slotBetween(%d, %d, %d) = %v, want %v",
				tt.lo, tt.mid, tt.hi, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkContainsOrInsert(b *testing.B) {
	var r Ring
	// Pre-generate keys to avoid string allocation in the benchmark loop
	type key struct {
		lt   uint64
		hash string
	}
	keys := make([]key, b.N)
	for i := range keys {
		keys[i] = key{
			lt:   uint64(i),
			hash: "tx_" + strconv.Itoa(i),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		r.ContainsOrInsert(keys[i].lt, keys[i].hash)
	}
}

func BenchmarkContains_Hit(b *testing.B) {
	var r Ring
	// Pre-fill with entries we will look up
	const fillSize = 1024
	type key struct {
		lt   uint64
		hash string
	}
	keys := make([]key, fillSize)
	for i := range keys {
		keys[i] = key{
			lt:   uint64(i + 1),
			hash: "tx_" + strconv.Itoa(i+1),
		}
		r.ContainsOrInsert(keys[i].lt, keys[i].hash)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		k := keys[i%fillSize]
		if !r.ContainsOrInsert(k.lt, k.hash) {
			b.Fatal("expected hit")
		}
	}
}

func BenchmarkContainsOrInsert_FullRing(b *testing.B) {
	var r Ring
	// Pre-fill ring to capacity so every insert triggers eviction
	for i := range ringSize {
		r.ContainsOrInsert(uint64(i), "prefill_"+strconv.FormatUint(uint64(i), 10))
	}

	// Pre-generate keys for inserts beyond ring capacity
	type key struct {
		lt   uint64
		hash string
	}
	keys := make([]key, b.N)
	for i := range keys {
		keys[i] = key{
			lt:   uint64(ringSize + i),
			hash: "bench_" + strconv.Itoa(i),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		r.ContainsOrInsert(keys[i].lt, keys[i].hash)
	}
}
