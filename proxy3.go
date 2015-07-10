package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	//"io/ioutil"
	"log"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	//"regexp"
	"encoding/json"

	"os"
	//"os/signal"
	_ "net/http/pprof"

	"github.com/vharitonsky/iniflags"
	"github.com/yangpingcd/mem"
)

var (
	ifNoneMatchResponseHeader         = []byte("HTTP/1.1 304 Not Modified\r\nServer: proxy3\r\nEtag: W/\"CacheForever\"\r\n\r\n")
	internalServerErrorResponseHeader = []byte("HTTP/1.1 500 Internal Server Error\r\nServer: proxy3\r\n\r\n")
	notAllowedResponseHeader          = []byte("HTTP/1.1 405 Method Not Allowed\r\nServer: proxy3\r\n\r\n")
	//okResponseHeader                  = []byte("HTTP/1.1 200 OK\r\nServer: proxy2\r\nCache-Control: public, max-age=31536000\r\nETag: W/\"CacheForever\"\r\n")
	okResponseHeader                 = []byte("HTTP/1.1 200 OK\r\nServer: proxy3\r\nCache-Control: no-cache\r\n")
	serviceUnavailableResponseHeader = []byte("HTTP/1.1 503 Service Unavailable\r\nServer: proxy3\r\n\r\n")
	statsJsonResponseHeader          = []byte("HTTP/1.1 200 OK\r\nServer: proxy3\r\nContent-Type: application/json\r\n\r\n")
	statsResponseHeader              = []byte("HTTP/1.1 200 OK\r\nServer: proxy3\r\nContent-Type: text/plain\r\n\r\n")
)

var (
	perIpConnTracker = createPerIpConnTracker()
	stats            Stats
	upstreamClient   http.Client

	upstreamClients []UpstreamClient = make([]UpstreamClient, 0)
)

func initUpstreamClients() {
	/*defer func() {
		//
		client := NewUpstreamClient("", Settings.upstreamHost)
		upstreamClients = append(upstreamClients, client)
	}()*/

	for _, upstream := range Settings.upstreams {
		client := NewUpstreamClient(upstream.name, upstream.upstreamHost)
		upstreamClients = append(upstreamClients, client)
	}

	if Settings.upstreamFile == "" {
		return
	}

	f, err := os.Open(Settings.upstreamFile)
	if err != nil {
		logMessage("Failed to open the upstreamFile \"%s\"", Settings.upstreamFile)
		//os.Exit(-1)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.Trim(line, " ")
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			// ignore the comment which starts with #;
			continue
		}

		if pos := strings.Index(line, "="); pos >= 0 {
			serviceName := strings.Trim(line[0:pos], "")
			upstreamHost := strings.Trim(line[pos+1:], "")

			client := NewUpstreamClient(serviceName, upstreamHost)
			upstreamClients = append(upstreamClients, client)
		}
	}
}

func main() {
	iniflags.Parse()
	initUpstreamClients()

	for _, client := range upstreamClients {
		logMessage("upstreamClient \"%s\": \"%s\"", client.name, client.upstreamHost)
	}

	runtime.GOMAXPROCS(Settings.goMaxProcs)

	//c := make(chan os.Signal, 1)
	//signal.Notify(c, os.Interrupt)
	//<-c
	//logMessage("ctrl-c is captured")

	Start(nil)
}

func Start(stop chan int) {
	var addr string
	for _, addr = range strings.Split(Settings.httpsListenAddrs, ",") {
		go serveHttps(addr)
	}
	for _, addr = range strings.Split(Settings.listenAddrs, ",") {
		go serveHttp(addr)
	}

	go manageCache()

	// performance
	/*go func() {
		//http.Server.ListenAndServe()
		http.ListenAndServe("localhost:9000", nil)
	}()*/

	for {
		select {
		case <-stop:
			return
		case <-time.After(1 * time.Second):
		}
	}
}

