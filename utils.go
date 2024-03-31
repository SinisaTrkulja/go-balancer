package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func parseFlags() (*string, *int, *bool, *string, *time.Duration) {
	strategyFlag := flag.String("strategy", "random", "Target-choosing strategy")
	portFlag := flag.Int("port", 8080, "Load balancer port")
	ipv6Flag := flag.Bool("ipv6", false, "Specify whether ipv6")
	backendsFlag := flag.String("services", "", "Pass in the comma-separated service URLs")
	timeoutFlag := flag.Duration("timeout", 10*time.Second, "Pass in the timeout")
	flag.Parse()

	if len(strings.TrimSpace(*backendsFlag)) == 0 {
		panic("No targets listed")
	}
	if *portFlag < 0 || *portFlag > MaxPort {
		panic("Invalid port")
	}

	return strategyFlag, portFlag, ipv6Flag, backendsFlag, timeoutFlag
}

func forwardRequest(writer http.ResponseWriter, request *http.Request) *http.Response {
	var backend Backend
	for {
		backend = getService()
		if backend.isAlive {
			break
		}
	}
	response, err := handleConnection(writer, request, backend.address)
	if err != nil {
		http.Error(writer, err.Error(), 500)
	}
	return response
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

func parseBackends(backendsArg string, ipv6 bool) []Backend {
	if len(strings.TrimSpace(backendsArg)) == 0 {
		panic("No services passed")
	}

	bknds := strings.Split(backendsArg, ",")
	newBknds := make([]Backend, len(bknds))
	for i := range bknds {
		newBknds[i] = Backend{bknds[i], true, ipv6, &sync.RWMutex{}}
	}
	return newBknds
}

func periodicHealthCheck() {
	t := time.NewTicker(time.Second * 10)
	for {
		select {
		case <-t.C:
			healthCheck()
		}
	}
}

func healthCheck() {
	faultyServices := make([]string, 0, len(backends))
	log.Println("Starting health check...")
	for i := range backends {
		backends[i].mux.RLock()
		backends[i].isAlive = isAlive(backends[i].address)
		backends[i].mux.RUnlock()
		if !backends[i].isAlive {
			faultyServices = append(faultyServices, backends[i].address)
		}
	}
	log.Println("Health check completed")
	if len(faultyServices) != 0 {
		log.Printf("Faulty or unreachable service/s: %s", strings.Join(faultyServices, ", "))
	}
}

func isAlive(backend string) bool {
	connection, err := net.DialTimeout("tcp", backend, time.Second*2)
	if connection != nil {
		err := connection.Close()
		if err != nil {
			log.Println(fmt.Sprintf("An issue appeared with closing the connection: %s", err))
		}
	}
	if err != nil {
		log.Println(fmt.Sprintf("Service down: %s", backend))
		return false
	}
	return true
}

func initializeResponseTimeMap() {
	responseTimes = make(map[string]time.Duration, len(backends))
	for _, backend := range backends {
		responseTimes[backend.address] = 0
	}
}

func handleAndTrackResponseTime(writer http.ResponseWriter, request *http.Request, address string) (*http.Response, error) {
	start := time.Now()
	response, err := handleConnection(writer, request, address)
	responseTimes[address] = (responseTimes[address] + time.Since(start)) / 2
	return response, err
}
