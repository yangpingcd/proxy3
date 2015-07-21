package main

import (
	//"fmt"
	"strings"
	"strconv"
	"log"
	"os"
	"io"
	"bufio"
	"time"
	"sync"
	
	//"github.com/natefinch/lumberjack"
	"gopkg.in/natefinch/lumberjack.v2"
)



type LogItem struct {
	date		time.Time
	//time
	s_ip		string
	cs_method	string
	cs_uri_stem string
	cs_uri_query string
	s_port		int
	//cs-username
	c_ip		string
	//cs(User-Agent)
	//cs(Referer)
	sc_status int
	sc_substatus int
	//sc-win32-status int
	time_taken int
}

type AccessLog struct {
	logItemsMutex	sync.Mutex
	logItems 		[]LogItem
	
	
	logWriter io.Writer
	//accessLogWriter bufio.Writer
	log *log.Logger
}

func NewAccessLog(setting LogSetting) AccessLog {
	aLog := AccessLog {
		logItemsMutex:	sync.Mutex{},
		logItems:  		make([]LogItem, 0),
	}
	
	if setting.Filename == "" {
		aLog.logWriter = os.Stdout
		if false {
			aLog.logWriter = bufio.NewWriter(os.Stdout)
		}
		
		//aLog.log = log.New(aLog.logWriter, "", log.Ldate|log.Ltime)
		aLog.log = log.New(aLog.logWriter, "", 0)
	
	} else {
		aLog.log = log.New(&lumberjack.Logger{
			Filename:	setting.Filename,
			MaxSize:    setting.MaxSize,
			MaxBackups: setting.MaxBackups,
			MaxAge:     setting.MaxAge,
			LocalTime:  setting.LocalTime,
		}, "", 0)
	}
		
	return aLog
}

func (aLog *AccessLog) AddItem(item LogItem) {
	aLog.logItemsMutex.Lock()
	defer aLog.logItemsMutex.Unlock()	
	
	aLog.logItems = append(aLog.logItems, item)
}

func writeAccessLogHeader(aLog *AccessLog) {
	// write header	
	aLog.log.Printf("#Software: Proxy3\n")
	aLog.log.Printf("#Version: 1.0\n")
	aLog.log.Printf("#Date: %s\n", time.Now().Format("2006-01-02 03:04:05"))
	//"date time s-ip cs-method cs-uri-stem cs-uri-query s-port cs-username c-ip cs(User-Agent) cs(Referer) sc-status sc-substatus sc-win32-status time-taken"
	aLog.log.Printf("#Fields: date time s-ip cs-method cs-uri-query s-port c-ip sc-status time-taken\n")
}

func (aLog *AccessLog) StartAccessLog(quit chan struct{}) {
	writeAccessLogHeader(aLog)
	
out:
	for {
		select {
			case <-quit:
				break out
				
			case <-time.After(200*time.Millisecond):
				break
		}				
		
		// copy the logs 
		aLog.logItemsMutex.Lock()
		items := aLog.logItems
		aLog.logItems = make([]LogItem, 0)
		aLog.logItemsMutex.Unlock()
		
		for _, item := range(items) {
			ss := []string {
				item.date.Format("2006-01-02"),
				item.date.Format("03:04:05"),
				item.s_ip,
				item.cs_method,
				item.cs_uri_stem,
				item.cs_uri_query,
				strconv.Itoa(item.s_port),
				//cs-username
				item.c_ip,
				//cs(User-Agent)
				//cs(Referer)
				strconv.Itoa(item.sc_status),
				strconv.Itoa(item.sc_substatus),
				//sc-win32-status int
				strconv.Itoa(item.time_taken),
			}
			
			aLog.log.Printf("%s\n", strings.Join(ss, "\t"))
		}
	}
}

