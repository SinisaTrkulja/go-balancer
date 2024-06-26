package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const MaxPort = 65535

var getService = func() Backend {
	return Backend{}
}

type Backend struct {
	address string
	isAlive bool
	mux     *sync.RWMutex
}

var backends []Backend
var counter int
var responseTimes map[string]time.Duration

func main() {
	strategyFlag, portFlag, backendsFlag, timeoutFlag := parseFlags()
	backends = parseBackends(*backendsFlag)
	getService = getStrategy(*strategyFlag)

	s := &http.Server{
		Addr:           fmt.Sprintf(":%d", *portFlag),
		Handler:        myHandler{},
		ReadTimeout:    *timeoutFlag,
		WriteTimeout:   *timeoutFlag,
		MaxHeaderBytes: 1 << 20,
	}

	go periodicHealthCheck()
	fmt.Printf(`
  _________    ___       __                     
 / ___/ __ \  / _ )___ _/ /__ ____  _______ ____
/ (_ / /_/ / / _  / _  / / _  / _ \/ __/ -_) __/
\___/\____/ /____/\___/_/\___/_//_/\__/\__/_/   
	`)
	fmt.Println("GO Balancer, the load balancer that GOes")
	log.Fatal(s.ListenAndServe())
}

type myHandler struct{}

func (m myHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	response, err := forwardRequest(writer, request)
	if err != nil {
		http.Error(writer, err.Error(), 500)
		return
	}

	byteArray, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(writer, err.Error(), 500)
		return
	}

	err = response.Body.Close()
	if err != nil {
		http.Error(writer, err.Error(), 500)
		return
	}

	writer.WriteHeader(response.StatusCode)
	_, err = writer.Write(byteArray)

	if err != nil {
		http.Error(writer, err.Error(), 500)
		return
	}
	log.Println(request)
}
