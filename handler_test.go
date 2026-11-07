package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DRuggeri/nut_exporter/v3/collectors"
)

func TestSplitList(t *testing.T) {
	for list, want := range map[string][]string{
		"":                       {},
		" , ,":                   {},
		"ups.firmware":           {"ups.firmware"},
		"ups.firmware, ups.mfr ": {"ups.firmware", "ups.mfr"},
		"a,,b\t":                 {"a", "b"},
	} {
		if have := splitList(list); !reflect.DeepEqual(have, want) {
			t.Errorf("splitList(%q) = %q, want %q", list, have, want)
		}
	}
}

func TestOptsFromQuery(t *testing.T) {
	base := collectors.NutCollectorOpts{
		Server:        "127.0.0.1",
		ServerPort:    3493,
		Variables:     []string{"ups.status"},
		InfoVariables: []string{"ups.firmware"},
		Statuses:      []string{"OL"},
	}

	query, _ := url.ParseQuery("ups=ups&serverport=3494&variables=battery.charge,%20ups.load&info_variables=ups.firmware,%20battery.mfr.date&statuses=OB")
	have := optsFromQuery(base, query)
	if have.Ups != "ups" || have.ServerPort != 3494 {
		t.Errorf("ups and serverport not applied: %+v", have)
	}
	if want := []string{"battery.charge", "ups.load"}; !reflect.DeepEqual(have.Variables, want) {
		t.Errorf("variables = %q, want %q", have.Variables, want)
	}
	if want := []string{"ups.firmware", "battery.mfr.date"}; !reflect.DeepEqual(have.InfoVariables, want) {
		t.Errorf("info_variables = %q, want %q", have.InfoVariables, want)
	}
	if want := []string{"OB"}; !reflect.DeepEqual(have.Statuses, want) {
		t.Errorf("statuses = %q, want %q", have.Statuses, want)
	}

	// Empty parameters, empty lists and invalid ports keep the command line values
	for _, raw := range []string{
		"serverport=abc&variables=&info_variables=&statuses=",
		"serverport=0&variables=,&info_variables=%20,%20&statuses=,,",
		"serverport=65536",
	} {
		query, _ = url.ParseQuery(raw)
		have = optsFromQuery(base, query)
		if have.ServerPort != 3493 || !reflect.DeepEqual(have.Variables, base.Variables) ||
			!reflect.DeepEqual(have.InfoVariables, base.InfoVariables) || !reflect.DeepEqual(have.Statuses, base.Statuses) {
			t.Errorf("%s: command line values not kept: %+v", raw, have)
		}
	}
}

func TestCacheKey(t *testing.T) {
	base := collectors.NutCollectorOpts{Server: "127.0.0.1", ServerPort: 3493, Ups: "ups", Password: "secret"}
	key := cacheKey(base)
	if strings.Contains(key, "secret") {
		t.Errorf("cache key holds the password: %s", key)
	}
	if same := base; cacheKey(same) != key {
		t.Errorf("the same options give another cache key")
	}

	for name, change := range map[string]func(*collectors.NutCollectorOpts){
		"server":         func(o *collectors.NutCollectorOpts) { o.Server = "nut" },
		"serverport":     func(o *collectors.NutCollectorOpts) { o.ServerPort = 3494 },
		"ups":            func(o *collectors.NutCollectorOpts) { o.Ups = "other" },
		"username":       func(o *collectors.NutCollectorOpts) { o.Username = "monitor" },
		"password":       func(o *collectors.NutCollectorOpts) { o.Password = "other" },
		"variables":      func(o *collectors.NutCollectorOpts) { o.Variables = []string{"ups.load"} },
		"info_variables": func(o *collectors.NutCollectorOpts) { o.InfoVariables = []string{"ups.firmware"} },
		"statuses":       func(o *collectors.NutCollectorOpts) { o.Statuses = []string{"OB"} },
	} {
		opts := base
		change(&opts)
		if cacheKey(opts) == key {
			t.Errorf("a different %s gives the same cache key", name)
		}
	}

	// Values holding separators must not collide
	a := collectors.NutCollectorOpts{Ups: "x|y", Username: "z"}
	b := collectors.NutCollectorOpts{Ups: "x", Username: "y|z"}
	c := collectors.NutCollectorOpts{Variables: []string{"a,b"}}
	d := collectors.NutCollectorOpts{Variables: []string{"a", "b"}}
	if cacheKey(a) == cacheKey(b) || cacheKey(c) == cacheKey(d) {
		t.Errorf("values holding separators share a cache key")
	}
}

// Concurrent first requests create handlers at the same time: run with -race
func TestConcurrentRequests(t *testing.T) {
	useOpts(t, collectors.NutCollectorOpts{Namespace: "network_ups_tools", Server: "127.0.0.1", ServerPort: 1})
	handler := &metricsHandler{handlers: make(map[string]*http.Handler)}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// half the requests share a handler, half create one
			request := httptest.NewRequest("GET", fmt.Sprintf("/ups_metrics?variables=var%d", i%10), nil)
			handler.ServeHTTP(httptest.NewRecorder(), request)
		}(i)
	}
	wg.Wait()

	if len(handler.handlers) != 10 {
		t.Errorf("%d handlers, want 10", len(handler.handlers))
	}
}

// A NUT server that does not answer must not block the requests of the other handlers
func TestSlowCreationDoesNotBlock(t *testing.T) {
	// Accepts connections and never answers: validating a UPS name there waits for the read timeout
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conns := make(chan net.Conn, 10)
	t.Cleanup(func() {
		silent.Close()
		for len(conns) > 0 {
			(<-conns).Close()
		}
	})
	go func() {
		for {
			conn, err := silent.Accept()
			if err != nil {
				return
			}
			conns <- conn
		}
	}()

	useOpts(t, collectors.NutCollectorOpts{Namespace: "network_ups_tools", Server: "127.0.0.1", ServerPort: 1})
	handler := &metricsHandler{handlers: make(map[string]*http.Handler)}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ups_metrics", nil))

	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET",
		fmt.Sprintf("/ups_metrics?ups=ups&serverport=%d", silent.Addr().(*net.TCPAddr).Port), nil))
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ups_metrics", nil))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a cached handler waited %s for the creation of another one", elapsed)
	}
}

// useOpts sets the command line options for one test
func useOpts(t *testing.T, opts collectors.NutCollectorOpts) {
	saved := collectorOpts
	collectorOpts = opts
	t.Cleanup(func() { collectorOpts = saved })
}
