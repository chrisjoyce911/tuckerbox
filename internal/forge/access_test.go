package forge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	for _, tt := range []struct {
		remote             string
		settings           Settings
		machine            Kind
		host, project, api string
		kind               Kind
	}{
		{"git@github.com:sam/app.git", Settings{}, "", "github.com", "sam/app", "https://api.github.com", GitHub},
		{"ssh://git@github.com/sam/app.git", Settings{}, "", "github.com", "sam/app", "https://api.github.com", GitHub},
		{"https://gitlab.com/team/tools/app.git", Settings{}, "", "gitlab.com", "team/tools/app", "https://gitlab.com/api/v4", GitLab},
		{"https://git.example.org/team/app", Settings{Kind: GitLab}, "", "git.example.org", "team/app", "https://git.example.org/api/v4", GitLab},
		{"git@code.example.org:sam/app.git", Settings{}, GitHub, "code.example.org", "sam/app", "https://code.example.org/api/v3", GitHub},
		{"https://git.example.org/team/app", Settings{Kind: GitLab, APIURL: "https://git.example.org/custom/api"}, "", "git.example.org", "team/app", "https://git.example.org/custom/api", GitLab},
	} {
		t.Run(tt.remote+tt.api, func(t *testing.T) {
			// HTTPS tests never make network calls; SSH queries only local config.
			got, err := Detect(context.Background(), tt.remote, tt.settings, tt.machine)
			if err != nil || got.Host != tt.host || got.Project != tt.project || got.APIURL != tt.api || got.Kind != tt.kind {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	for _, api := range []string{"https://evil.example/api", "http://git.example.org/api", "https://user:secret@git.example.org/api", "https://git.example.org:444/api", "https://git.example.org/api?token=secret", "https://git.example.org/api#x"} {
		_, err := DetectKey("git.example.org/team/app", Settings{Kind: GitLab, APIURL: api}, "")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %v", api, err)
		}
	}
	if _, err := DetectKey("unknown.example/team/app", Settings{}, ""); err == nil || !strings.Contains(err.Error(), "[forge] kind") {
		t.Fatal(err)
	}
	for _, key := range []string{"/tmp/repo", "host/../repo", "host/team//repo", "host/team/./repo", "host/team/repo?token=x"} {
		if _, err := DetectKey(key, Settings{Kind: GitLab}, ""); err == nil {
			t.Fatal(key)
		}
	}
}

func TestTokenSources(t *testing.T) {
	env := map[string]string{"GH_TOKEN": "gh-secret", "GITHUB_TOKEN": "github-secret", "GITLAB_TOKEN": "lab-secret", "WORK_TOKEN": "work-secret"}
	get := func(k string) string { return env[k] }
	noBorrow := func(context.Context, string, ...string) (string, error) {
		t.Fatal("unexpected CLI invocation")
		return "", nil
	}
	for _, tt := range []struct {
		host string
		cfg  Credentials
		want string
	}{
		{"github.com", Credentials{}, "GH_TOKEN"}, {"gitlab.com", Credentials{}, "GITLAB_TOKEN"},
		{"git.example.org", Credentials{}, ""}, {"git.example.org", Credentials{TokenEnv: "WORK_TOKEN"}, "WORK_TOKEN"},
		{"github.com", Credentials{TokenEnv: "WORK_TOKEN", BorrowCLIToken: true}, "WORK_TOKEN"},
	} {
		tok, err := LookupToken(context.Background(), Target{Host: tt.host}, tt.cfg, get, noBorrow)
		if err != nil || tok.Source != tt.want {
			t.Fatalf("%+v %v", tok, err)
		}
	}
	delete(env, "GH_TOKEN")
	tok, _ := LookupToken(context.Background(), Target{Host: "github.com"}, Credentials{}, get, noBorrow)
	if tok.Source != "GITHUB_TOKEN" {
		t.Fatal(tok)
	}
	for _, kind := range []Kind{GitHub, GitLab} {
		tok, err := LookupToken(context.Background(), Target{Host: "code.example", Kind: kind}, Credentials{BorrowCLIToken: true}, func(string) string { return "" }, func(ctx context.Context, p string, args ...string) (string, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second {
				t.Fatal("missing timeout")
			}
			want := "gh auth token --hostname code.example"
			if kind == GitLab {
				want = "glab config get token --host code.example"
			}
			if p+" "+strings.Join(args, " ") != want {
				t.Fatal(p, args)
			}
			return "borrowed-secret\n", nil
		})
		if err != nil || !tok.Available() || strings.Contains(fmt.Sprintf("%#v %v %+v", tok, tok, tok), "borrowed-secret") {
			t.Fatal("unsafe token formatting")
		}
	}
	tok, err := LookupToken(context.Background(), Target{Host: "code.example", Kind: GitHub}, Credentials{BorrowCLIToken: true}, func(string) string { return "" }, func(context.Context, string, ...string) (string, error) { return "leaked", fmt.Errorf("leaked") })
	if tok.Available() || err == nil || strings.Contains(err.Error(), "leaked") {
		t.Fatal("CLI failure leaked credentials")
	}
}

// The fake gh/glab is this test executable, copied under the command's name.
// This exercises real exec lookup and stdout with no shell or platform scripts.
func TestMain(m *testing.M) {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "gh" || name == "glab" {
		fmt.Print(os.Getenv("FAKE_AUTH_TOKEN"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestBorrowFakeCLIAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gh", "glab"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_AUTH_TOKEN", "borrowed-secret")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	before := strings.Join(os.Environ(), "\n")
	for _, target := range []Target{{Host: "github.com", Kind: GitHub}, {Host: "gitlab.com", Kind: GitLab}} {
		tok, err := ReadToken(context.Background(), target, Credentials{BorrowCLIToken: true})
		if err != nil || !tok.Available() {
			t.Fatal(tok, err)
		}
		tok, err = ReadToken(context.Background(), target, Credentials{})
		if err != nil || tok.Available() {
			t.Fatal(tok, err)
		}
	}
	if strings.Join(os.Environ(), "\n") != before {
		t.Fatal("token lookup mutated child environment")
	}
}

func TestHTTPSCredentialsRedirectAndMasking(t *testing.T) {
	secret := Token{value: "private-secret", Source: "test"}
	received := 0
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received++; t.Error("redirect followed") }))
	defer sink.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-secret" {
			t.Error("missing authorization")
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, sink.URL, 302)
		case "/same":
			http.Redirect(w, r, "/log", 307)
		case "/error":
			http.Error(w, "private-secret", 401)
		default:
			fmt.Fprint(w, "log private-secret")
		}
	}))
	defer server.Close()
	h, err := NewHTTP(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var log string
	if _, err := h.Do(context.Background(), "GET", "/log", nil, &log); err != nil || log != "log ***" {
		t.Fatal(log, err)
	}
	for _, path := range []string{"/redirect", "/same", "/error"} {
		_, err := h.Do(context.Background(), "GET", path, nil, nil)
		if err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatal(err)
		}
	}
	if received != 0 {
		t.Fatal(received)
	}
	for _, endpoint := range []string{sink.URL, "//evil/path", "/\\evil", "/foo#fragment"} {
		if _, err := h.Do(context.Background(), "GET", endpoint, nil, nil); err == nil {
			t.Fatal(endpoint)
		}
	}
	if _, err := NewHTTP("http://example.org", secret, nil); err == nil {
		t.Fatal("HTTP accepted")
	}
}

