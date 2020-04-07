package osdb

import (
	"sync"

	"github.com/oz/osdb"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

type Client struct {
	user   string
	pass   string
	mux    sync.Mutex
	inited bool
	err    error
	value  *osdb.Client
}

const (
	OSDB_USER = "osdb-user"
	OSDB_PASS = "osdb-pass"
)

func RegisterOSDBCLientFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OSDB_USER,
		Usage:  "osdb user",
		Value:  "",
		EnvVar: "OSDB_USER",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OSDB_PASS,
		Usage:  "osdb pass",
		Value:  "",
		EnvVar: "OSDB_PASS",
	})
}

func NewClient(c *cli.Context) *Client {
	return &Client{user: c.String(OSDB_USER), pass: c.String(OSDB_PASS), inited: false}
}

func (s *Client) get() (*osdb.Client, error) {
	c, err := osdb.NewClient()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to init OSDB client")
	}

	if err = c.LogIn(s.user, s.pass, ""); err != nil {
		return nil, errors.Wrap(err, "Failed to auth to OSDB")
	}
	return c, nil
}

func (s *Client) Get() (*osdb.Client, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.err != nil {
		s.inited = false
	}
	if s.inited {
		return s.value, s.err
	}
	s.value, s.err = s.get()
	s.inited = true
	return s.value, s.err
}

func (s *Client) Close() {
	if s.value != nil {
		s.value.Close()
	}
}
