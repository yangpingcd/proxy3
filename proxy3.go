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
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"encoding/json"

	"os"
	//"os/signal"
	_ "net/http/pprof"

	"github.com/vharitonsky/iniflags"
	//"github.com/natefinch/lumberjack"
	//"gopkg.in/natefinch/lumberjack.v2"	
	"github.com/kardianos/service"
	"github.com/yangpingcd/mem"
	"github.com/yangpingcd/lumberjack"
)



type CodeHeader struct {
	code 	int
	header	[]byte
}

var (
	ifNoneMatchResponseHeader         = CodeHeader{304, []byte("HTTP/1.1 304 Not Modified\r\nServer: proxy3\r\nEtag: W/\"CacheForever\"\r\n\r\n")}
	internalServerErrorResponseHeader = CodeHeader{500, []byte("HTTP/1.1 500 Internal Server Error\r\nServer: proxy3\r\n\r\n")}
	notAllowedResponseHeader          = CodeHeader{405, []byte("HTTP/1.1 405 Method Not Allowed\r\nServer: proxy3\r\n\r\n")}
	//okResponseHeader                  = []byte("HTTP/1.1 200 OK\r\nServer: proxy2\r\nCache-Control: public, max-age=31536000\r\nETag: W/\"CacheForever\"\r\n")
	okResponseHeader                 = CodeHeader{200, []byte("HTTP/1.1 200 OK\r\nServer: proxy3\r\nCache-Control: no-cache\r\n")}
	serviceUnavailableResponseHeader = CodeHeader{503, []byte("HTTP/1.1 503 Service Unavailable\r\nServer: proxy3\r\n\r\n")}
	statsJsonResponseHeader          = CodeHeader{200, []byte("HTTP/1.1 200 OK\r\nServer: proxy3\r\nContent-Type: application/json\r\n\r\n")}
	statsResponseHeader              = CodeHeader{200, []byte("HTTP/1.1 200 OK\r\nServer: proxy3\r\nContent-Type: text/plain\r\n\r\n")}
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

	for _, upstream := range Settings.Upstreams {
		client := NewUpstreamClient(upstream.Name, upstream.UpstreamHost)
		upstreamClients = append(upstreamClients, client)
	}

	/*if Settings.upstreamFile == "" {
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
	}*/
}



var svcFlag = flag.String("service", "", "Control the system service.")
var accessLog AccessLog

func main() {
	//flag.Parse()
	iniflags.Parse()
	
	
	svcConfig := &service.Config {
		Name:        "proxy3",
		DisplayName: "Sliq Proxy3 Service",
		Description: "Proxy cache server for Live HLS streams",
		Arguments:   []string {},
	}
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-service=") {
			svcConfig.Arguments = append(svcConfig.Arguments, arg)
		}
	}
	/*fmt.Println(svcConfig.Arguments)
	return*/
	
	// intiailze the generic log file
	if Settings.LogSetting.Filename != "" {
		log.SetOutput(&lumberjack.Logger{
			Filename: 	Settings.LogSetting.Filename,
			MaxSize:    Settings.LogSetting.MaxSize,
			MaxBackups: Settings.LogSetting.MaxBackups,
			MaxAge:     Settings.LogSetting.MaxAge,
			LocalTime:  Settings.LogSetting.LocalTime,
		})
	}
	
	// initialize the access log file
	if true {
		accessLog = NewAccessLog(Settings.AccessLogSetting)	
	}
		
	initUpstreamClients()
	for _, client := range upstreamClients {
		logMessage("upstreamClient \"%s\": \"%s\"", client.name, client.upstreamHost)
	}

	runtime.GOMAXPROCS(Settings.GoMaxProcs)

	//c := make(chan os.Signal, 1)
	//signal.Notify(c, os.Interrupt)
	//<-c
	//logMessage("ctrl-c is captured")
	
	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}
	errs := make(chan error, 5)
	logger, err = s.Logger(errs)
	if err != nil {
		log.Fatal(err)
	}
	
	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			log.Printf("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		return
	}
	
	go func() {
		for {
			err := <-errs
			if err != nil {
				log.Print(err)
			}
		}
	}()

	
	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			log.Printf("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		return
	}
	err = s.Run()
	if err != nil {
		logger.Error(err)
	}
	//Start(nil)
}

