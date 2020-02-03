package osdb

import (
	"bytes"
	"encoding/binary"
	"net"
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
	inited bool
	err    error
	mux    sync.Mutex
}

func NewHash(url string, c *redis.Cache) *Hash {
	return &Hash{url: url, cache: c, inited: false}
}

func (s *Hash) get(purge bool) (uint64, error) {
	if !purge {
		hash, err := s.cache.GetHash()
		if err != nil {
			return 0, errors.Wrap(err, "Failed to get hash from cache")
		}
		if hash != 0 {
			return hash, nil
		}
	}
	r := sh.New(s.url)
	myTransport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout: 5 * time.Minute,
		}).Dial,
	}
	r.Client = &http.Client{
		Timeout:   5 * time.Minute,
		Transport: myTransport,
	}
	hash, err := makeHash(r)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get hash")
	}
	err = s.cache.SetHash(hash)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to store hash in cache")
	}
	return hash, nil
}

func (s *Hash) Get(purge bool) (uint64, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if purge {
		s.inited = false
	}
	if s.inited {
		return s.hash, s.err
	}
	s.hash, s.err = s.get(purge)
	s.inited = true
	return s.hash, s.err
}

// https://trac.opensubtitles.org/projects/opensubtitles/wiki/HashSourceCodes#GO
const (
	ChunkSize = 65536 // 64k
)

func makeHash(r *sh.SeekingHTTP) (uint64, error) {
	var hash uint64 = 0
	size, err := r.Size()
	if err != nil {
		return 0, errors.Wrap(err, "Unable to read size")
	}
	if size < ChunkSize {
		return 0, errors.Errorf("File is too small %v", size)
	}

	// Read head and tail blocks.
	buf := make([]byte, ChunkSize*2)
	err = readChunk(r, 0, buf[:ChunkSize])
	if err != nil {
		return 0, errors.Wrap(err, "Failed to read head block")
	}
	err = readChunk(r, size-ChunkSize, buf[ChunkSize:])
	if err != nil {
		return 0, errors.Wrap(err, "Failed to read tail block")
	}

	// Convert to uint64, and sum.
	var nums [(ChunkSize * 2) / 8]uint64
	reader := bytes.NewReader(buf)
	err = binary.Read(reader, binary.LittleEndian, &nums)
	if err != nil {
		return 0, err
	}
	for _, num := range nums {
		hash += num
	}

	return hash + uint64(size), nil
}

// Read a chunk of a file at `offset` so as to fill `buf`.
func readChunk(r *sh.SeekingHTTP, offset int64, buf []byte) (err error) {
	n, err := r.ReadAt(buf, offset)
	if err != nil {
		return errors.Wrapf(err, "Failed to read chunk")
	}
	if n != ChunkSize {
		return errors.Errorf("Invalid read %v", n)
	}
	return
}
