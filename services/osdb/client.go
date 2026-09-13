package osdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Client struct {
	apiKey string
	apiURL string
	apiUA  string
	user   string
	pass   string
	cl     *http.Client
	token  string
	mux    sync.Mutex
	// limiter paces every outbound request: the API allows 5 req/s per IP
	// and a player page can fire dozens of track downloads at once.
	limiter *rate.Limiter
	// retryBackoff is the pause before retrying a 429 when the API sends no
	// reset hint; tests shrink it.
	retryBackoff time.Duration
}

const (
	OsdbRateFlag = "osdb-rate"
	// maxAttempts bounds the 429 retry loop per request.
	maxAttempts = 3
	maxBackoff  = 3 * time.Second
)

const (
	OsdbApiKeyFlag       = "osdb-api-key"
	OsdbApiUserAgentFlag = "osdb-api-user-agent"
	OsdbApiURLFlag       = "osdb-api-url"
	OsdbUser             = "osdb-user"
	OsdbPass             = "osdb-pass"
)

func RegisterOSDBClientFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   OsdbApiKeyFlag,
			Usage:  "osdb api key",
			Value:  "",
			EnvVar: "OSDB_API_KEY",
		},
		cli.StringFlag{
			Name:   OsdbApiUserAgentFlag,
			Usage:  "osdb api user agent",
			Value:  "",
			EnvVar: "OSDB_API_USER_AGENT",
		},
		cli.StringFlag{
			Name:   OsdbApiURLFlag,
			Usage:  "osdb api url",
			Value:  "https://api.opensubtitles.com/api/v1",
			EnvVar: "OSDB_API_URL",
		},
		cli.StringFlag{
			Name:   OsdbUser,
			Usage:  "osdb user",
			Value:  "",
			EnvVar: "OSDB_USER",
		},
		cli.StringFlag{
			Name:   OsdbPass,
			Usage:  "osdb pass",
			Value:  "",
			EnvVar: "OSDB_PASS",
		},
		cli.Float64Flag{
			Name:   OsdbRateFlag,
			Usage:  "max outbound OpenSubtitles requests per second for this process (API limit is 5/s per IP, shared by all replicas behind one egress IP)",
			Value:  2,
			EnvVar: "OSDB_RATE",
		},
	)
}

func NewClient(c *cli.Context, cl *http.Client) *Client {
	client := &Client{
		apiKey: c.String(OsdbApiKeyFlag),
		apiUA:  c.String(OsdbApiUserAgentFlag),
		apiURL: c.String(OsdbApiURLFlag),
		user:   c.String(OsdbUser),
		pass:   c.String(OsdbPass),
		cl:     cl,
	}
	client.SetRate(c.Float64(OsdbRateFlag))
	client.retryBackoff = time.Second
	return client
}

// SetRate installs the outbound limiter: r requests per second, burst 1, so
// a burst of track downloads is spread out instead of tripping the API's
// per-IP limit. r <= 0 disables pacing.
func (s *Client) SetRate(r float64) {
	if r <= 0 {
		s.limiter = nil
		return
	}
	s.limiter = rate.NewLimiter(rate.Limit(r), 1)
}

// do builds the request with mk on every attempt (bodies are consumed), paces
// it through the limiter and retries HTTP 429 up to maxAttempts times, waiting
// for the reset the API announces (ratelimit-reset / Retry-After, seconds) or
// retryBackoff when it announces none. The last 429 is returned to the caller
// as a normal response so its status check produces the error message.
func (s *Client) do(ctx context.Context, mk func() (*http.Request, error)) (*http.Response, error) {
	var res *http.Response
	for attempt := 1; ; attempt++ {
		if s.limiter != nil {
			if err := s.limiter.Wait(ctx); err != nil {
				return nil, errors.Wrap(err, "rate limiter wait")
			}
		}
		req, err := mk()
		if err != nil {
			return nil, err
		}
		res, err = s.cl.Do(req)
		if err != nil {
			return nil, errors.Wrap(err, "failed to do request")
		}
		if res.StatusCode != http.StatusTooManyRequests || attempt >= maxAttempts {
			return res, nil
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		delay := s.retryDelay(res.Header)
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "cancelled while waiting to retry after 429")
		case <-time.After(delay):
		}
	}
}

func (s *Client) retryDelay(h http.Header) time.Duration {
	for _, k := range []string{"ratelimit-reset", "Retry-After"} {
		if v := strings.TrimSpace(h.Get(k)); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				d := time.Duration(n) * time.Second
				if d > maxBackoff {
					d = maxBackoff
				}
				return d
			}
		}
	}
	if s.retryBackoff > 0 {
		return s.retryBackoff
	}
	return time.Second
}

func (s *Client) getToken(ctx context.Context) (token string, err error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.token != "" {
		return s.token, nil
	}
	u := fmt.Sprintf("%v/login", s.apiURL)
	lr := &LoginRequest{
		Username: s.user,
		Password: s.pass,
	}
	rb, err := json.Marshal(lr)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal object=%+v", lr)
	}
	res, err := s.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(rb))
		if err != nil {
			return nil, errors.Wrap(err, "failed to make new login request")
		}
		return s.prepareRequest(req), nil
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to do login request")
	}
	b := res.Body
	defer b.Close()
	d, err := io.ReadAll(b)
	if err != nil {
		return "", errors.Wrap(err, "failed to read login data")
	}
	if res.StatusCode != 200 {
		return "", errors.Errorf("got bad status code on download request code=%v with body=%v", res.StatusCode, string(d))
	}
	lre := LoginResponse{}
	err = json.Unmarshal(d, &lre)

	if err != nil {
		return "", errors.Wrapf(err, "failed to unmarshal data=%v", string(d))
	}
	go func() {
		<-time.After(time.Hour)
		s.mux.Lock()
		defer s.mux.Unlock()
		s.token = ""
	}()
	s.token = lre.Token
	return s.token, nil
}

