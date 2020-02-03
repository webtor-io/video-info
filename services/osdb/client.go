package osdb

import (
	"sync"

	"github.com/oz/osdb"
	"github.com/pkg/errors"
)

type Client struct {
	mux    sync.Mutex
	inited bool
	err    error
	value  *osdb.Client
}

func NewClient() *Client {
	return &Client{inited: false}
}

func (s *Client) get() (*osdb.Client, error) {
	c, err := osdb.NewClient()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to init OSDB client")
	}

	if err = c.LogIn("", "", ""); err != nil {
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
