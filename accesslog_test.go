package main

import (
	"fmt"
	"time"
	"testing"
)

var (
	//clients UpstreamParamSlice
)

func init() {
	//flag.Var(&clients, "client", "client")
}

func TestAccessLog(*testing.T) {
	
	aLog := NewAccessLog()
	
	quit := make(chan struct{})
	go aLog.StartAccessLog(quit)
	
	
	for i := 0; i < 1000; i++ {
		aLog.AddItem(LogItem{
			date: time.Now(), 
			s_ip: "192.168.0.31",
			cs_method: "GET",
			
		})
	}
	time.Sleep(1 * time.Second)
	
	//quit <- struct{}
	close(quit)
	time.Sleep(1 * time.Second)
	
	fmt.Println("TestAccessLog is called")
}
