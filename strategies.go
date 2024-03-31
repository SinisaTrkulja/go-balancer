package main

import "math/rand"

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