func getAvailableMemoryPercent() float64 {
	var avail = mem.GetAvailable()
	var total = mem.GetTotal()

	if total > 0 {
		avail100 := float64(avail) * 100

		return avail100 / float64(total)
	}

	return 0
}

type KeyDuration struct {
	key      KeyType
	duration time.Duration
}
type ByDuration []KeyDuration

func (kd ByDuration) Len() int {
	return len(kd)
}
func (kd ByDuration) Swap(i, j int) {
	kd[i], kd[j] = kd[j], kd[i]
}
func (kd ByDuration) Less(i, j int) bool {
	return kd[i].duration < kd[j].duration
}

func removeOldChunks() {
	now := time.Now()

	sortKeys := []KeyDuration{}
	removing := []KeyType{}

	// method1
	/*if true {
		chunkCache.cacheMapMutex.Lock()
		for key, item := range(chunkCache.cacheMap) {
			cacheDuration := now.Sub(item.cacheTime)
			if cacheDuration.Seconds() > 3 {
				removing = append(removing, key)
			}
		}
		chunkCache.cacheMapMutex.Unlock()
	}*/

	// method2
	if true {
		for _, client := range upstreamClients {
			chunkCache := client.chunkCache

			chunkCache.tasksMutex.Lock()
			for key, task := range client.chunkCache.tasks {
				//cacheDuration := now.Sub(task.stat.cacheTime)
				cacheTime := atomic.LoadInt64(&task.stat.cacheTime)
				cacheDuration := now.Sub(time.Unix(cacheTime/1e9, cacheTime%1e9))

				sortKeys = append(sortKeys, KeyDuration{key: key, duration: cacheDuration})
			}
			chunkCache.tasksMutex.Unlock()

			// sort descending
			sort.Sort(sort.Reverse(ByDuration(sortKeys)))

			// remove 1/10 of the cache
			for i := 0; i < len(sortKeys)/10; i++ {
				removing = append(removing, sortKeys[i].key)
			}

			count := chunkCache.RemoveTasks(removing)
			if count > 0 {
				logMessage(fmt.Sprintf("delete %d cache items because of low memory", count))
			}
		}
	}
}

// remove the expired items from the cache
func manageCache() {
	//go purgeCacheLockKey()

	lastCheckTime := time.Now()

	for {
		now := time.Now()
		if now.Sub(lastCheckTime) < time.Second*2 {
			var percent = getAvailableMemoryPercent()
			// free os memory check it again
			if percent < 10.0 {
				runtime.GC()
				debug.FreeOSMemory()
				percent = getAvailableMemoryPercent()
			}
			if percent < 10.0 {
				removeOldChunks()
				runtime.GC()
				debug.FreeOSMemory()
			}

			time.Sleep(time.Second * 1)
			//logMessage("%0.2f%% memory available", percent)
			continue
		}

		managers := make([]CacheManager, 0)
		for _, client := range upstreamClients {
			managers = append(managers, client.playlistCache)
			managers = append(managers, client.chunkCache)
			managers = append(managers, client.manifestCache)
		}

		count := 0
		//for _, cache := range([...]CacheManager {playlistCache, chunkCache, manifestCache}) {
		for _, cache := range managers {

			removing := []KeyType{}

			cache.tasksMutex.Lock()
			for key, task := range cache.tasks {

				//task.statLock.Lock()
				//cacheDuration := now.Sub(task.stat.cacheTime)
				//task.statLock.Unlock()

				cacheTime := atomic.LoadInt64(&task.stat.cacheTime)
				cacheDuration := now.Sub(time.Unix(cacheTime/1e9, cacheTime%1e9))

				if cacheDuration.Seconds() > float64(cache.cacheDuration) || cacheDuration < 0 {
					removing = append(removing, key)
				}
			}
			cache.tasksMutex.Unlock()

			count += cache.RemoveTasks(removing)
		}

		if count > 0 {
			logMessage(fmt.Sprintf("delete %d cache items", count))
		}

		lastCheckTime = time.Now()
	}
}

