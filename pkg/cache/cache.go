package cache

import (
	"sync"
)

type ResourceCache interface {
	Set(key string, obj interface{})
	Delete(key string)
	Get(key string) (interface{}, bool)
	ListKeys() []string
}

type mapCache struct {
	cache map[string]interface{}
	mu    *sync.Mutex
}

func NewCache() ResourceCache {
	return &mapCache{
		cache: map[string]interface{}{},
		mu:    &sync.Mutex{},
	}
}

func (c *mapCache) ListKeys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := []string{}
	for k, _ := range c.cache {
		keys = append(keys, k)
	}
	return keys
}

func (c *mapCache) Set(key string, obj interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = obj
}

func (c *mapCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, key)
}

func (c *mapCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.cache[key]; true {
		return v, ok
	}
	return nil, false
}
