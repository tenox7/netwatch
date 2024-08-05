package main

import (
	"math"
	"math/rand"
	"time"
)

type probe func(string, chan []float64) error

var probes = map[string]probe{
	// Basic test probe
	"sine": func(target string, c chan []float64) error {
		go func() {
			i := 0.0
			for {
				c <- []float64{math.Sin(i) + 1}
				i += 0.1
			}
		}()
		return nil
	},
	// Timeout test probe
	"lagsine": func(target string, c chan []float64) error {
		go func() {
			i := 0.0
			for range time.Tick(interval * 5) {
				c <- []float64{math.Sin(i) + 1}
				i += 0.1
			}
		}()
		return nil
	},
	// Multi-plot test probe
	"multi": func(target string, c chan []float64) error {
		go func() {
			i := 0.0
			for {
				c <- []float64{math.Sin(i) + 1, math.Cos(i) + 1, rand.Float64() * 2}
				i += 0.1
			}
		}()
		return nil
	},
}