func serveHttps(addr string) {
	if addr == "" {
		return
	}
	cert, err := tls.LoadX509KeyPair(Settings.httpsCertFile, Settings.httpsKeyFile)
	if err != nil {
		logFatal("Cannot load certificate: [%s]", err)
	}
	c := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	ln := tls.NewListener(listen(addr), c)
	logMessage("Listening https on [%s]", addr)
	serve(ln)
}

func serveHttp(addr string) {
	if addr == "" {
		return
	}
	ln := listen(addr)
	logMessage("Listening http on [%s]", addr)
	serve(ln)
}

func listen(addr string) net.Listener {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		logFatal("Cannot listen [%s]: [%s]", addr, err)
	}
	return ln
}

func serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				logMessage("Cannot accept connections due to temporary network error: [%s]", err)
				time.Sleep(time.Second)
				continue
			}
			logFatal("Cannot accept connections due to permanent error: [%s]", err)
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	/*timeStartConnection := time.Now()
	defer func() {
		timeEndConnection := time.Now()
		elapse := timeEndConnection.Sub(timeStartConnection)
		logMessage(fmt.Sprintf("Spent %v to handleConnection", elapse))
	}()*/

	defer conn.Close()

	clientAddr := conn.RemoteAddr().(*net.TCPAddr).IP.To4()
	ipUint32 := ip4ToUint32(clientAddr)
	/*if perIpConnTracker.registerIp(ipUint32) > *maxConnsPerIp {
		logMessage("Too many concurrent connections (more than %d) from ip=%s. Denying new connection from the ip", *maxConnsPerIp, clientAddr)
		perIpConnTracker.unregisterIp(ipUint32)
		return
	}*/
	defer perIpConnTracker.unregisterIp(ipUint32)

	r := bufio.NewReaderSize(conn, Settings.readBufferSize)
	w := bufio.NewWriterSize(conn, Settings.writeBufferSize)
	clientAddrStr := clientAddr.String()
	for {
		req, err := http.ReadRequest(r)
		if err != nil {
			if err != io.EOF {
				logMessage("Error when reading http request from ip=%s: [%s]", clientAddr, err)
			}
			return
		}
		req.RemoteAddr = clientAddrStr
		ok := handleRequest(req, w)
		w.Flush()
		if !ok || !req.ProtoAtLeast(1, 1) || req.Header.Get("Connection") == "close" {
			return
		}
	}
}

//var chunkRegExp, _ = regexp.Compile(`(media_).*_(\d*)(\.ts)`)

