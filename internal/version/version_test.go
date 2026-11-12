package version

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestGetDefaults(t *testing.T) {
	i := Get()
	if i.Version != Version {
		t.Errorf("Version = %q, want %q", i.Version, Version)
	}
	if i.Commit == "" {
		t.Error("Commit is empty")
	}
	if i.GoVersion != runtime.Version() || i.OS != runtime.GOOS || i.Arch != runtime.GOARCH {
		t.Errorf("runtime fields = %+v", i)
	}
}

func TestGetUsesLdflagsValues(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })
	Version, Commit = "v1.2.3", "abc1234"
	i := Get()
	if i.Version != "v1.2.3" || i.Commit != "abc1234" {
		t.Errorf("Get() = %+v", i)
	}
}

func TestString(t *testing.T) {
	i := Info{Version: "v1.2.3", Commit: "abc1234", GoVersion: "go1.23.0", OS: "windows", Arch: "amd64"}
	want := "tuckerbox v1.2.3\ncommit: abc1234\ngo: go1.23.0\nplatform: windows/amd64\n"
	if got := i.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(i.String(), "tuckerbox ") {
		t.Error("missing prefix")
	}
}

func TestJSON(t *testing.T) {
	i := Info{Version: "v1.2.3", Commit: "abc1234", GoVersion: "go1.23.0", OS: "windows", Arch: "amd64"}
	want := `{"version":"v1.2.3","commit":"abc1234","go":"go1.23.0","os":"windows","arch":"amd64"}` + "\n"
	got, err := i.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("JSON() = %q, want %q", got, want)
	}
	var back Info
	if err := json.Unmarshal(got, &back); err != nil || back != i {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}
