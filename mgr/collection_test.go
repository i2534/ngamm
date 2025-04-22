package mgr_test

import (
	"fmt"
	"testing"

	"github.com/go-playground/assert/v2"
	"github.com/i2534/ngamm/mgr"
)

func TestLRUMap_BasicOperations(t *testing.T) {
	lru := mgr.NewLRUMap[string, int]()

	// 测试Put和Get操作
	lru.Put("one", 1)
	lru.Put("two", 2)
	lru.Put("three", 3)

	val, ok := lru.Get("one")
	assert.Equal(t, true, ok)
	assert.Equal(t, 1, val)

	val, ok = lru.Get("two")
	assert.Equal(t, true, ok)
	assert.Equal(t, 2, val)

	val, ok = lru.Get("three")
	assert.Equal(t, true, ok)
	assert.Equal(t, 3, val)

	// 测试Has操作
	assert.Equal(t, true, lru.Has("one"))
	assert.Equal(t, true, lru.Has("two"))
	assert.Equal(t, true, lru.Has("three"))
	assert.Equal(t, false, lru.Has("four"))

	// 测试Size/Len操作
	assert.Equal(t, 3, lru.Size())
	assert.Equal(t, 3, lru.Len())

	// 测试Delete操作
	lru.Delete("two")
	assert.Equal(t, false, lru.Has("two"))
	assert.Equal(t, 2, lru.Size())

	// 测试Clear操作
	lru.Clear()
	assert.Equal(t, 0, lru.Size())
	assert.Equal(t, false, lru.Has("one"))
	assert.Equal(t, false, lru.Has("three"))
}

func TestLRUMap_LRUBehavior(t *testing.T) {
	// 创建容量为3的LRU缓存
	lru := mgr.NewLRUMap[string, int]().WithCapacity(3)

	// 添加3个元素填满缓存
	lru.Put("one", 1)
	lru.Put("two", 2)
	lru.Put("three", 3)

	assert.Equal(t, []string{"three", "two", "one"}, lru.Keys())

	lru.Get("two")
	assert.Equal(t, []string{"two", "three", "one"}, lru.Keys())

	// 再添加一个新元素，应该淘汰最老的元素"one"
	lru.Put("four", 4)

	// "two"应该被淘汰
	assert.Equal(t, false, lru.Has("one"))
	assert.Equal(t, true, lru.Has("two"))
	assert.Equal(t, true, lru.Has("three"))
	assert.Equal(t, true, lru.Has("four"))

	// 验证键的顺序
	assert.Equal(t, []string{"four", "two", "three"}, lru.Keys())
	assert.Equal(t, 3, lru.Size())
}

func TestLRUMap_UnlimitedCapacity(t *testing.T) {
	// 默认无限容量
	lru := mgr.NewLRUMap[string, int]()

	// 添加多个元素
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		lru.Put(key, i)
	}

	// 验证所有元素都被保存
	assert.Equal(t, 100, lru.Size())

	// 验证可以访问所有元素
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		val, ok := lru.Get(key)
		assert.Equal(t, true, ok)
		assert.Equal(t, i, val)
	}
}

func TestLRUMap_UpdateExistingKey(t *testing.T) {
	lru := mgr.NewLRUMap[string, int]()

	lru.Put("key", 1)
	assert.Equal(t, 1, lru.Size())

	val, _ := lru.Get("key")
	assert.Equal(t, 1, val)

	// 更新现有的键
	lru.Put("key", 100)
	assert.Equal(t, 1, lru.Size())

	val, _ = lru.Get("key")
	assert.Equal(t, 100, val)
}

func TestLRUMap_RangeAndEach(t *testing.T) {
	lru := mgr.NewLRUMap[string, int]()

	lru.Put("one", 1)
	lru.Put("two", 2)
	lru.Put("three", 3)

	// 测试Range
	sum := 0
	lru.Range(func(key string, value int) bool {
		sum += value
		return true
	})
	assert.Equal(t, 6, sum)

	// 测试提前终止Range
	sum = 0
	lru.Range(func(key string, value int) bool {
		sum += value
		return value != 2 // 在value=2时停止
	})
	assert.Equal(t, 5, sum) // 3+2=5

	// 测试Each
	sum = 0
	lru.Each(func(key string, value int) {
		sum += value
	})
	assert.Equal(t, 6, sum)
}

func TestLRUMap_Values(t *testing.T) {
	lru := mgr.NewLRUMap[string, int]()

	lru.Put("one", 1)
	lru.Put("two", 2)
	lru.Put("three", 3)

	keys := lru.Keys()
	assert.Equal(t, []string{"three", "two", "one"}, keys)

	values := lru.Values()
	assert.Equal(t, []int{3, 2, 1}, values)
}

func TestLRUMap_RAC_EAC(t *testing.T) {
	lru := mgr.NewLRUMap[string, int]()

	lru.Put("one", 1)
	lru.Put("two", 2)
	lru.Put("three", 3)

	// 测试RAC
	sum := 0
	lru.RAC(func(key string, value int) bool {
		sum += value
		return true
	})
	assert.Equal(t, 6, sum)
	assert.Equal(t, 0, lru.Size()) // 确认已清空

	// 重新填充
	lru.Put("one", 1)
	lru.Put("two", 2)
	lru.Put("three", 3)

	// 测试EAC
	sum = 0
	lru.EAC(func(key string, value int) {
		sum += value
	})
	assert.Equal(t, 6, sum)
	assert.Equal(t, 0, lru.Size()) // 确认已清空
}
