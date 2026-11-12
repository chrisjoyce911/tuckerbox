package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

type preflightForge struct {
	forge.Client
	mr  *forge.MR
	err error
}

func (f preflightForge) FindMR(context.Context, string) (*forge.MR, error) { return f.mr, f.err }
func TestShippingPreflightMissingTokenForeignClaimAndOffline(t *testing.T) {
	for _, scenario := range []string{"no token", "foreign claim", "offline", "ours"} {
		t.Run(scenario, func(t *testing.T) {
			var out, stderr bytes.Buffer
			a := App{Stdout: &out, Stderr: &stderr}
			api := preflightForge{}
			want := true
			switch scenario {
			case "no token":
				want = false
			case "foreign claim":
				want = false
				api.mr = &forge.MR{Body: "<!-- tuckerbox: change=add-login machine=mini run=1 -->", URL: "https://forge.example/mr/1"}
			case "offline":
				api.err = fmt.Errorf("connection refused")
			case "ours":
				api.mr = &forge.MR{Body: "<!-- tuckerbox: change=add-login machine=studio run=1 -->", URL: "https://forge.example/mr/1"}
			}
			a.Forge = func(context.Context, *config.Config) (forge.Client, func(string) string, error) {
				if scenario == "no token" {
					return nil, nil, fmt.Errorf("no token: token_env or GH_TOKEN")
				}
				return api, nil, nil
			}
			cfg := &config.Config{Ship: config.ShipMR, Machine: "studio"}
			if got := a.shippingPreflight(context.Background(), runs.Repo{}, cfg, []string{"add-login"}); got != want {
				t.Fatal(got, stderr.String())
			}
			if scenario == "offline" && !strings.Contains(stderr.String(), "starting anyway") {
				t.Fatal(stderr.String())
			}
			if scenario == "foreign claim" && (!strings.Contains(stderr.String(), "mini") || !strings.Contains(stderr.String(), "https://forge.example/mr/1")) {
				t.Fatal(stderr.String())
			}
		})
	}
}
