package redis

import (
	"fmt"
	"sync"

	"github.com/go-redis/redis"

	"github.com/urfave/cli"
)

type Client struct {
	host   string
	port   int
	value  redis.UniversalClient
	inited bool
	err    error
	mux    sync.Mutex
}

const (
	REDIS_HOST_FLAG = "redis-host"
	REDIS_PORT_FLAG = "redis-port"
)

func NewClient(c *cli.Context) *Client {
	return &Client{host: c.String(REDIS_HOST_FLAG), port: c.Int(REDIS_PORT_FLAG)}
}

func (s *Client) Close() {
	if s.value != nil {
		s.value.Close()
	}
}

func (s *Client) get() (redis.UniversalClient, error) {
	addrs := []string{fmt.Sprintf("%s:%d", s.host, s.port)}
	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    addrs,
		Password: "",
		DB:       0,
	})
	return client, nil
}

func (s *Client) Get() (redis.UniversalClient, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.inited {
		return s.value, s.err
	}
	s.value, s.err = s.get()
	s.inited = true
	return s.value, s.err
}

func RegisterRedisFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   REDIS_HOST_FLAG,
		Usage:  "redis host",
		Value:  "localhost",
		EnvVar: "REDIS_MASTER_SERVICE_HOST, REDIS_SERVICE_HOST",
	})
	c.Flags = append(c.Flags, cli.IntFlag{
		Name:   REDIS_PORT_FLAG,
		Usage:  "redis port",
		Value:  6379,
		EnvVar: "REDIS_MASTER_SERVICE_PORT, REDIS_SERVICE_PORT",
	})
}
