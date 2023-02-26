package services

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"sync"
	"time"

	"github.com/webtor-io/video-info/services/redis"

	sh "github.com/jeffallen/seekinghttp"
	"github.com/pkg/errors"
)

type Hash struct {
	url    string
	cache  *redis.Cache
	hash   uint64
	size   int64
	inited bool
	err    error
	mux    sync.Mutex
}

func NewHash(url string, c *redis.Cache) *Hash {
	return &Hash{url: url, cache: c, inited: false}
}

func (s *Hash) get(purge bool) (uint64, int64, error) {
	if !purge {
		hash, size, err := s.cache.GetHashAndSize()
		if err != nil {
			return 0, 0, errors.Wrap(err, "failed to get hash and size from cache")
		}
		if hash != 0 && size != 0 {
			return hash, size, nil
		}
	}
	r := sh.New(s.url)
	//myTransport := &http.Transport{
	//	Dial: (&net.Dialer{
	//		Timeout: 5 * time.Minute,
	//	}).Dial,
	//}
	r.Client = &http.Client{
		Timeout: 5 * time.Minute,
		//Transport: myTransport,
	}
	hash, size, err := makeHash(r)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to get hash")
	}
	err = s.cache.SetHashAndSize(hash, size)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to store hash in cache")
	}
	return hash, size, nil
}

func (s *Hash) Get(purge bool) (uint64, int64, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if purge {
		s.inited = false
	}
	if s.inited {
		return s.hash, s.size, s.err
	}
	s.hash, s.size, s.err = s.get(purge)
	s.inited = true
	return s.hash, s.size, s.err
}

// https://trac.opensubtitles.org/projects/opensubtitles/wiki/HashSourceCodes#GO
const (
	ChunkSize = 65536 // 64k
)

func makeHash(r *sh.SeekingHTTP) (uint64, int64, error) {
	var hash uint64 = 0
	size, err := r.Size()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to read size")
	}
	if size < ChunkSize {
		return 0, 0, errors.Errorf("file is too small %v", size)
	}

	// Read head and tail blocks.
	buf := make([]byte, ChunkSize*2)
	err = readChunk(r, 0, buf[:ChunkSize])
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to read head block")
	}
	err = readChunk(r, size-ChunkSize, buf[ChunkSize:])
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to read tail block")
	}

	// Convert to uint64, and sum.
	var nums [(ChunkSize * 2) / 8]uint64
	reader := bytes.NewReader(buf)
	err = binary.Read(reader, binary.LittleEndian, &nums)
	if err != nil {
		return 0, 0, err
	}
	for _, num := range nums {
		hash += num
	}

	return hash + uint64(size), size, nil
}

// Read a chunk of a file at `offset` so as to fill `buf`.
func readChunk(r *sh.SeekingHTTP, offset int64, buf []byte) (err error) {
	n, err := r.ReadAt(buf, offset)
	if err != nil {
		return errors.Wrapf(err, "failed to read chunk")
	}
	if n != ChunkSize {
		return errors.Errorf("invalid read %v", n)
	}
	return
}
