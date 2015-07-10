package main

import (
	"io"
	"time"
)

type CacheItem struct {
	//requestHost	string
	//requestUrl	string
	cacheTime time.Time

	contentType string
	contentBody []byte
	ttl         time.Duration
}

func (item *CacheItem) Close() {
	return
}

func (item *CacheItem) Size() int {
	return len(item.contentBody)
	/*size := 0
	if item.contentBody != nil {
		size += len(item.contentBody)
	}
	return size*/
}

/*func (item *CacheItem)Available() int {
	return len(item.contentBody)
}*/

func (item *CacheItem) WriteTo(w io.Writer) (bytesWritten int64, err error) {
	n, err := w.Write(item.contentBody)
	bytesWritten = int64(n)
	return
}
