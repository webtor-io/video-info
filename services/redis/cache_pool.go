package redis

import (
	"sync"

	cs "github.com/webtor-io/common-services"
)

type CachePool struct {
	sm sync.Map
	cl *cs.RedisClient
}

func NewCachePool(cl *cs.RedisClient) *CachePool {
	return &CachePool{cl: cl}
}

func (s *CachePool) Get(key string) *Cache {
	v, loaded := s.sm.LoadOrStore(key, NewCache(key, s.cl))
	if !loaded {
		defer s.sm.Delete(key)
	}
	return v.(*Cache)
}
