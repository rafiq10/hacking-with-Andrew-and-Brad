package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	metaURL = "https://go.googlesource.com/?b=maser&format=JSON"
)

var (
	pollInterval = flag.Duration("poll", 10*time.Second, "Remote poll inerval")
	listenAddr   = flag.String("listen", "localhost:8080", "HTTP listen address")
)

// https://pkg.go.dev/net/http/httputil#ReverseProxy ReverseProxy has no zero values
type Proxy struct {
	// owned by poll loop
	last string // signature of gorepo+toolsrepo
	side string

	mu    sync.Mutex
	proxy *httputil.ReverseProxy
}

func main() {
	flag.Parse()
	p := new(Proxy)
	go p.run()
	http.Handle("/", p)
	log.Fatal(http.ListenAndServe(*listenAddr, nil))
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
	p.side = "a"
	for {
		p.poll()
		time.Sleep(*pollInterval)
	}
}

func (p *Proxy) poll() {
	heads := gerritMetaMap()
	if heads == nil {
		return
	}
	sig := heads["go"] + "-" + heads["tools"]
	if sig == p.last {
		return
	}
	newSide := "b"
	if p.side == "b" {
		newSide = "a"
	}
	hostport, err := p.initSide(newSide, heads["go"], heads["tools"])
	if err != nil {
		log.Println(err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := url.Parse(fmt.Sprintf("http://%v/", hostport))
	if err != nil {
		log.Println(err)
		return
	}
	p.side = newSide
	p.proxy = httputil.NewSingleHostReverseProxy(u)
}

func (p *Proxy) initSide(side, goHash, toolsHash string) (hostport string, err error) {
	dir := filepath.Join(os.TempDir(), "godoc", side)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	goDir := filepath.Join(dir, "go")
	toolsDir := filepath.Join(dir, "gopath/src/golang.org/x/tools")
	if err = checkout("https://go.googlesource.com/go", goHash, goDir); err != nil {
		return "", err
	}
	if err = checkout("https://go.googlesource.com/tools", toolsHash, toolsDir); err != nil {
		return "", err
	}
	return "", nil
}

func checkout(repo, hash, path string) error {

	// Clone git repoif it doesn't exist
	fullPath := filepath.Join(path, ".git")

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Base(path), 0755); err != nil {
			return err
		}
		if err := exec.Command("git", "clone", repo, path).Run(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	cmd := exec.Command("git", "fetch")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("git", "reset", "--hard", hash)
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("git", "clean", "-d", "-f", "-x")
	cmd.Dir = path
	return cmd.Run()
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
