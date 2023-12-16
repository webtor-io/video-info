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
	"sync"
	"time"
)

type Client struct {
	apiKey string
	apiURL string
	user   string
	pass   string
	cl     *http.Client
	token  string
	mux    sync.Mutex
}

const (
	OsdbApiKeyFlag = "osdb-api-key"
	OsdbApiURLFlag = "osdb-api-url"
	OsdbUser       = "osdb-user"
	OsdbPass       = "osdb-pass"
)

func RegisterOSDBClientFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OsdbApiKeyFlag,
		Usage:  "osdb api key",
		Value:  "",
		EnvVar: "OSDB_API_KEY",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OsdbApiURLFlag,
		Usage:  "osdb api url",
		Value:  "https://api.opensubtitles.com/api/v1",
		EnvVar: "OSDB_API_URL",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OsdbUser,
		Usage:  "osdb user",
		Value:  "",
		EnvVar: "OSDB_USER",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OsdbPass,
		Usage:  "osdb pass",
		Value:  "",
		EnvVar: "OSDB_PASS",
	})
}

func NewClient(c *cli.Context, cl *http.Client) *Client {
	return &Client{
		apiKey: c.String(OsdbApiKeyFlag),
		apiURL: c.String(OsdbApiURLFlag),
		user:   c.String(OsdbUser),
		pass:   c.String(OsdbPass),
		cl:     cl,
	}
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
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(rb))
	if err != nil {
		return "", errors.Wrap(err, "failed to make new login request")
	}
	req = s.prepareRequest(req)
	//rd, _ := httputil.DumpRequest(req, true)
	//log.Info(string(rd))
	res, err := s.cl.Do(req)
	//red, _ := httputil.DumpResponse(res, true)
	//log.Info(string(red))
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
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new request")
	}
	req = s.prepareRequest(req)
	res, err := s.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to do request")
	}
	b := res.Body
	defer b.Close()
	data, err := io.ReadAll(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read data")
	}
	sr := SubtitleSearchResponse{}
	err = json.Unmarshal(data, &sr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal data=%v", string(data))
	}
	subs = sr.Data
	return
}

func (s *Client) SearchSubtitlesByIMDB(ctx context.Context, id string) (subs []Subtitle, err error) {
	u := fmt.Sprintf("%v/subtitles?imdb_id=%v", s.apiURL, id)
	return s.SearchSubtitles(ctx, u)
}

func (s *Client) SearchSubtitlesByHash(ctx context.Context, hash string) (subs []Subtitle, err error) {
	for i := 0; i < 16-len(hash); i++ {
		hash = "0" + hash
	}
	u := fmt.Sprintf("%v/subtitles?moviehash=%v", s.apiURL, hash)
	return s.SearchSubtitles(ctx, u)
}

func (s *Client) prepareRequest(req *http.Request) *http.Request {
	req.Header.Add("Api-Key", s.apiKey)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "*/*")
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
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(rb))
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new download request")
	}
	req = s.prepareRequest(req)
	req, err = s.addToken(ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to add token to request")
	}
	//rd, _ := httputil.DumpRequest(req, true)
	//log.Info(string(rd))
	res, err := s.cl.Do(req)
	//red, _ := httputil.DumpResponse(res, true)
	//log.Info(string(red))
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

	lreq, err := http.NewRequestWithContext(ctx, "GET", dlink, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new link request")
	}
	lresp, err := s.cl.Do(lreq)
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
