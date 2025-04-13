package mgr

import "sync"

type SyncMap[K comparable, V any] struct {
	lock *sync.RWMutex
	raw  map[K]V
}

func NewSyncMap[K comparable, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{
		lock: &sync.RWMutex{},
		raw:  make(map[K]V),
	}
}

func (m *SyncMap[K, V]) Has(key K) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, ok := m.raw[key]
	return ok
}

func (m *SyncMap[K, V]) Get(key K) (value V, ok bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	value, ok = m.raw[key]
	return
}

func (m *SyncMap[K, V]) Put(key K, value V) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.raw[key] = value
}

func (m *SyncMap[K, V]) Delete(key K) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.raw, key)
}

func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for key, value := range m.raw {
		if !f(key, value) {
			break
		}
	}
}
func (m *SyncMap[K, V]) Each(f func(key K, value V)) {
	m.Range(func(key K, value V) bool {
		f(key, value)
		return true
	})
}

func (m *SyncMap[K, V]) Keys() []K {
	m.lock.RLock()
	defer m.lock.RUnlock()
	keys := make([]K, 0, len(m.raw))
	for key := range m.raw {
		keys = append(keys, key)
	}
	return keys
}
func (m *SyncMap[K, V]) Values() []V {
	m.lock.RLock()
	defer m.lock.RUnlock()
	values := make([]V, 0, len(m.raw))
	for _, value := range m.raw {
		values = append(values, value)
	}
	return values
}

func (m *SyncMap[K, V]) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.raw = make(map[K]V)
}

// Range and Clear
func (m *SyncMap[K, V]) RAC(f func(key K, value V) bool) {
	m.Range(f)
	m.Clear()
}

// Each and Clear
func (m *SyncMap[K, V]) EAC(f func(key K, value V)) {
	m.RAC(func(key K, value V) bool {
		f(key, value)
		return true
	})
}

func (m *SyncMap[K, V]) Size() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.raw)
}
func (m *SyncMap[K, V]) Len() int {
	return m.Size()
}
