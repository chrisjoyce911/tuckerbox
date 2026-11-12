package doctor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

func TestForgeAccessAndRejectedToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "private-secret")
	for _, status := range []int{200, 401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-secret" {
					t.Error("missing token")
				}
				w.WriteHeader(status)
				if status == 200 {
					fmt.Fprint(w, `{"permissions":{"push":true}}`)
				} else {
					fmt.Fprint(w, `{"message":"private-secret rejected"}`)
				}
			}))
			defer s.Close()
			line := CheckForge(context.Background(), forge.Target{Host: "github.com", Project: "sam/app", Kind: forge.GitHub, APIURL: s.URL}, forge.Credentials{}, s.Client())
			if strings.Contains(line, "private-secret") {
				t.Fatal("token exposed")
			}
			for _, want := range []string{"github", "sam/app", "GITHUB_TOKEN"} {
				if !strings.Contains(line, want) {
					t.Fatal(line)
				}
			}
			switch status {
			case 200:
				if !strings.Contains(line, "read access true; push access true") {
					t.Fatal(line)
				}
			case 401:
				if !strings.Contains(line, "GITHUB_TOKEN token was rejected for github.com") {
					t.Fatal(line)
				}
			case 403:
				if !strings.Contains(line, "access check failed") {
					t.Fatal(line)
				}
			}
		})
	}
}
func TestMissingTokenAndBorrowingOff(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	line := CheckForge(context.Background(), forge.Target{Host: "github.com", Project: "sam/app", Kind: forge.GitHub}, forge.Credentials{}, nil)
	if !strings.Contains(line, "no token") || !strings.Contains(line, "borrow_cli_token") || !strings.Contains(line, "token_env") {
		t.Fatal(line)
	}
}
