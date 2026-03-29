package dedup

const (
	ringSize = 4096         // must be power of 2
	ringMask = ringSize - 1 // bitmask for ring index
	slotSize = 8192         // must be power of 2, >= 2*ringSize for load factor <= 0.5
	slotMask = slotSize - 1 // bitmask for slot index
)

// entry stores a ring buffer element: the composite key for eviction lookup.
// 16 bytes, fits in a single fetch alongside its neighbor.
type entry struct {
	lt      uint64 // transaction logical time
	hashKey uint64 // precomputed FNV-1a hash for O(1) eviction lookup
}

// slot is an open-addressing hash table bucket.
// 24 bytes with padding. The occupied flag + padding keeps the struct
// at a predictable alignment boundary.
type slot struct {
	lt       uint64  // key part 1: logical time
	hashKey  uint64  // key part 2: precomputed hash (avoids rehashing on lookup)
	ringIdx  uint32  // back-reference to ring position for eviction
	occupied bool    // whether this slot holds a live entry
	_        [3]byte // explicit padding to 24 bytes
}

// Ring is a fixed-size deduplication filter using a ring buffer
// backed by an open-addressing hash table.
//
// Design constraints:
//   - Single-writer only (no locks, no atomics). Called from one SSE goroutine.
//   - Zero heap allocations on the hot path (ContainsOrInsert).
//   - All small functions kept under 80 AST nodes for inlining.
//   - Open addressing with linear probing; load factor capped at 0.5.
//   - Ring wrap evicts the oldest entry from the hash table.
type Ring struct {
	entries [ringSize]entry // ring buffer of seen transactions
	slots   [slotSize]slot  // open-addressing hash table
	pos     uint32          // monotonic write position into ring
}

// fnv1a computes FNV-1a hash of lt and txHash.
// Kept small for inlining (~30 AST nodes).
// Mixes lt as two 32-bit halves, then folds in each txHash byte.
func fnv1a(lt uint64, txHash string) uint64 {
	h := uint64(14695981039346656037) // FNV offset basis
	// Mix lower 32 bits of lt
	h ^= lt & 0xFFFFFFFF
	h *= 1099511628211 // FNV prime
	// Mix upper 32 bits of lt
	h ^= lt >> 32
	h *= 1099511628211
	// Fold in each byte of txHash
	for i := 0; i < len(txHash); i++ {
		h ^= uint64(txHash[i])
		h *= 1099511628211
	}
	return h
}

// ContainsOrInsert checks if (lt, txHash) was already seen.
// Returns true if duplicate (already present), false if new (and inserts it).
//
// Zero allocations. Single-writer safe. No defer, no interfaces, no closures.
func (r *Ring) ContainsOrInsert(lt uint64, txHash string) bool {
	h := fnv1a(lt, txHash)

	// --- Probe hash table for existing entry ---
	idx := uint32(h) & slotMask
	for {
		s := &r.slots[idx]
		if !s.occupied {
			break
		}
		if s.lt == lt && s.hashKey == h {
			return true // duplicate found
		}
		idx = (idx + 1) & slotMask
	}

	// --- Not found: insert into ring buffer and hash table ---
	ringIdx := r.pos & ringMask

	// Evict oldest entry when ring has wrapped
	if r.pos >= ringSize {
		old := &r.entries[ringIdx]
		r.evict(old.lt, old.hashKey)
	}

	// Write new entry into ring
	r.entries[ringIdx] = entry{lt: lt, hashKey: h}

	// Insert into hash table (reuse idx from probe above — it stopped at empty slot,
	// but eviction may have shifted things, so re-probe from home slot)
	idx = uint32(h) & slotMask
	for r.slots[idx].occupied {
		idx = (idx + 1) & slotMask
	}
	r.slots[idx] = slot{
		lt:       lt,
		hashKey:  h,
		ringIdx:  ringIdx,
		occupied: true,
	}

	r.pos++
	return false
}

// evict removes an entry from the hash table and rehashes the trailing
// probe chain to maintain linear probing invariants.
// Called only on ring wrap — at most once per ContainsOrInsert.
func (r *Ring) evict(lt uint64, hashKey uint64) {
	idx := uint32(hashKey) & slotMask
	for {
		s := &r.slots[idx]
		if !s.occupied {
			return // entry already gone (should not happen in correct usage)
		}
		if s.lt == lt && s.hashKey == hashKey {
			// Found the entry to evict — remove it
			r.slots[idx].occupied = false
			// Rehash subsequent entries in the probe chain.
			// This is the standard "backward shift deletion" for open addressing.
			r.rehashFrom((idx + 1) & slotMask)
			return
		}
		idx = (idx + 1) & slotMask
	}
}

// rehashFrom fixes the probe chain starting at position start
// after a deletion. Moves entries closer to their home slot
// when the gap allows it (standard backward-shift deletion).
func (r *Ring) rehashFrom(start uint32) {
	gap := (start - 1) & slotMask // position of the gap (just deleted)
	idx := start
	for {
		s := &r.slots[idx]
		if !s.occupied {
			return // end of probe chain
		}
		home := uint32(s.hashKey) & slotMask
		// Standard backward-shift condition: move entry into the gap
		// if its home slot is not strictly between (gap, idx] circularly.
		// This means the entry "wants" to be at or before gap.
		if !slotBetween(gap, home, idx) {
			r.slots[gap] = r.slots[idx]
			r.slots[idx].occupied = false
			gap = idx
		}
		idx = (idx + 1) & slotMask
	}
}

// slotBetween returns true if 'mid' is circularly in the half-open
// range (lo, hi] within the slot index space.
// Inlinable: ~10 AST nodes.
func slotBetween(lo, mid, hi uint32) bool {
	if lo < hi {
		return lo < mid && mid <= hi
	}
	return lo < mid || mid <= hi
}
