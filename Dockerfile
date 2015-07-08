# Pull base image.
#FROM google/golang
FROM golang:latest


# Define working directory.
#WORKDIR /gopath/bin
WORKDIR /go/bin

# fetch
#ADD . /gopath/src/proxy3
ADD . /go/src/proxy3

# fetch the dependencies
RUN go get github.com/vharitonsky/iniflags
RUN go get github.com/yangpingcd/mem

# build the proxy3 project
RUN go build proxy3


# Expose ports.
#   - 8080: HTTP
EXPOSE 8080