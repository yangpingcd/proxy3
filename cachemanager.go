package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	//"strconv"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

var (
	// Maximum time to live for cached items.
	//
	// Use this value when adding items, which must live in the cache as long
	// as possible.
	MaxTtl = time.Hour * 24 * 365 * 100
)

type KeyType string

const KeyZero = KeyType("")

func CalcKey(buf []byte) KeyType {
	if buf == nil {
		return KeyZero
	}

	return KeyType(string(buf[:]))
}

type CacheTaskStat struct {
	// change cacheTime to UInt64 for atomic
	//cacheTime 	time.Time
	cacheTime int64
	cacheSize int64
}

type CacheTask struct {
	chItem chan CacheItem
	chQuit chan bool

	statMutex *sync.Mutex
	stat      *CacheTaskStat
}

type CacheManagerStat struct {
	cacheUsed int64

	cacheHits   int64
	cacheMisses int64
}

type CacheManager struct {
	// cacheCap: maximum memory in MB
	// cacheDuration: how long it keep in cache in seconds
	prefix        string
	upstreamHost  string
	cacheCap      int64
	cacheDuration int64

	tasksMutex *sync.Mutex
	tasks      map[KeyType]CacheTask

	statMutex *sync.Mutex
	stat      *CacheManagerStat
}

// cacheCap: maximum memory in MB
// cacheDuration: how long it keep in cache in seconds
func CreateCacheManager(prefix string, upstreamHost string, cacheCap int64, cacheDuration int64) (CacheManager, error) {
	manager := CacheManager{
		prefix:        prefix,
		upstreamHost:  upstreamHost,
		cacheCap:      cacheCap,
		cacheDuration: cacheDuration,

		tasksMutex: &sync.Mutex{},
		tasks:      make(map[KeyType]CacheTask),

		statMutex: &sync.Mutex{},
		stat:      &CacheManagerStat{},
	}

	return manager, nil
}

func (manager CacheManager) Close() {
	manager.RemoveAllTasks()
}

func (manager CacheManager) AddTask(key KeyType, req *http.Request) (CacheTask, bool) {
	manager.tasksMutex.Lock()
	defer manager.tasksMutex.Unlock()

	cacheTask, ok := manager.tasks[key]
	if !ok {
		atomic.AddInt64(&manager.stat.cacheMisses, 1)

		cacheTask = CacheTask{
			chItem: make(chan CacheItem),
			chQuit: make(chan bool),

			statMutex: &sync.Mutex{},
			stat:      &CacheTaskStat{},
		}

		//atomic.cacheTask.stat.cacheTime
		cacheTime := time.Now().UnixNano()
		atomic.StoreInt64(&cacheTask.stat.cacheTime, cacheTime)

		manager.tasks[key] = cacheTask

		go manager.fetchItem(key, req, cacheTask)
	} else {
		atomic.AddInt64(&manager.stat.cacheHits, 1)
	}

	return cacheTask, ok
}

func (manager CacheManager) RemoveTask(key KeyType) int {
	return manager.RemoveTasks([]KeyType{key})

	/*manager.tasksMutex.Lock()
	defer manager.tasksMutex.Unlock()

	cacheTask, ok := manager.tasks[key]
	if ok {
		cacheSize := atomic.LoadInt64(&cacheTask.stat.cacheSize)
		atomic.AddInt64(&manager.stat.cacheUsed, -cacheSize)

		delete(manager.tasks, key)

		cacheTask.chQuit <- true
	}
	return ok*/
}

// return how many item(s) have been removed
func (manager CacheManager) RemoveTasks(keys []KeyType) int {
	manager.tasksMutex.Lock()
	defer manager.tasksMutex.Unlock()

	count := 0
	for _, key := range keys {
		cacheTask, ok := manager.tasks[key]
		if ok {
			cacheSize := atomic.LoadInt64(&cacheTask.stat.cacheSize)
			atomic.AddInt64(&manager.stat.cacheUsed, -cacheSize)

			delete(manager.tasks, key)
			count++

			cacheTask.chQuit <- true
		}
	}

	return count
}

func (manager CacheManager) RemoveAllTasks() int {
	manager.tasksMutex.Lock()
	defer manager.tasksMutex.Unlock()

	count := 0
	for key, cacheTask := range manager.tasks {
		cacheSize := atomic.LoadInt64(&cacheTask.stat.cacheSize)
		atomic.AddInt64(&manager.stat.cacheUsed, -cacheSize)

		delete(manager.tasks, key)
		count++

		cacheTask.chQuit <- true
	}

	return count
}

func (manager CacheManager) fetchItem(key KeyType, req *http.Request, cacheTask CacheTask) {
	// simulate the fetch process
	//time.Sleep(300 * time.Microsecond)

	// fake result
	//result := CacheItem{}
	//result.contentType = string(key)

	var result CacheItem
	temp := manager.fetchFromUpstream(req, key)
	if temp != nil {
		result = *temp
	}

	//cacheTask.statLock.Lock()
	cacheSize := len(result.contentBody)
	atomic.StoreInt64(&cacheTask.stat.cacheSize, int64(cacheSize))
	//cacheTask.statLock.Unlock()

	atomic.AddInt64(&(manager.stat.cacheUsed), int64(cacheSize))

	// broadcast the result
out:
	for {
		select {
		case cacheTask.chItem <- result:
			// do nothing and resend the result on the next round
			break
		case _ = <-cacheTask.chQuit:
			//close(cacheTask.chItem)
			cacheTask.chItem = nil

			//return
			break out
		}
	}
	fmt.Printf("fetchItem %v is finished\n", key)
}

func (manager CacheManager) fetchFromUpstream(req *http.Request, key KeyType) *CacheItem {
	//atomic.AddInt64(&stats.BytesReadFromUpstream, int64(bodyLen))
	//logMessage("fetchFromUpstream: %s://%s%s", *upstreamProtocol, *upstreamHost, req.RequestURI)
	//logMessage("fetchFromUpstream: http://%s%s", *upstreamHost, req.RequestURI)

	//upstreamUrl := fmt.Sprintf("%s://%s%s", *upstreamProtocol, *upstreamHost, req.RequestURI)
	newuri := req.RequestURI
	if manager.prefix != "/" {
		newuri = newuri[len(manager.prefix):]
	}

	//upstreamUrl := fmt.Sprintf("http://%s%s", manager.upstreamHost, newuri)
	upstreamHost := manager.upstreamHost
	if upstreamHost == "" {
		pos := strings.Index(newuri[1:], "/")
		if pos >= 0 {
			upstreamHost = newuri[1 : pos+1]
			newuri = newuri[pos+1:]
		}
	}
	upstreamUrl := fmt.Sprintf("http://%s%s", upstreamHost, newuri)

	logMessage("fetchFromUpstream: %s", upstreamUrl)

	upstreamReq, err := http.NewRequest("GET", upstreamUrl, nil)
	if err != nil {
		logRequestError(req, "Cannot create request structure for [%v]: [%s]", key, err)
		return nil
	}
	upstreamReq.Host = getRequestHost(req, upstreamHost)

	upstreamClient := http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: Settings.maxIdleUpstreamConns,
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
	item := CacheItem{contentType: contentType, contentBody: body, ttl: MaxTtl}
	bodyLen := len(body)

	/*logMessage("upstreamUrl=%s", upstreamUrl)
	logMessage("contentType=%s\n", contentType)
	logMessage("contentLength=%d\n", bodyLen)*/

	atomic.AddInt64(&stats.BytesReadFromUpstream, int64(bodyLen))
	return &item
}
