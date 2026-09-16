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
	// store is resolved per call, not at construction: the shared client
	// connects lazily. A test supplies its own.
	store func() store
}

// store is the slice of the redis client this cache uses. It is narrow so a
// test can implement it — the shared client's own interface has hundreds of
// methods, which is why the TTL of a stored entry used to be unobservable.
type store interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
}

type HashAndSize struct {
	Hash uint64
	Size int64
}

// subtitlesEntry is how a search result is stored. The marker is the whole
// point of the wrapper: gob decodes an empty slice back as nil, so "this file
// has no subtitles" was indistinguishable from "never searched", and 53% of
// listings are empty — every one of them went to the API again on every page
// view. The marker makes an empty answer an answer.
type subtitlesEntry struct {
	Found     bool
	Subtitles []osdb.Subtitle
}

const (
	subtitlesTTL = 24 * time.Hour
	// emptySubtitlesTTL is shorter: a torrent published minutes ago usually has
	// no subtitles yet, and a day-long "nothing here" would hide the ones
	// uploaded right after it. (24h is the upstream cap for caching results.)
	emptySubtitlesTTL = 6 * time.Hour
)

func NewCache(key string, cl *cs.RedisClient) *Cache {
	return &Cache{key: key, store: func() store { return cl.Get() }}
}

// Key is the prefix every entry of this cache lives under. Callers that key
// their own in-process maps by the same thing read it from here.
func (s *Cache) Key() string {
	return s.key
}

func (s *Cache) GetHashAndSize(ctx context.Context) (uint64, int64, error) {
	cl := s.store()
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
	cl := s.store()
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

// GetSubtitles returns the cached list and whether this file has been searched
// at all. An empty list with found=true is an answer, not a miss.
func (s *Cache) GetSubtitles(ctx context.Context) ([]osdb.Subtitle, bool, error) {
	cl := s.store()
	data, err := cl.Get(ctx, s.key+"subsrest").Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to get subs")
	}
	subs, found := s.decodeSubtitles(data)
	return subs, found, nil
}

// encodeSubtitles and decodeSubtitles are the stored form of a search result.
// They are a pair on purpose: the Found marker only works if both ends agree,
// and this is the boundary where the emptiness used to be lost.
func (s *Cache) encodeSubtitles(subs []osdb.Subtitle) ([]byte, error) {
	return s.encode(subtitlesEntry{Found: true, Subtitles: subs})
}

func (s *Cache) decodeSubtitles(data []byte) ([]osdb.Subtitle, bool) {
	var res subtitlesEntry
	if err := s.decode(data, &res); err != nil {
		// Either a corrupt entry or one written before the marker existed (a
		// bare slice, which fails as a type mismatch). Both are miss-and-rewrite
		// rather than an error: the next search overwrites the key.
		return nil, false
	}
	return res.Subtitles, res.Found
}

func (s *Cache) SetSubtitles(ctx context.Context, subs []osdb.Subtitle) error {
	cl := s.store()
	data, err := s.encodeSubtitles(subs)
	if err != nil {
		return errors.Wrap(err, "failed to encode subs")
	}
	ttl := subtitlesTTL
	if len(subs) == 0 {
		ttl = emptySubtitlesTTL
	}
	err = cl.Set(ctx, s.key+"subsrest", data, ttl).Err()
	if err != nil {
		return errors.Wrap(err, "failed to set subs")
	}
	return nil
}

func (s *Cache) GetSubtitle(ctx context.Context, id int, format string) ([]byte, error) {
	cl := s.store()
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
	cl := s.store()
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
