package services

import (
	"context"

	"github.com/webtor-io/video-info/services/osdb"
)

// The search legs talk to their cache through these interfaces rather than
// through *redis.Cache directly, so a leg can be driven in a test without a
// redis server. *redis.Cache is the only production implementation.

// hashCache holds the OpenSubtitles moviehash and file size of one file.
type hashCache interface {
	GetHashAndSize(ctx context.Context) (uint64, int64, error)
	SetHashAndSize(ctx context.Context, hash uint64, size int64) error
}

// subtitleCache holds one search result. GetSubtitles reports separately
// whether the search has been made at all, because an empty result is an
// answer worth keeping: without that flag every file with no subtitles is
// searched upstream again on every page view.
type subtitleCache interface {
	GetSubtitles(ctx context.Context) ([]osdb.Subtitle, bool, error)
	SetSubtitles(ctx context.Context, subs []osdb.Subtitle) error
}

// searchCache is what the hash leg needs: it reads the moviehash and the list
// it produced from the same entry.
type searchCache interface {
	hashCache
	subtitleCache
}
