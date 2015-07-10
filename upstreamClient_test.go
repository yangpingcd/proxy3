package main

import (
	"flag"
	"fmt"
	"testing"
)

var (
	clients UpstreamParamSlice
)

func init() {
	flag.Var(&clients, "client", "client")
}

func TestUpstreamParamSlice(*testing.T) {
	//flag.Parse()

	/*clients := UpstreamParamSlice {
		{"server1", ":1990"},
		{"server2", ":1991"},
	}*/

	fmt.Println(clients)
}
