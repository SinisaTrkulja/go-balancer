package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var getBackend = func() Backend {
	return Backend{}
}

type Backend struct {
	address string
	isAlive bool
	ipv6    bool
	mux     *sync.RWMutex
}

var backends []Backend
var counter int
var responseTimes map[string]time.Duration

func main() {
	strategyFlag := flag.String("strategy", "random", "Target-choosing strategy")
	portFlag := flag.Int("port", 8080, "Load balancer port")
	ipv6Flag := flag.Bool("ipv6", false, "Specify whether ipv6")
	backendsFlag := flag.String("services", "", "Pass in the comma-separated service URLs")
	timeoutFlag := flag.Duration("timeout", 10*time.Second, "Pass in the timeout")
	flag.Parse()

	if len(strings.TrimSpace(*backendsFlag)) == 0 {
		panic("No targets listed")
	}
	if *portFlag < 0 || *portFlag > 65000 {
		panic("Invalid port")
	}

	backends = parseBackends(*backendsFlag, *ipv6Flag)
	healthcheck()
	getBackend = getStrategy(*strategyFlag)

	s := &http.Server{
		Addr:           fmt.Sprintf(":%d", *portFlag),
		Handler:        myHandler{},
		ReadTimeout:    *timeoutFlag,
		WriteTimeout:   *timeoutFlag,
		MaxHeaderBytes: 1 << 20,
	}

	go healthCheck()
	fmt.Println("GO Balancer, the load balancer that goes")
	fmt.Println("Starting server")
	log.Fatal(s.ListenAndServe())
}

func (m myHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	err, response := sendRequest(writer, request)
	byteArray, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(writer, err.Error(), 500)
	}

	writer.WriteHeader(response.StatusCode)
	_, err = writer.Write(byteArray)

	if err != nil {
		http.Error(writer, err.Error(), 500)
	}

	log.Println(request)
}

func sendRequest(writer http.ResponseWriter, request *http.Request) (error, *http.Response) {
	var backend Backend
	for {
		backend = getBackend()
		if backend.isAlive {
			break
		}
	}
	response, err := handleConnection(writer, request, backend.address)
	defer func(body io.ReadCloser) {
		err := body.Close()
		if err != nil {
			log.Println(fmt.Sprintf("An issue appeared with closing the connection: %s", err))
		}
	}(response.Body)

	if err != nil {
		http.Error(writer, err.Error(), 500)
	}
	return err, response
}

var handleConnection = func(writer http.ResponseWriter, request *http.Request, address string) (*http.Response, error) {
	request, err := http.NewRequest(
		request.Method,
		fmt.Sprintf("http://%s%s", address, request.RequestURI),
		request.Body)

	if err != nil {
		http.Error(writer, err.Error(), 500)
	}
	return http.DefaultClient.Do(request)
}

func handleAndTrackResponseTime(writer http.ResponseWriter, request *http.Request, address string) (*http.Response, error) {
	start := time.Now()
	response, err := handleConnection(writer, request, address)
	responseTimes[address] = (responseTimes[address] + time.Since(start)) / 2
	return response, err
}

// Utils

type myHandler struct{}

func parseBackends(backendsArg string, ipv6 bool) []Backend {
	bknds := strings.Split(backendsArg, ",")
	newBknds := make([]Backend, len(bknds))
	for i := range bknds {
		newBknds = append(newBknds, Backend{bknds[i], true, ipv6, &sync.RWMutex{}})
	}
	return newBknds
}

func healthCheck() {
	t := time.NewTicker(time.Second * 20)
	for {
		select {
		case <-t.C:
			healthcheck()
		}
	}
}

func healthcheck() {
	log.Println("Starting health check...")
	for i := range backends {
		backends[i].mux.RLock()
		backends[i].isAlive = isAlive(backends[i].address)
		backends[i].mux.RUnlock()
	}
	log.Println("Health check completed")
}

func isAlive(backend string) bool {
	connection, err := net.DialTimeout("tcp", backend, time.Second*2)
	defer func(connection net.Conn) {
		if connection != nil {
			err := connection.Close()
			if err != nil {
				log.Println(fmt.Sprintf("An issue appeared with closing the connection: %s", err))
			}
		}
	}(connection)

	if err != nil {
		log.Println(fmt.Sprintf("Service down: %s", backend))
		return false
	}
	return true
}

func getStrategy(s string) func() Backend {
	switch s {
	case "round-robin":
		return roundRobin
	case "random":
		return random
	case "avg-duration":
		initializeResponseTimeMap()
		handleConnection = handleAndTrackResponseTime
		return byAvgResponseTime
	default:
		panic("No matching strategy")
	}
}

// Strategies
func random() Backend {
	return backends[rand.Intn(len(backends))]
}

func roundRobin() Backend {
	s := backends[counter]
	if counter == len(backends)-1 {
		counter = 0
	} else {
		counter++
	}
	return s
}

func initializeResponseTimeMap() {
	responseTimes = make(map[string]time.Duration, len(backends))
	for _, backend := range backends {
		responseTimes[backend.address] = 0
	}
}

func byAvgResponseTime() Backend {
	index := 0
	minTime := responseTimes[backends[index].address]
	backend := backends[index]
	for _, value := range responseTimes {
		if value < minTime {
			minTime = value
			backend = backends[index]
		}
		index++
	}
	return backend
}