func (s *Client) SearchSubtitles(ctx context.Context, u string) (subs []Subtitle, err error) {
	res, err := s.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make new request")
		}
		return s.prepareRequest(req), nil
	})
	if err != nil {
		return nil, err
	}
	b := res.Body
	defer b.Close()
	data, err := io.ReadAll(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read data")
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.Errorf("got bad status code on search request code=%v with body=%v", res.StatusCode, string(data))
	}
	sr := SubtitleSearchResponse{}
	err = json.Unmarshal(data, &sr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal data=%v", string(data))
	}
	subs = sr.Data
	return
}

// NormalizeImdbID strips the "tt" prefix and leading zeros, because the
// OpenSubtitles API wants the bare number. It does not validate: see
// ValidImdbID.
func NormalizeImdbID(id string) string {
	id = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "tt")
	return strings.TrimLeft(id, "0")
}

// ValidImdbID returns the normalized id and true only when it is non-empty and
// all ASCII digits, so it can be spliced into an upstream query and used as a
// cache/pool key without smuggling anything along.
func ValidImdbID(id string) (string, bool) {
	n := NormalizeImdbID(id)
	if n == "" {
		return "", false
	}
	for _, c := range n {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return n, true
}

func (s *Client) SearchSubtitlesByIMDB(ctx context.Context, id string) (subs []Subtitle, err error) {
	n, ok := ValidImdbID(id)
	if !ok {
		return nil, errors.Errorf("invalid imdb id %q", id)
	}
	q := url.Values{}
	q.Set("imdb_id", n)
	return s.SearchSubtitles(ctx, s.apiURL+"/subtitles?"+q.Encode())
}

// SearchSubtitlesByEpisode looks up one episode of a series by the
// series' IMDb id. Query keys are alphabetical so tests can match the
// exact URL.
func (s *Client) SearchSubtitlesByEpisode(ctx context.Context, parentImdbID string, season, episode int) (subs []Subtitle, err error) {
	n, ok := ValidImdbID(parentImdbID)
	if !ok {
		return nil, errors.Errorf("invalid imdb id %q", parentImdbID)
	}
	q := url.Values{}
	q.Set("episode_number", strconv.Itoa(episode))
	q.Set("parent_imdb_id", n)
	q.Set("season_number", strconv.Itoa(season))
	return s.SearchSubtitles(ctx, s.apiURL+"/subtitles?"+q.Encode())
}

// padMoviehash left-pads the hex moviehash to the 16 characters the API
// requires. (The previous loop re-read len(hash) on every iteration and so
// stopped one short whenever two or more leading nibbles were zero.)
func padMoviehash(hash string) string {
	if len(hash) >= 16 {
		return hash
	}
	return strings.Repeat("0", 16-len(hash)) + hash
}

func (s *Client) SearchSubtitlesByHash(ctx context.Context, hash string) (subs []Subtitle, err error) {
	q := url.Values{}
	q.Set("moviehash", padMoviehash(hash))
	return s.SearchSubtitles(ctx, s.apiURL+"/subtitles?"+q.Encode())
}

func (s *Client) prepareRequest(req *http.Request) *http.Request {
	req.Header.Add("Api-Key", s.apiKey)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "*/*")
	req.Header.Add("User-Agent", s.apiUA)
	return req
}

func (s *Client) DownloadSubtitle(ctx context.Context, id int, format string) (d []byte, err error) {
	u := fmt.Sprintf("%v/download", s.apiURL)
	sdr := &SubtitleDownloadRequest{
		FileID:    id,
		SubFormat: format,
	}
	rb, err := json.Marshal(sdr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal object=%+v", sdr)
	}
	res, err := s.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(rb))
		if err != nil {
			return nil, errors.Wrap(err, "failed to make new download request")
		}
		req = s.prepareRequest(req)
		req, err = s.addToken(ctx, req)
		if err != nil {
			return nil, errors.Wrap(err, "failed to add token to request")
		}
		return req, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to do download request")
	}
	b := res.Body
	defer b.Close()
	dd, err := io.ReadAll(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read download data")
	}
	if res.StatusCode != 200 {
		return nil, errors.Errorf("got bad status code on download request code=%v with body=%v", res.StatusCode, string(dd))
	}
	dresp := SubtitleDownloadResponse{}
	err = json.Unmarshal(dd, &dresp)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal download response data=%v", string(dd))
	}
	dlink := dresp.Link

	lresp, err := s.do(ctx, func() (*http.Request, error) {
		lreq, err := http.NewRequestWithContext(ctx, "GET", dlink, nil)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make new link request")
		}
		lreq.Header.Set("User-Agent", s.apiUA)
		return lreq, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to do link request")
	}
	lb := lresp.Body
	defer lb.Close()
	d, err = io.ReadAll(lb)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read link data")
	}
	if lresp.StatusCode != 200 {
		return nil, errors.Errorf("got bad status code on link request code=%v with body=%v", lresp.StatusCode, string(d))
	}
	return
}

func (s *Client) addToken(ctx context.Context, req *http.Request) (*http.Request, error) {
	token, err := s.getToken(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get token")
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %v", token))
	return req, nil
}
