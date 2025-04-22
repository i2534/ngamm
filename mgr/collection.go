package mgr

import (
	"slices"
	"sync"
)

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

type LRUMap[K comparable, V any] struct {
	lock      *sync.RWMutex
	raw       map[K]V
	keys      []K
	capacity  int  // 添加容量限制
	unlimited bool // 标记是否无限容量
}

func NewLRUMap[K comparable, V any]() *LRUMap[K, V] {
	return &LRUMap[K, V]{
		lock:      &sync.RWMutex{},
		raw:       make(map[K]V),
		keys:      make([]K, 0),
		unlimited: true, // 默认无限容量
	}
}

// WithCapacity 设置LRUMap的容量限制
func (m *LRUMap[K, V]) WithCapacity(capacity int) *LRUMap[K, V] {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.capacity = capacity
	m.unlimited = capacity <= 0
	// 如果当前元素超出容量限制，则移除最老的元素
	m.enforceCapacity()
	return m
}

// 强制执行容量限制
func (m *LRUMap[K, V]) enforceCapacity() {
	if m.unlimited {
		return
	}
	for len(m.keys) > m.capacity {
		// 删除最老的元素（队列头部）
		oldestKey := m.keys[0]
		m.keys = m.keys[1:]
		delete(m.raw, oldestKey)
	}
}

func (m *LRUMap[K, V]) Has(key K) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, ok := m.raw[key]
	return ok
}
func (m *LRUMap[K, V]) Get(key K) (value V, ok bool) {
	m.lock.RLock()
	value, ok = m.raw[key]
	m.lock.RUnlock()
	if ok {
		m.lock.Lock()
		defer m.lock.Unlock()
		m.moveToTail(key)
	}
	return
}
func (m *LRUMap[K, V]) Put(key K, value V) {
	m.lock.Lock()
	defer m.lock.Unlock()
	if _, ok := m.raw[key]; ok {
		m.moveToTail(key)
	} else {
		m.keys = append(m.keys, key)
	}
	m.raw[key] = value
	m.enforceCapacity() // 添加容量限制检查
}
func (m *LRUMap[K, V]) moveToTail(key K) {
	for i, k := range m.keys {
		if k == key {
			m.keys = slices.Delete(m.keys, i, i+1)
			break
		}
	}
	m.keys = append(m.keys, key)
}
func (m *LRUMap[K, V]) Delete(key K) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.raw, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = slices.Delete(m.keys, i, i+1)
			break
		}
	}
}
func (m *LRUMap[K, V]) Range(f func(key K, value V) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for _, key := range m.Keys() {
		value := m.raw[key]
		if !f(key, value) {
			break
		}
	}
}
func (m *LRUMap[K, V]) Each(f func(key K, value V)) {
	m.Range(func(key K, value V) bool {
		f(key, value)
		return true
	})
}
func (m *LRUMap[K, V]) Keys() []K {
	m.lock.RLock()
	defer m.lock.RUnlock()
	keys := slices.Clone(m.keys)
	slices.Reverse(keys)
	return keys
}
func (m *LRUMap[K, V]) Values() []V {
	m.lock.RLock()
	defer m.lock.RUnlock()
	len := len(m.raw)
	values := make([]V, len)
	for i, key := range m.keys {
		values[len-i-1] = m.raw[key]
	}
	return values
}
func (m *LRUMap[K, V]) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.raw = make(map[K]V)
	m.keys = make([]K, 0)
}
func (m *LRUMap[K, V]) Size() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.raw)
}
func (m *LRUMap[K, V]) Len() int {
	return m.Size()
}
func (m *LRUMap[K, V]) RAC(f func(key K, value V) bool) {
	m.Range(f)
	m.Clear()
}
func (m *LRUMap[K, V]) EAC(f func(key K, value V)) {
	m.RAC(func(key K, value V) bool {
		f(key, value)
		return true
	})
}
