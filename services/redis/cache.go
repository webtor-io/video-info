package redis

import (
	"bytes"
	"encoding/gob"
	"time"

	"github.com/oz/osdb"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
)

type Cache struct {
	key string
	cl  *cs.RedisClient
}

type HashAndSize struct {
	Hash uint64
	Size int64
}

func NewCache(key string, cl *cs.RedisClient) *Cache {
	return &Cache{key: key, cl: cl}
}

func (s *Cache) GetHashAndSize() (uint64, int64, error) {
	cl := s.cl.Get()
	// if err != nil {
	// 	return 0, errors.Wrap(err, "Failed to get redis client")
	// }
	data, err := cl.Get(s.key + "hashandsize").Bytes()
	if err == redis.Nil {
		return 0, 0, nil
	}
	res := HashAndSize{}
	err = s.decode(data, &res)
	if err != nil {
		return 0, 0, errors.Wrap(err, "Failed to decode data")
	}
	if err == redis.Nil {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, errors.Wrap(err, "Failed to get hash and size")
	}
	return res.Hash, res.Size, nil
}

func (s *Cache) SetHashAndSize(hash uint64, size int64) error {
	cl := s.cl.Get()
	// if err != nil {
	// 	return errors.Wrap(err, "Failed to get redis client")
	// }
	data, err := s.encode(HashAndSize{Hash: hash, Size: size})
	if err != nil {
		return errors.Wrap(err, "Failed to encode hash and size")
	}
	err = cl.Set(s.key+"hashandsize", data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "Failed to set hash")
	}
	return nil
}

func (s *Cache) GetSubtitles() (osdb.Subtitles, error) {
	cl := s.cl.Get()
	// if err != nil {
	// 	return nil, errors.Wrap(err, "Failed to get redis client")
	// }
	data, err := cl.Get(s.key + "subs").Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get subs")
	}
	res := osdb.Subtitles{}
	err = s.decode(data, &res)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to decode data")
	}
	return res, nil
}

func (s *Cache) SetSubtitles(subs osdb.Subtitles) error {
	cl := s.cl.Get()
	// if err != nil {
	// 	return errors.Wrap(err, "Failed to get redis client")
	// }
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

func (s *Cache) GetSubtitle(id string) ([]byte, error) {
	cl := s.cl.Get()
	// if err != nil {
	// 	return nil, errors.Wrap(err, "Failed to get redis client")
	// }
	data, err := cl.Get(s.key + "sub" + id).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get subtitle")
	}
	return data, nil
}

func (s *Cache) SetSubtitle(id string, data []byte) error {
	cl := s.cl.Get()
	// if err != nil {
	// 	return errors.Wrap(err, "Failed to get redis client")
	// }
	err := cl.Set(s.key+"sub"+id, data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "Failed to set subtitle")
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