func handleRequest(req *http.Request, w io.Writer) bool {
	/*timeStartRequest := time.Now()
	defer func() {
		timeEndRequest := time.Now()
		elapse := timeEndRequest.Sub(timeStartRequest)
		logMessage(fmt.Sprintf("spent %v to handleRequest %v", elapse, req.RequestURI))
	}()*/

	atomic.AddInt64(&stats.RequestsCount, 1)

	logMessage("request %s from %s", req.RequestURI, req.RemoteAddr)

	if req.Method != "GET" {
		w.Write(notAllowedResponseHeader)
		return false
	}

	if req.RequestURI == "/" {
		w.Write(okResponseHeader)
		w.Write([]byte("proxy3 server\r\n"))
		return false
	}
	if req.RequestURI == "/favicon.ico" {
		w.Write(serviceUnavailableResponseHeader)
		return false
	}

	if req.RequestURI == Settings.statsRequestPath {
		w.Write(statsResponseHeader)
		stats.WriteToStream(w)
		return false
	}

	if req.RequestURI == Settings.statsJsonRequestPath {
		w.Write(statsJsonResponseHeader)
		stats.WriteJsonToStream(w)
		return false
	}

	if req.Header.Get("If-None-Match") != "" {
		_, err := w.Write(ifNoneMatchResponseHeader)
		atomic.AddInt64(&stats.IfNoneMatchHitsCount, 1)
		return err == nil
	}

	for _, client := range upstreamClients {
		match := "/" + client.name + "/"
		if strings.HasPrefix(req.RequestURI, match) {
			return client.Handle(req, w)
		}
	}

	// find the default client
	for _, client := range upstreamClients {
		if client.name == "" {
			return client.Handle(req, w)
		}
	}

	// no clients to handle this
	//if true {
	w.Write(serviceUnavailableResponseHeader)
	return false
	//}

	//key := append([]byte(getRequestHost(req)), []byte(req.RequestURI)...)
	//key := CalcHash(append([]byte(getRequestHost(req)), []byte(req.RequestURI)...))
	//item, err := cache.GetDeItem(key, time.Second)

	/*key := KeyType(0)
	err := error(nil)
	uri := req.RequestURI

	// by default use the chunkCache
	cache := chunkCache

	if chunkRegExp.MatchString(uri) {
		newuri := chunkRegExp.ReplaceAllString(uri, "$1$2$3")
		logMessage("%s to %s", uri, newuri)
		key = CalcKey(append([]byte(getRequestHost(req)), []byte(newuri)...))
	} else if (strings.HasSuffix(uri, ".m3u8")) {
		newuri := uri

		manifestRegExp, _ := regexp.Compile(`chunklist.*\.m3u8`)
		if manifestRegExp.MatchString(uri) {
			newuri = manifestRegExp.ReplaceAllString(uri, "chunklist.m3u8")
			cache = manifestCache
		} else {
			// we treat playlist.m3u8 as chunk cache because it will not be changed
			cache = playlistCache
		}

		key = CalcKey(append([]byte(getRequestHost(req)), []byte(newuri)...))
	}

	//task, exist := cache.AddTask(key, req)
	task, _ := cache.AddTask(key, req)

	var item CacheItem

	select {
	case item = <-task.chItem:
		break
	case <-time.After(30*time.Second):
		// timeout
		logMessage("timeout on request %s", uri)
		break
	}


	//item := fetchIt(req, cache, key)
	//if item == nil {
	//	w.Write(serviceUnavailableResponseHeader)
	//	return false
	//}
	// it is in the cache, but error
	if item.contentBody == nil {
		w.Write(serviceUnavailableResponseHeader)
		return false
	}

	//if item.contentType == nil {
	//	w.Write(internalServerErrorResponseHeader)
	//	return false
	//}

	if _, err = w.Write(okResponseHeader); err != nil {
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
	return true*/
}

/*func fetchIt(req *http.Request, cache *Cache, key KeyType) *CacheItem {
	cache.LockKey(key)
	defer cache.UnlockKey(key)

	item, err := cache.GetDeItem(key, time.Second)

	if err != nil {
		//if err != ybc.ErrCacheMiss {
		if err != ErrCacheMiss {
			logFatal("Unexpected error when obtaining cache value by key=[%v]: [%s]", key, err)
		}

		if key == KeyZero {
			atomic.AddInt64(&stats.UncacheableCount, 1)
		}else {
			atomic.AddInt64(&stats.CacheMissesCount, 1)
		}
		item = fetchFromUpstream(req, key)

		if item == nil {
			// we make an empty item to indicate there is an error
			item = &CacheItem{}
		}

		if item != nil {
			item.requestHost = getRequestHost(req)
			item.requestUrl = req.RequestURI
			item.cacheTime = time.Now()

			if key != KeyZero {
				// add it to cache
				cache.AddItem(key, item)

				logMessage("add item to cache for %s", req.RequestURI)
			}
		}

	} else {
		atomic.AddInt64(&stats.CacheHitsCount, 1)
	}
	//defer item.Close()

	// todo: handle the same key
	return item
}*/

