package redis

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/oz/osdb"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
)

type Cache struct {
	key string
	cl  *Client
}

func NewCache(key string, cl *Client) *Cache {
	return &Cache{key: key, cl: cl}
}

func (s *Cache) GetHash() (uint64, error) {
	cl, err := s.cl.Get()
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get redis client")
	}
	hash, err := cl.Get(s.key + "hash").Uint64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get hash")
	}
	return hash, nil
}

func (s *Cache) SetHash(hash uint64) error {
	cl, err := s.cl.Get()
	if err != nil {
		return errors.Wrap(err, "Failed to get redis client")
	}
	err = cl.Set(s.key+"hash", hash, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "Failed to set hash")
	}
	return nil
}

func (s *Cache) GetSubtitles() (osdb.Subtitles, error) {
	cl, err := s.cl.Get()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get redis client")
	}
	data, err := cl.Get(s.key + "subs").Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get hash")
	}
	res := osdb.Subtitles{}
	err = s.decode(data, &res)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to decode data")
	}
	return res, nil
}

func (s *Cache) SetSubtitles(subs osdb.Subtitles) error {
	cl, err := s.cl.Get()
	if err != nil {
		return errors.Wrap(err, "Failed to get redis client")
	}
	data, err := s.encode(subs)
	if err != nil {
		return errors.Wrap(err, "Failed to encode subs")
	}
	err = cl.Set(s.key+"subs", data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "Failed to set subs")
	}
	return nil
}

func (s *Cache) GetSubtitle(id int) ([]byte, error) {
	cl, err := s.cl.Get()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get redis client")
	}
	data, err := cl.Get(s.key + "sub" + fmt.Sprintf("%d", id)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get subtitle")
	}
	return data, nil
}

func (s *Cache) SetSubtitle(id int, data []byte) error {
	cl, err := s.cl.Get()
	if err != nil {
		return errors.Wrap(err, "Failed to get redis client")
	}
	err = cl.Set(s.key+"sub"+fmt.Sprintf("%d", id), data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "Failed to set hash")
	}
	return nil
}

func (s *Cache) encode(data interface{}) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(data)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Cache) decode(data []byte, to interface{}) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	return dec.Decode(to)
}
