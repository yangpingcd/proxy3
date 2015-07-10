package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

const (
	VERSION = "0.1.0"
)

type AppSettings struct {
	chunkCacheSize       int64
	manifestCacheSize    int64
	goMaxProcs           int
	httpsCertFile        string
	httpsKeyFile         string
	httpsListenAddrs     string
	listenAddrs          string
	maxConnsPerIp        int
	maxIdleUpstreamConns int
	maxItemsCount        int
	readBufferSize       int
	statsRequestPath     string
	statsJsonRequestPath string
	//upstreamHost         string
	//upstreamProtocol   string
	useClientRequestHost bool
	writeBufferSize      int

	upstreams    Upstreams
	upstreamFile string
}

var Settings AppSettings = AppSettings{}

func usage() {
	fmt.Printf("Proxy3 is a live HLS cache server\n"+
		"Author: <sliq> ping@sliq.com\n"+
		"Current Version: %s\n\n", VERSION)

	flag.PrintDefaults()
	os.Exit(2)
}

func init() {
	flag.Usage = usage

	flag.Int64Var(&Settings.chunkCacheSize, "chunkCacheSize", 100, "The total chunk cache size in Mbytes")
	flag.Int64Var(&Settings.manifestCacheSize, "manifestCacheSize", 10, "The total manifest cache size in Mbytes")
	flag.IntVar(&Settings.goMaxProcs, "goMaxProcs", runtime.NumCPU(), "Maximum number of simultaneous Go threads")
	flag.StringVar(&Settings.httpsCertFile, "httpsCertFile", "/etc/ssl/certs/ssl-cert-snakeoil.pem", "Path to HTTPS server certificate. Used only if listenHttpsAddr is set")
	flag.StringVar(&Settings.httpsKeyFile, "httpsKeyFile", "/etc/ssl/private/ssl-cert-snakeoil.key", "Path to HTTPS server key. Used only if listenHttpsAddr is set")
	flag.StringVar(&Settings.httpsListenAddrs, "httpsListenAddrs", "", "A list of TCP addresses to listen to HTTPS requests. Leave empty if you don't need https")
	flag.StringVar(&Settings.listenAddrs, "listenAddrs", ":8080", "A list of TCP addresses to listen to HTTP requests. Leave empty if you don't need http")
	flag.IntVar(&Settings.maxConnsPerIp, "maxConnsPerIp", 32, "The maximum number of concurrent connections from a single ip")
	flag.IntVar(&Settings.maxIdleUpstreamConns, "maxIdleUpstreamConns", 50, "The maximum idle connections to upstream host")
	flag.IntVar(&Settings.maxItemsCount, "maxItemsCount", 100*1000, "The maximum number of items in the cache")
	flag.IntVar(&Settings.readBufferSize, "readBufferSize", 1024, "The size of read buffer for incoming connections")
	flag.StringVar(&Settings.statsRequestPath, "statsRequestPath", "/static_proxy_stats", "Path to page with statistics")
	flag.StringVar(&Settings.statsJsonRequestPath, "statsJsonRequestPath", "/static_proxy_statsjson", "Path to page with statistics")
	//flag.StringVar(&Settings.upstreamHost, "upstreamHost", "t-wowza:1935", "Upstream host to proxy data from. May include port in the form 'host:port'")
	//flag.String(&Settings.upstreamProtocol, "upstreamProtocol", "http", "Use this protocol when talking to the upstream")
	flag.BoolVar(&Settings.useClientRequestHost, "useClientRequestHost", false, "If set to true, then use 'Host' header from client requests in requests to upstream host. Otherwise use upstreamHost as a 'Host' header in upstream requests")
	flag.IntVar(&Settings.writeBufferSize, "writeBufferSize", 4096, "The size of write buffer for incoming connections")

	flag.Var(&Settings.upstreams, "upstream", "Path to read upstream clients")
	flag.StringVar(&Settings.upstreamFile, "upstreamFile", "upstream.ini", "Path to read upstream clients")
}