//func fetchFromUpstream(req *http.Request, key []byte) *ybc.Item {
/*func fetchFromUpstream(req *http.Request, key KeyType) *CacheItem {
	//atomic.AddInt64(&stats.BytesReadFromUpstream, int64(bodyLen))
	logMessage("fetchFromUpstream: %s://%s%s", *upstreamProtocol, *upstreamHost, req.RequestURI)

	upstreamUrl := fmt.Sprintf("%s://%s%s", *upstreamProtocol, *upstreamHost, req.RequestURI)
	upstreamReq, err := http.NewRequest("GET", upstreamUrl, nil)
	if err != nil {
		logRequestError(req, "Cannot create request structure for [%v]: [%s]", key, err)
		return nil
	}
	upstreamReq.Host = getRequestHost(req)

	upstreamClient := http.Client {
		Transport: &http.Transport{
			MaxIdleConnsPerHost: *maxIdleUpstreamConns,
		},
	}
	resp, err := upstreamClient.Do(upstreamReq)
	if err != nil {
		logRequestError(req, "Cannot make request for [%v]: [%s]", key, err)
		return nil
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logRequestError(req, "Cannot read response for [%v]: [%s]", key, err)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		logRequestError(req, "Unexpected status code=%d for the response [%v]", resp.StatusCode, key)
		return nil
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	//item := Item {contentType, body, MaxTtl}
	item := CacheItem {contentType: contentType, contentBody: body, ttl: MaxTtl}
	bodyLen := len(body)


	atomic.AddInt64(&stats.BytesReadFromUpstream, int64(bodyLen))
	return &item
}*/

//func loadContentType(req *http.Request, r io.Reader) (contentType string, err error) {
func loadContentType(req *http.Request, item *CacheItem) (contentType string, err error) {
	/*var sizeBuf [1]byte
	if _, err = r.Read(sizeBuf[:]); err != nil {
		logRequestError(req, "Cannot read content-type length from cache: [%s]", err)
		return
	}
	strSize := int(sizeBuf[0])
	strBuf := make([]byte, strSize)
	if _, err = r.Read(strBuf); err != nil {
		logRequestError(req, "Cannot read content-type string with length=%d from cache: [%s]", strSize, err)
		return
	}
	contentType = string(strBuf)
	return*/

	contentType = item.contentType
	return
}

func getRequestHost(req *http.Request, upstreamHost string) string {
	if Settings.useClientRequestHost {
		return req.Host
	}
	//return *upstreamHost
	return upstreamHost
}

func logRequestError(req *http.Request, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logMessage("%s - %s - %s - %s. %s", req.RemoteAddr, req.RequestURI, req.Referer(), req.UserAgent(), msg)
}

func logMessage(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s\n", msg)
}

func logFatal(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Fatalf("%s\n", msg)
}

func ip4ToUint32(ip4 net.IP) uint32 {
	return (uint32(ip4[0]) << 24) | (uint32(ip4[1]) << 16) | (uint32(ip4[2]) << 8) | uint32(ip4[3])
}

type PerIpConnTracker struct {
	mutex          sync.Mutex
	perIpConnCount map[uint32]int
}

func (ct *PerIpConnTracker) registerIp(ipUint32 uint32) int {
	ct.mutex.Lock()
	ct.perIpConnCount[ipUint32] += 1
	connCount := ct.perIpConnCount[ipUint32]
	ct.mutex.Unlock()
	return connCount
}

func (ct *PerIpConnTracker) unregisterIp(ipUint32 uint32) {
	ct.mutex.Lock()
	ct.perIpConnCount[ipUint32] -= 1
	ct.mutex.Unlock()
}

func createPerIpConnTracker() *PerIpConnTracker {
	return &PerIpConnTracker{
		perIpConnCount: make(map[uint32]int),
	}
}

type Stats struct {
	RequestsCount int64
	//UncacheableCount	  int64
	//CacheHitsCount        int64
	//CacheMissesCount      int64
	IfNoneMatchHitsCount  int64
	BytesReadFromUpstream int64
	BytesSentToClients    int64
}

