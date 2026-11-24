// Aether's sync.Map (Batch 1). gc's is a concurrent hash trie
// (internal/sync.HashTrieMap); Aether runs every goroutine on one thread
// and switches only when one blocks or yields (D-048), so a map does. Its
// behavior is gc's: keys hashed, and values compared, as interface values,
// with the runtime's panics for an unhashable key or an uncomparable value;
// Range visits the entries in Aether's map order (V-8), where gc's order is
// its trie's.

package sync

// Map is like a Go map[any]any but is safe for concurrent use by multiple
// goroutines. The zero Map is empty and ready for use.
type Map struct {
	m map[any]any
}

// Load returns the value stored in the map for a key, or nil if no value is
// present. The ok result indicates whether value was found in the map.
func (m *Map) Load(key any) (value any, ok bool) {
	value, ok = m.m[key]
	return value, ok
}

// Store sets the value for a key.
func (m *Map) Store(key, value any) {
	m.Swap(key, value)
}

// Clear deletes all the entries, resulting in an empty Map.
func (m *Map) Clear() {
	clear(m.m)
}

// LoadOrStore returns the existing value for the key if present. Otherwise,
// it stores and returns the given value. The loaded result is true if the
// value was loaded, false if stored.
func (m *Map) LoadOrStore(key, value any) (actual any, loaded bool) {
	if v, ok := m.m[key]; ok {
		return v, true
	}
	m.set(key, value)
	return value, false
}

// LoadAndDelete deletes the value for a key, returning the previous value if
// any. The loaded result reports whether the key was present.
func (m *Map) LoadAndDelete(key any) (value any, loaded bool) {
	value, loaded = m.m[key]
	delete(m.m, key)
	return value, loaded
}

// Delete deletes the value for a key.
func (m *Map) Delete(key any) {
	delete(m.m, key)
}

// Swap swaps the value for a key and returns the previous value if any. The
// loaded result reports whether the key was present.
func (m *Map) Swap(key, value any) (previous any, loaded bool) {
	previous, loaded = m.m[key]
	m.set(key, value)
	return previous, loaded
}

// CompareAndSwap swaps the old and new values for key if the value stored
// in the map is equal to old. The old value must be of a comparable type.
func (m *Map) CompareAndSwap(key, old, new any) (swapped bool) {
	v, ok := m.m[key]
	if !ok || v != old {
		return false
	}
	m.m[key] = new
	return true
}

// CompareAndDelete deletes the entry for key if its value is equal to old.
// The old value must be of a comparable type.
func (m *Map) CompareAndDelete(key, old any) (deleted bool) {
	v, ok := m.m[key]
	if !ok || v != old {
		return false
	}
	delete(m.m, key)
	return true
}

// Range calls f sequentially for each key and value present in the map. If
// f returns false, range stops the iteration.
func (m *Map) Range(f func(key, value any) bool) {
	for k, v := range m.m {
		if !f(k, v) {
			break
		}
	}
}

func (m *Map) set(key, value any) {
	if m.m == nil {
		m.m = map[any]any{}
	}
	m.m[key] = value
}