func Start(stop chan struct{}) error {
	quitAccessLog := make(chan struct{})
	go accessLog.StartAccessLog(quitAccessLog)
	
	
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
			return nil
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
	timeStartHandle := time.Now()
	/*defer func() {
		timeEndHandle := time.Now()
		elapse := timeEndRequest.Sub(timeStartHandle)
		logMessage(fmt.Sprintf("spent %v to handleRequest %v", elapse, req.RequestURI))
	}()*/
	
	var logItem LogItem = LogItem{}
	logItem.date = time.Now()
	//logItem.cs_uri_stem = req.req.RequestURI
	logItem.cs_uri_stem = req.URL.Path
	logItem.cs_uri_query = req.URL.RawQuery
	logItem.cs_method = req.Method
	if pos := strings.Index(req.RemoteAddr, ":"); pos>=0 {
		logItem.s_ip = req.RemoteAddr[:pos]
		port, err := strconv.Atoi(req.RemoteAddr[pos+1:])
		if err == nil {
			logItem.s_port = port
		}
	} else {
		logItem.s_ip = req.RemoteAddr
		//logItem.s_port = 80
		/*if pos := strings.Index(req.URL.Host, ":"); pos >=0 {
			port, err := strconv.Atoi(req.URL.Host[pos+1:])
			if err == nil {
				logItem.s_port = port
			}
		}*/
	}
	
	//req.Header.
	
	
	logItem.cs_User_Agent = req.UserAgent()
	logItem.cs_Referer = req.Referer()
	logItem.sc_status = 200
	logItem.sc_substatus = 0
	
	defer func(startTime time.Time, logItem *LogItem) {
		elapse := time.Now().Sub(startTime)		
		logItem.time_taken = int(elapse.Nanoseconds() / 1000000)
		accessLog.AddItem(*logItem)
	}(timeStartHandle, &logItem)
	

	atomic.AddInt64(&stats.RequestsCount, 1)

	logMessage("request %s from %s", req.RequestURI, req.RemoteAddr)

	if req.Method != "GET" {
		w.Write(notAllowedResponseHeader.header)
		logItem.sc_status = notAllowedResponseHeader.code
		return false
	}

	if req.RequestURI == "/" {
		w.Write(okResponseHeader.header)
		w.Write([]byte("proxy3 server\r\n"))
		logItem.sc_status = okResponseHeader.code
		return false
	}
	if req.RequestURI == "/favicon.ico" {
		w.Write(serviceUnavailableResponseHeader.header)
		logItem.sc_status = serviceUnavailableResponseHeader.code
		return false
	}

	if req.RequestURI == Settings.statsRequestPath {
		w.Write(statsResponseHeader.header)
		stats.WriteToStream(w)
		logItem.sc_status = statsResponseHeader.code
		return false
	}

	if req.RequestURI == Settings.statsJsonRequestPath {
		w.Write(statsJsonResponseHeader.header)
		stats.WriteJsonToStream(w)
		logItem.sc_status = statsJsonResponseHeader.code
		return false
	}

	if req.Header.Get("If-None-Match") != "" {
		_, err := w.Write(ifNoneMatchResponseHeader.header)
		atomic.AddInt64(&stats.IfNoneMatchHitsCount, 1)
		logItem.sc_status = ifNoneMatchResponseHeader.code
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
	w.Write(serviceUnavailableResponseHeader.header)
	logItem.sc_status = serviceUnavailableResponseHeader.code
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
	
	items := GetStats()
	for _, item := range items {
		fmt.Fprintf(w, "%s PlaylistCache stats (playlist.m3u8)\n", item.Name)
		item.PlaylistStats.WriteToStream(w)
		fmt.Fprintf(w, "%s ManifestCache stats (chunklist*.m3u8)\n", item.Name)
		item.ManifestStats.WriteToStream(w)
		fmt.Fprintf(w, "%s ChunkCache stats (*.ts)\n", item.Name)
		item.ChunkStats.WriteToStream(w)
	}	
}

type CacheStatsJson struct {
	
	CacheCap int64
	Duration int64
	CacheObjects int
	
	CacheUsed int64
	CacheMisses int64
	CacheHits int64
}

func (stats CacheStatsJson) WriteToStream(w io.Writer) {
	fmt.Fprintf(w, "  Capacity: %d MB\n", stats.CacheCap)
	fmt.Fprintf(w, "  Duration: %d seconds\n", stats.Duration)

	fmt.Fprintf(w, "  Objects: %d\n", stats.CacheObjects)

	fmt.Fprintf(w, "  Used: %s\n", UsageToString(stats.CacheUsed))
	fmt.Fprintf(w, "  Misses: %d\n", stats.CacheMisses)
	fmt.Fprintf(w, "  Hits: %d\n", stats.CacheHits)
}

func cacheStatsToJson(cache CacheManager, json *CacheStatsJson) {

	json.CacheCap = atomic.LoadInt64(&cache.cacheCap)
	json.Duration = atomic.LoadInt64(&cache.cacheDuration)

	cache.tasksMutex.Lock()
	json.CacheObjects = len(cache.tasks)
	cache.tasksMutex.Unlock()

	json.CacheUsed = atomic.LoadInt64(&cache.stat.cacheUsed)
	json.CacheMisses = atomic.LoadInt64(&cache.stat.cacheMisses)
	json.CacheHits = atomic.LoadInt64(&cache.stat.cacheHits)
}

type ClientStatsJson struct {
	Name string
		
	PlaylistStats 	CacheStatsJson
	ManifestStats	CacheStatsJson
	ChunkStats		CacheStatsJson	
}

func GetStats() []ClientStatsJson {
	items := make([]ClientStatsJson, 0)
	
	for _, client := range upstreamClients {
		item := ClientStatsJson {
			Name: client.name,
			
			PlaylistStats: CacheStatsJson{},
			ManifestStats: CacheStatsJson{},
			ChunkStats: CacheStatsJson{},
		}
		
		cacheStatsToJson(client.playlistCache, &item.PlaylistStats)
		cacheStatsToJson(client.manifestCache, &item.ManifestStats)
		cacheStatsToJson(client.chunkCache, &item.ChunkStats)
		
		items = append(items, item)
	}
	
	return items
}

func (s *Stats) WriteJsonToStream(w io.Writer) {	
	
	stats := struct {
		Settings		AppSettings
		Stats			Stats
		ClientStats		[]ClientStatsJson
	} {
		Settings: 		Settings,
		Stats:			*s,
		ClientStats:	GetStats(),
	} 
		
	//buf, _ := json.Marshal(stats)
	buf, _ := json.MarshalIndent(stats, "", "    ")
	
	w.Write(buf)
}
