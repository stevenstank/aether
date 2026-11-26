// Package fips140cache provides a weak map that associates the lifetime of
// values with the lifetime of keys. Aether's (v2 evaluation): gc's keys
// its map by weak pointers, whose entries a cleanup removes when the key
// is collected; Aether's collector has neither, so the map holds its keys,
// and an entry lives as long as the program. Get returns what gc's does.
package fips140cache

import "sync"

// Cache is a map that associates the lifetime of values with the lifetime
// of keys.
type Cache[K, V any] struct {
	m sync.Map
}

// Get returns the result of new, for an associated key k.
//
// If Get was called with k before and didn't return an error, Get may
// return the same value it returned from the previous call if check
// returns true on it. If check returns false, Get will call new again and
// return the result.
func (c *Cache[K, V]) Get(k *K, new func() (*V, error), check func(*V) bool) (*V, error) {
	if cached, ok := c.m.Load(k); ok {
		v := cached.(*V)
		if check(v) {
			return v, nil
		}
	}
	v, err := new()
	if err != nil {
		return nil, err
	}
	c.m.Store(k, v)
	return v, nil
}