func UsageToString(size int64) string {
	if size >= 1024*1024 {
		return fmt.Sprintf("%1.1f MB", float32(size)/1024/1024)
	} else if size >= 1024 {
		return fmt.Sprintf("%1.1f KB", float32(size)/1024)
	} else {
		return fmt.Sprintf("%d B", size)
	}
}

func cacheStatsToStream(cache CacheManager, w io.Writer) {

	cacheCap := atomic.LoadInt64(&cache.cacheCap)
	duration := atomic.LoadInt64(&cache.cacheDuration)

	cache.tasksMutex.Lock()
	cacheObjects := len(cache.tasks)
	cache.tasksMutex.Unlock()

	cacheUsed := atomic.LoadInt64(&cache.stat.cacheUsed)
	cacheMisses := atomic.LoadInt64(&cache.stat.cacheMisses)
	cacheHits := atomic.LoadInt64(&cache.stat.cacheHits)

	fmt.Fprintf(w, "  capacity: %d MB\n", cacheCap)
	fmt.Fprintf(w, "  duration: %d seconds\n", duration)

	fmt.Fprintf(w, "  objects: %d\n", cacheObjects)

	fmt.Fprintf(w, "  used: %s\n", UsageToString(cacheUsed))
	fmt.Fprintf(w, "  misses: %d\n", cacheMisses)
	fmt.Fprintf(w, "  hits: %d\n", cacheHits)
}

func (s *Stats) WriteToStream(w io.Writer) {
	fmt.Fprintf(w, "Command-line flags\n")
	flag.VisitAll(func(f *flag.Flag) {
		fmt.Fprintf(w, "%s=%v\n", f.Name, f.Value)
	})
	fmt.Fprintf(w, "\n")

	//requestsCount := s.CacheHitsCount + s.CacheMissesCount + s.IfNoneMatchHitsCount
	//requestsCount := s.UncacheableCount + s.CacheHitsCount + s.CacheMissesCount + s.IfNoneMatchHitsCount
	requestsCount := s.RequestsCount

	var cacheHitRatio float64
	if requestsCount > 0 {
		//cacheHitRatio = float64(s.CacheHitsCount+s.IfNoneMatchHitsCount) / float64(requestsCount) * 100.0
	}

	fmt.Fprintf(w, "Requests count: %d\n", requestsCount)
	fmt.Fprintf(w, "Cache hit ratio: %.3f%%\n", cacheHitRatio)
	/*fmt.Fprintf(w, "Uncacheable: %d\n", s.UncacheableCount)
	fmt.Fprintf(w, "Cache hits: %d\n", s.CacheHitsCount)
	fmt.Fprintf(w, "Cache misses: %d\n", s.CacheMissesCount)
	fmt.Fprintf(w, "If-None-Match hits: %d\n", s.IfNoneMatchHitsCount)
	fmt.Fprintf(w, "Read from upstream: %s\n", UsageToString(s.BytesReadFromUpstream))*/
	fmt.Fprintf(w, "Sent to clients: %s\n", UsageToString(s.BytesSentToClients))
	fmt.Fprintf(w, "Upstream traffic saved: %s\n", UsageToString(s.BytesSentToClients-s.BytesReadFromUpstream))
	//fmt.Fprintf(w, "Upstream requests saved: %d\n", s.CacheHitsCount+s.IfNoneMatchHitsCount)

	//
	fmt.Fprintf(w, "\n")

	for _, client := range upstreamClients {

		fmt.Fprintf(w, "%s PlaylistCache stats (playlist.m3u8)\n", client.name)
		cacheStatsToStream(client.playlistCache, w)
		fmt.Fprintf(w, "%s ManifestCache stats (chunklist*.m3u8)\n", client.name)
		cacheStatsToStream(client.manifestCache, w)
		fmt.Fprintf(w, "%s ChunkCache stats (*.ts)\n", client.name)
		cacheStatsToStream(client.chunkCache, w)
	}
}

func (s *Stats) WriteJsonToStream(w io.Writer) {
	/*s := struct {

			}*/
	buf, _ := json.Marshal(stats)
	w.Write(buf)
}
