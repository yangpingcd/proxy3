package main

import (
	"errors"
	"fmt"
	"strings"
)

type Upstreams []Upstream
type Upstream struct {
	Name         string
	UpstreamHost string
}

func (u *Upstreams) String() string {
	return fmt.Sprint(*u)
}

func (u *Upstreams) Set(value string) error {
	v := strings.SplitN(value, "=", 2)
	if len(v) != 2 {
		return errors.New("Expected `name=host`")
	}

	upstream := Upstream{
		strings.TrimSpace(v[0]),
		strings.TrimSpace(v[1]),
	}

	*u = append(*u, upstream)
	return nil
}
