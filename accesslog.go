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
	cs_username	string
	c_ip		string
	cs_User_Agent	string
	cs_Referer		string
	sc_status 		int
	sc_substatus 	int
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
	aLog.log.Printf("#Start-Date: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	aLog.log.Printf("#Date: %s\n", time.Now().Format("2006-01-02"))
	//aLog.log.Printf("#Date: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fields := []string {
		"date",
		"time",
		"s-ip",
		"cs-method",
		"cs-uri-stem",
		"cs-uri-query",
		"s-port",
		"cs-username",
		"c-ip",
		"cs(User-Agent)",
		"cs(Referer)",
		"sc-status",
		"sc-substatus",
		//"sc-win32-status ",
		"time-taken",
	}
	
	aLog.log.Printf("#Fields: " + strings.Join(fields, "\t"))	
}

func logString(s string) string {
	if s == "" {
		return "-"
	}
	return s
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
			s_ip := item.s_ip
			if s_ip == "" { s_ip = "-"}
			c_ip := item.c_ip
			if c_ip == "" { c_ip = "-"}
		
			ss := []string {
				item.date.Format("2006-01-02"),
				item.date.Format("03:04:05"),
				logString(item.s_ip),
				logString(item.cs_method),
				logString(item.cs_uri_stem),
				logString(item.cs_uri_query),
				strconv.Itoa(item.s_port),
				logString(item.cs_username),
				logString(item.c_ip),
				logString(item.cs_User_Agent),
				logString(item.cs_Referer),
				strconv.Itoa(item.sc_status),
				strconv.Itoa(item.sc_substatus),
				//strconv.Itoa(sc-win32-status),
				strconv.Itoa(item.time_taken),
			}
			
			aLog.log.Printf("%s\n", strings.Join(ss, "\t"))
		}
	}
}

