package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type UpstreamClient struct {
	name         string
	upstreamHost string

	playlistCache CacheManager
	chunkCache    CacheManager
	manifestCache CacheManager
}

func NewUpstreamClient(name string, upstreamHost string) UpstreamClient {
	client := UpstreamClient{}

	client.name = name
	client.upstreamHost = upstreamHost

	prefix := "/" + name

	var err error
	cacheSize := int64(10)
	if cacheSize <= 0 {
		// todo, auto
		cacheSize = 10
	}
	client.playlistCache, err = CreateCacheManager(prefix, upstreamHost, cacheSize, 3*60) // 3 minutes
	if err != nil {
		logFatal("Failed to create the playlist cache")
	}
	//defer playlistCache.Close()

	cacheSize = Settings.ChunkCacheSize
	if cacheSize <= 0 {
		// todo, auto
		cacheSize = 100
	}
	client.chunkCache, err = CreateCacheManager(prefix, upstreamHost, cacheSize, 30) // 30 seconds
	if err != nil {
		logFatal("Failed to create the chunk cache")
	}
	//defer chunkCache.Close()

	cacheSize = Settings.ManifestCacheSize
	if cacheSize <= 0 {
		// todo, auto
		cacheSize = 10
	}
	client.manifestCache, err = CreateCacheManager(prefix, upstreamHost, cacheSize, 2) // 2 seconds
	if err != nil {
		logFatal("Failed to create the manifest cache")
	}
	//defer manifestCache.Close()

	return client
}

var chunkRegExp, _ = regexp.Compile(`(media_).*_(\d*)(\.ts)`)

func (client *UpstreamClient) Handle(req *http.Request, w io.Writer) bool {
	key := KeyType(0)
	err := error(nil)
	uri := req.RequestURI

	// by default use the chunkCache
	cache := client.chunkCache

	if chunkRegExp.MatchString(uri) {
		newuri := chunkRegExp.ReplaceAllString(uri, "$1$2$3")
		logMessage("%s to %s", uri, newuri)
		key = CalcKey(append([]byte(getRequestHost(req, client.upstreamHost)), []byte(newuri)...))
	} else if strings.HasSuffix(uri, ".m3u8") {
		newuri := uri

		manifestRegExp, _ := regexp.Compile(`chunklist.*\.m3u8`)
		if manifestRegExp.MatchString(uri) {
			newuri = manifestRegExp.ReplaceAllString(uri, "chunklist.m3u8")
			cache = client.manifestCache
		} else {
			// we treat playlist.m3u8 as chunk cache because it will not be changed
			cache = client.playlistCache
		}

		key = CalcKey(append([]byte(getRequestHost(req, client.upstreamHost)), []byte(newuri)...))
	}

	//task, exist := cache.AddTask(key, req)
	task, _ := cache.AddTask(key, req)

	var item CacheItem

	select {
	case item = <-task.chItem:
		break
	case <-time.After(30 * time.Second):
		// timeout
		logMessage("timeout on request %s", uri)
		break
	}

	/*item := fetchIt(req, cache, key)
	if item == nil {
		w.Write(serviceUnavailableResponseHeader)
		return false
	}*/
	// it is in the cache, but error
	if item.contentBody == nil {
		w.Write(serviceUnavailableResponseHeader.header)
		return false
	}

	/*if item.contentType == nil {
		w.Write(internalServerErrorResponseHeader)
		return false
	}*/

	if _, err = w.Write(okResponseHeader.header); err != nil {
		return false
	}
	//if _, err = fmt.Fprintf(w, "Content-Type: %s\nContent-Length: %d\n\n", contentType, item.Available()); err != nil {
	if _, err = fmt.Fprintf(w, "Content-Type: %s\r\nContent-Length: %d\r\n\r\n", item.contentType, item.Size()); err != nil {
		return false
	}
	var bytesSent int64
	if bytesSent, err = item.WriteTo(w); err != nil {
		logRequestError(req, "Cannot send file with key=[%v] to client: %s", key, err)
		return false
	}
	atomic.AddInt64(&stats.BytesSentToClients, bytesSent)
	return true
}
