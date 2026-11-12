package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"sync"
)

// An agent reports by ending with its result JSON as its final message. The
// final message comes from one of:
//
//   - the {lastmessage} file, when the agent's template passes it (codex -o):
//     the agent CLI writes it from outside the model's sandbox;
//   - the last {"type":"result"} event of Claude Code's stream-json output
//     (its "result" field);
//   - the last agent_message item of codex --json output;
//   - otherwise the agent's standard output as plain text.
//
// The result is a fenced json block or the bare object, and must name the
// change, step and round of the brief like a result file
// (formal/runs.tla, StaleNeverAccepted).

// messageLabel names the final message in errors and logs, where a result
// file's path would go.
const messageLabel = "final message"

// maxTextMessage bounds how much plain-text output is searched for a result.
const maxTextMessage = 256 << 10

// FinalMessage returns the agent's final message from its standard output:
// the last stream-json result event, the last codex agent message, or else
// the output itself (its last 256 KiB). ok is false when there is none.
func FinalMessage(stdout []byte) (msg string, ok bool) {
	var last string
	found := false
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type   string  `json:"type"`
			Result *string `json:"result"`
			Item   *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "result" && ev.Result != nil:
			last, found = *ev.Result, true
		case ev.Type == "item.completed" && ev.Item != nil && ev.Item.Type == "agent_message":
			last, found = ev.Item.Text, true
		}
	}
	if found {
		return last, true
	}
	if len(stdout) > maxTextMessage {
		stdout = stdout[len(stdout)-maxTextMessage:]
	}
	text := string(stdout)
	return text, strings.TrimSpace(text) != ""
}

// ReadLastMessage reads a {lastmessage} file. ok is false when the agent CLI
// did not write one.
func ReadLastMessage(path string) (msg string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(data) > maxResultSize {
		data = data[len(data)-maxResultSize:]
	}
	return string(data), true, nil
}

var fenceRE = regexp.MustCompile("(?s)```[ \t]*(?:json|JSON)?[ \t]*\r?\n(.*?)\r?\n[ \t]*```")

// MessageResult finds the result JSON in an agent's final message and checks
// it as ParseResult does. The message may be the bare object, or hold it in
// a fenced code block or at its end after other text. It returns a
// *MalformedError when there is no well-formed result and a *StaleError when
// the result names another change, step or round.
func MessageResult(msg string, want Expect) (Result, error) {
	text := strings.TrimSpace(msg)
	if text == "" {
		return Result{}, &MalformedError{messageLabel, "the final message is empty"}
	}
	if len(text) > maxResultSize {
		return Result{}, &MalformedError{messageLabel, fmt.Sprintf("larger than %d bytes", maxResultSize)}
	}
	return ParseResult(messageLabel, []byte(resultJSON(text)), want)
}

// resultJSON picks the text to parse as the result: the whole message if it
// is a JSON value, else the last fenced block holding a JSON object, else the
// last JSON object in the text that has a "change" key. If none is found the
// whole message is returned, so ParseResult reports why it is not JSON.
func resultJSON(text string) string {
	if json.Valid([]byte(text)) {
		return text
	}
	blocks := fenceRE.FindAllStringSubmatch(text, -1)
	for i := len(blocks) - 1; i >= 0; i-- {
		b := strings.TrimSpace(blocks[i][1])
		if strings.HasPrefix(b, "{") && json.Valid([]byte(b)) {
			return b
		}
	}
	// Search back from the end for an object with a "change" key; inner
	// objects (findings) have none, so the outermost result object wins.
	const maxTries = 4096
	tries := 0
	for end := len(text); end > 0 && tries < maxTries; tries++ {
		i := strings.LastIndexByte(text[:end], '{')
		if i < 0 {
			break
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var obj map[string]json.RawMessage
		if dec.Decode(&obj) == nil {
			if _, ok := obj["change"]; ok {
				return text[i : i+int(dec.InputOffset())]
			}
		}
		end = i
	}
	return text
}

// sameResult reports whether two accepted results say the same thing.
func sameResult(a, b Result) bool {
	if a.Change != b.Change || a.Step != b.Step || a.Round != b.Round ||
		a.Status != b.Status || strings.TrimSpace(a.Summary) != strings.TrimSpace(b.Summary) ||
		len(a.Findings) != len(b.Findings) {
		return false
	}
	for i := range a.Findings {
		if a.Findings[i] != b.Findings[i] {
			return false
		}
	}
	return true
}

// writeResult writes r as the result file at p, through a temporary file and
// a rename, so the engine's records are the same whichever way it arrived.
func writeResult(p string, r Result) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	// Trim only once well over the limit, so most writes copy nothing.
	if len(t.buf) > t.max+t.max/4 {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf
	if len(b) > t.max {
		b = b[len(b)-t.max:]
	}
	return bytes.Clone(b)
}

// lockedWriter serialises writes from the agent's stdout and stderr copiers
// into the step's log.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
