package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
)

func at(s int) time.Time {
	return time.Date(2026, 11, 12, 9, 0, s, 0, time.FixedZone("AEST", 10*3600))
}

func TestAppendAndRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", FileName)
	code := 0
	in := []Event{
		{Time: at(0), Kind: Started, Change: "c", Step: "build", Round: 1, Agent: "claude", Machine: "m"},
		{Time: at(1), Kind: StepFinished, Change: "c", Step: "review", Round: 1, Status: agent.Rejected, ExitCode: &code,
			Findings: []agent.Finding{{Severity: "high", File: "a.go", Detail: "x"}}},
	}
	for _, ev := range in {
		if err := Append(p, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != Started || got[1].Findings[0].Detail != "x" || *got[1].ExitCode != 0 {
		t.Fatalf("got %+v", got)
	}
	if got[0].V != Version || !got[0].Time.Equal(at(0)) {
		t.Errorf("v %d time %v", got[0].V, got[0].Time)
	}
	if _, off := got[0].Time.Zone(); off != 10*3600 {
		t.Errorf("offset %d not kept", off)
	}
	data, _ := os.ReadFile(p)
	if n := strings.Count(string(data), "\n"); n != 2 || !strings.HasSuffix(string(data), "\n") {
		t.Errorf("want two whole lines:\n%s", data)
	}
}

func TestReadMissingFile(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), FileName))
	if err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

// A torn last line (a crash mid-write) is skipped on read, and the next
// append starts a line of its own.
func TestTornLastLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	if err := Append(p, Event{Time: at(0), Kind: Started, Change: "c", Round: 1}); err != nil {
		t.Fatal(err)
	}
	whole, _ := json.Marshal(Event{V: 1, Time: at(1), Kind: Queued, Change: "c", Round: 1})
	torn := whole[:len(whole)/2]
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(torn)
	f.Close()

	got, err := Read(p)
	if err != nil || len(got) != 1 || got[0].Kind != Started {
		t.Fatalf("with torn last line: %+v %v", got, err)
	}
	if err := Append(p, Event{Time: at(2), Kind: Stopped, Change: "c", Round: 1, Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	got, err = Read(p)
	if err != nil || len(got) != 2 || got[1].Kind != Stopped || got[1].Reason != "r" {
		t.Fatalf("after append: %+v %v", got, err)
	}
}

// A whole last event without its newline is still read.
func TestLastLineWithoutNewline(t *testing.T) {
	line, _ := json.Marshal(Event{V: 1, Time: at(0), Kind: Ready, Change: "c", Round: 2})
	got, err := Parse(strings.NewReader("\r\n" + string(line)))
	if err != nil || len(got) != 1 || got[0].Kind != Ready {
		t.Fatalf("%+v %v", got, err)
	}
}

// Readers ignore fields they don't know (fields may be added within a
// version).
func TestUnknownFieldsIgnored(t *testing.T) {
	got, err := Parse(strings.NewReader(`{"v":1,"time":"2026-11-12T09:00:00+10:00","kind":"queued","change":"c","round":1,"later":true}` + "\n"))
	if err != nil || len(got) != 1 || got[0].Kind != Queued {
		t.Fatalf("%+v %v", got, err)
	}
}
