package main

import (
	//"flag"
	"fmt"
	"testing"
)

var (
	upstreams    Upstreams
)

func init() {
	//flag.Var(&clients, "client", "client")
}

func TestUpstreams(*testing.T) {
	//flag.Parse()

	/*clients := UpstreamParamSlice {
		{"server1", ":1990"},
		{"server2", ":1991"},
	}*/

	fmt.Println("upstreams=", upstreams)
}
