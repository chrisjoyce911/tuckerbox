// Package forgetest replays sanitized API fixtures on local TLS servers only.
package forgetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type Exchange struct {
	Method, Path string
	Request      json.RawMessage
	Response     json.RawMessage
	Text         string
	Status       int
	Headers      map[string]string
}

func Replay(t *testing.T, path, base string) *forge.HTTP {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []Exchange
	if err := json.Unmarshal(b, &fixtures); err != nil {
		t.Fatal(err)
	}
	i := 0
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(fixtures) {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		f := fixtures[i]
		i++
		if r.Method != f.Method || r.URL.RequestURI() != base+f.Path {
			t.Errorf("request %d: %s %s, want %s %s", i, r.Method, r.URL.RequestURI(), f.Method, base+f.Path)
		}
		if f.Request != nil {
			var want, got any
			if err := json.Unmarshal(f.Request, &want); err != nil {
				t.Error(err)
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("request %d body: %#v want %#v", i, got, want)
			}
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("fixture request unexpectedly authenticated")
		}
		for k, v := range f.Headers {
			w.Header().Set(k, v)
		}
		if f.Status != 0 {
			w.WriteHeader(f.Status)
		}
		if f.Response != nil {
			fmt.Fprint(w, string(f.Response))
		} else {
			fmt.Fprint(w, f.Text)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		mu.Lock()
		defer mu.Unlock()
		if i != len(fixtures) {
			t.Errorf("replayed %d/%d fixture requests", i, len(fixtures))
		}
	})
	h, err := forge.NewHTTP(strings.TrimRight(server.URL, "/")+base, forge.Token{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return h
}
