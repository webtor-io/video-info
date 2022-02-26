package osdb

import (
	"github.com/oz/osdb"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

type Client struct {
	user string
	pass string
	lang string
	ua   string
}

const (
	OSDB_USER = "osdb-user"
	OSDB_PASS = "osdb-pass"
	OSDB_UA   = "osdb-ua"
	OSDB_LANG = "osdb-lang"
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
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OSDB_UA,
		Usage:  "osdb user agent",
		Value:  "osdb-go 0.2",
		EnvVar: "OSDB_UA",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   OSDB_LANG,
		Usage:  "osdb language",
		Value:  "en",
		EnvVar: "OSDB_LANG",
	})
}

func NewClient(c *cli.Context) *Client {
	return &Client{
		user: c.String(OSDB_USER),
		pass: c.String(OSDB_PASS),
		ua:   c.String(OSDB_UA),
		lang: c.String(OSDB_LANG),
	}
}

func (s *Client) get() (*osdb.Client, error) {
	c, err := osdb.NewClient()
	c.UserAgent = s.ua
	if err != nil {
		return nil, errors.Wrap(err, "Failed to init OSDB client")
	}

	if err = c.LogIn(s.user, s.pass, s.lang); err != nil {
		return nil, errors.Wrapf(err, "Failed to auth to OSDB ua=%v", s.ua)
	}
	return c, nil
}

func (s *Client) Get() (*osdb.Client, error) {
	return s.get()
}