func TestETagsRateLimits(t *testing.T) {
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("ETag", `"v1"`)
			io.WriteString(w, `{"state":"ok"}`)
		case 2:
			if r.Header.Get("If-None-Match") != `"v1"` {
				t.Error("missing ETag")
			}
			w.WriteHeader(304)
		case 3:
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "40")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(12*time.Minute).Unix()))
			io.WriteString(w, `{"state":"ok"}`)
		}
	}))
	defer s.Close()
	h, _ := NewHTTP(s.URL, Token{}, s.Client())
	for i := 0; i < 3; i++ {
		var got map[string]string
		if _, err := h.Do(context.Background(), "GET", "/state", nil, &got); err != nil || got["state"] != "ok" {
			t.Fatal(got, err)
		}
	}
	if _, err := h.Do(context.Background(), "GET", "/state", nil, nil); err == nil || calls != 3 {
		t.Fatal(calls, err)
	}
	if time.Until(h.RetryAt()) < 11*time.Minute {
		t.Fatal(h.RetryAt())
	}
}

func TestSignedJobLogDownloadNeverGetsCredentials(t *testing.T) {
	token := Token{value: "private-secret", Source: "test"}
	calls := 0
	download := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials sent to download host")
		}
		if r.URL.Path == "/again" {
			http.Redirect(w, r, "/log", 302)
			return
		}
		fmt.Fprint(w, "job log: private-secret")
	}))
	defer download.Close()
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-secret" {
			t.Error("API missing token")
		}
		destination := download.URL + "/log"
		switch r.URL.Path {
		case "/again":
			destination = download.URL + "/again"
		case "/insecure":
			destination = "http://example.org/log"
		}
		http.Redirect(w, r, destination, 302)
	}))
	defer api.Close()
	// httptest certificates share their CA, so the local API client trusts both servers.
	h, _ := NewHTTP(api.URL, token, api.Client())
	got, err := h.JobLog(context.Background(), "/logs")
	if err != nil || got.Text != "job log: ***" {
		t.Fatal(got, err)
	}
	if _, err := h.JobLog(context.Background(), "/again"); err == nil {
		t.Fatal("second redirect followed")
	}
	if _, err := h.JobLog(context.Background(), "/insecure"); err == nil {
		t.Fatal("HTTP log URL accepted")
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestRetryAfterAndGitLabRateFloorPreventRequests(t *testing.T) {
	for _, retryAfter := range []bool{false, true} {
		t.Run(fmt.Sprint(retryAfter), func(t *testing.T) {
			calls := 0
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if retryAfter {
					w.Header().Set("Retry-After", "120")
					w.WriteHeader(429)
				} else {
					w.Header().Set("RateLimit-Limit", "100")
					w.Header().Set("RateLimit-Remaining", "5")
					w.Header().Set("RateLimit-Reset", fmt.Sprint(time.Now().Add(12*time.Minute).Unix()))
					fmt.Fprint(w, `{}`)
				}
			}))
			defer s.Close()
			h, err := NewHTTP(s.URL, Token{}, s.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, _ = h.Do(context.Background(), "GET", "/state", nil, nil)
			if _, err := h.Do(context.Background(), "GET", "/state", nil, nil); err == nil || calls != 1 {
				t.Fatal(calls, err)
			}
			if time.Until(h.RetryAt()) < time.Minute {
				t.Fatal(h.RetryAt())
			}
		})
	}
}

func TestLargeJobLogStreamsBoundedTailAndCountsLines(t *testing.T) {
	line := strings.Repeat("x", 511) + "\n"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat(line, 20000)) }))
	defer server.Close()
	h, err := NewHTTP(server.URL, Token{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	log, err := h.JobLog(context.Background(), "/log")
	if err != nil || !log.Truncated || len(log.Text) > 256<<10 || log.OmittedLines+strings.Count(log.Text, "\n") != 20000 {
		t.Fatal(len(log.Text), log.OmittedLines, err)
	}
}
