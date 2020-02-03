package redis

import (
	"sync"
)

type CachePool struct {
	sm sync.Map
	cl *Client
}

func NewCachePool(cl *Client) *CachePool {
	return &CachePool{cl: cl}
}

func (s *CachePool) Get(key string) *Cache {
	v, loaded := s.sm.LoadOrStore(key, NewCache(key, s.cl))
	if !loaded {
		defer s.sm.Delete(key)
	}
	return v.(*Cache)
}
