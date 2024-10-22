package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"github.com/redis/go-redis/v9"
	"github.com/webtor-io/video-info/services/osdb"
	"strconv"
	"time"

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

func (s *Cache) GetHashAndSize(ctx context.Context) (uint64, int64, error) {
	cl := s.cl.Get()
	// if err != nil {
	// 	return 0, errors.Wrap(err, "Failed to get redis client")
	// }
	data, err := cl.Get(ctx, s.key+"hashandsize").Bytes()
	if errors.Is(err, redis.Nil) {
		return 0, 0, nil
	}
	res := HashAndSize{}
	err = s.decode(data, &res)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to decode data")
	}
	if errors.Is(err, redis.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to get hash and size")
	}
	return res.Hash, res.Size, nil
}

func (s *Cache) SetHashAndSize(ctx context.Context, hash uint64, size int64) error {
	cl := s.cl.Get()
	// if err != nil {
	// 	return errors.Wrap(err, "failed to get redis client")
	// }
	data, err := s.encode(HashAndSize{Hash: hash, Size: size})
	if err != nil {
		return errors.Wrap(err, "failed to encode hash and size")
	}
	err = cl.Set(ctx, s.key+"hashandsize", data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "Failed to set hash")
	}
	return nil
}

func (s *Cache) GetSubtitles(ctx context.Context) ([]osdb.Subtitle, error) {
	//return nil, nil
	cl := s.cl.Get()
	//if err != nil {
	//	return nil, errors.Wrap(err, "failed to get redis client")
	//}
	data, err := cl.Get(ctx, s.key+"subsrest").Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get subs")
	}
	var res []osdb.Subtitle
	err = s.decode(data, &res)
	if err != nil {
		return nil, nil
		//return nil, errors.Wrap(err, "failed to decode data")
	}
	return res, nil
}

func (s *Cache) SetSubtitles(ctx context.Context, subs []osdb.Subtitle) error {
	cl := s.cl.Get()
	// if err != nil {
	// 	return errors.Wrap(err, "Failed to get redis client")
	// }
	data, err := s.encode(subs)
	if err != nil {
		return errors.Wrap(err, "failed to encode subs")
	}
	err = cl.Set(ctx, s.key+"subsrest", data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "failed to set subs")
	}
	return nil
}

func (s *Cache) GetSubtitle(ctx context.Context, id int, format string) ([]byte, error) {
	cl := s.cl.Get()
	// if err != nil {
	// 	return nil, errors.Wrap(err, "failed to get redis client")
	// }
	data, err := cl.Get(ctx, s.key+"sub"+strconv.Itoa(id)+format).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get subtitle")
	}
	return data, nil
}

func (s *Cache) SetSubtitle(ctx context.Context, id int, format string, data []byte) error {
	cl := s.cl.Get()
	// if err != nil {
	// 	return errors.Wrap(err, "failed to get redis client")
	// }
	err := cl.Set(ctx, s.key+"sub"+strconv.Itoa(id)+format, data, time.Hour*24).Err()
	if err != nil {
		return errors.Wrap(err, "failed to set subtitle")
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
