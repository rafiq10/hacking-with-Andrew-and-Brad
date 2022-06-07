package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"sync"

	"github.com/google/uuid"
)

const (
	metaURL = "https://go.googlesource.com/?b=maser&format=JSON"
)

// https://pkg.go.dev/net/http/httputil#ReverseProxy ReverseProxy has no zero values
type Proxy struct {
	mu    sync.Mutex
	proxy *httputil.ReverseProxy
}

func main() {

	fmt.Println("%#v", gerritMetaMap())
	return

	p := new(Proxy)
	go p.run()
	http.Handle("/", p)
	log.Fatal(http.ListenAndServe("localhost:8080", nil))
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/_tipstatus" {
		p.serveStatus(w, r)
		return
	}

	p.mu.Lock()
	proxy := p.proxy
	p.mu.Unlock()
	if proxy == nil {
		http.Error(w, "not ready yet", http.StatusInternalServerError)
		return
	}
	proxy.ServeHTTP(w, r)
}
func (p *Proxy) serveStatus(w http.ResponseWriter, r *http.Request) {

}

func (p *Proxy) run() {
	for {
	}
}

// gerritMetaMap returns the map from repo name (e.g. "go") to its
// latest master hash.
// The returned map is nil on any transient error.
func gerritMetaMap() map[string]string {
	res, err := http.Get(metaURL)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	defer io.Copy(ioutil.Discard, res.Body) // ensure EOF for keep-alive
	if res.StatusCode != 200 {
		return nil
	}
	var meta map[string]struct {
		b map[string]struct{}
	}
	br := bufio.NewReader(res.Body)
	// For security reasons or something, this URL starts with ")]}'\n" before
	// the JSON object. So ignore that.
	// Shawn Pearce says it's guaranteed to always be just one line, ending in '\n'.
	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil
		}
		if b == '\n' {
			break
		}
	}
	if err := json.NewDecoder(br).Decode(&meta); err != nil {
		log.Printf("JSON decoding error from %v: %s", metaURL, err)
		return nil
	}
	m := map[string]string{}
	for repo, _ := range meta {
		m[repo] = uuid.New().String()
	}
	return m
}
