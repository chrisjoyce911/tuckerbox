package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// The turn hook. In a terminal, an interactive agent CLI does not exit
// after its answer, so its final message can't be read from its output
// at exit. Instead each CLI runs a command at the end of every turn:
// Claude Code its Stop hook, with JSON on standard input that names the
// session's transcript; Codex its notify program, with JSON as the last
// argument whose last-assistant-message is the final message. That
// command is the hidden `tuckerbox agent-turn`, which writes the message
// to the step's last-message file, atomically, and does nothing else.
// The terminal backend reads the file with the result contract.

// Turn hook payload formats, as agent-turn's flags name them.
const (
	// TurnClaude is a Claude Code Stop hook payload on standard input.
	TurnClaude = "claude"
	// TurnCodex is a Codex notify payload as the last argument.
	TurnCodex = "codex"
	// TurnAuto guesses: a JSON payload of either kind, else plain text,
	// from the last argument or standard input. For custom agents.
	TurnAuto = ""
)

// TurnHookArgv is the turn hook for a step: exe (tuckerbox) agent-turn
// with the step's state directory, step and round, and the payload
// format of the agent's hook style.
func TurnHookArgv(exe string, state statedir.Dir, step string, round int, style HookStyle) []string {
	argv := []string{exe, "agent-turn"}
	switch style {
	case HookClaude:
		argv = append(argv, "--claude")
	case HookCodex:
		argv = append(argv, "--codex")
	}
	return append(argv, "--state", string(state), "--step", step, "--round", strconv.Itoa(round))
}

// ClaudeHookSettings is the --settings JSON that installs hook as Claude
// Code's Stop hook for this session only.
func ClaudeHookSettings(hook []string) string {
	type cmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcher struct {
		Hooks []cmd `json:"hooks"`
	}
	v := map[string]map[string][]matcher{
		"hooks": {"Stop": {{Hooks: []cmd{{Type: "command", Command: ShellJoin(hook)}}}}},
	}
	return marshalNoEscape(v)
}

// CodexNotify is the -c value that makes Codex run hook after each turn:
// notify, a TOML array of strings. JSON strings are valid TOML basic
// strings when HTML characters are not escaped.
func CodexNotify(hook []string) string {
	if hook == nil {
		hook = []string{}
	}
	return "notify=" + marshalNoEscape(hook)
}

func marshalNoEscape(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}

// ShellJoin quotes argv as one POSIX shell command line.
func ShellJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:@,+%", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// maxTurnInput bounds what agent-turn reads from standard input.
const maxTurnInput = 4 << 20

// TurnMessage returns a turn's final message from a hook's input: args
// are agent-turn's positional arguments (Codex appends its payload),
// stdin its standard input. format is TurnClaude, TurnCodex or TurnAuto.
func TurnMessage(format string, args []string, stdin io.Reader) (string, error) {
	switch format {
	case TurnCodex:
		if len(args) == 0 {
			return "", errors.New("no notify payload: Codex passes it as the last argument")
		}
		return codexMessage([]byte(args[len(args)-1]))
	case TurnClaude:
		data, err := io.ReadAll(io.LimitReader(stdin, maxTurnInput))
		if err != nil {
			return "", err
		}
		return claudeMessage(data)
	case TurnAuto:
		if len(args) > 0 {
			last := []byte(args[len(args)-1])
			if msg, err := codexMessage(last); err == nil {
				return msg, nil
			}
			return strings.Join(args, " "), nil
		}
		data, err := io.ReadAll(io.LimitReader(stdin, maxTurnInput))
		if err != nil {
			return "", err
		}
		if msg, err := claudeMessage(data); err == nil {
			return msg, nil
		}
		return string(data), nil
	}
	return "", fmt.Errorf("unknown turn hook format %q", format)
}

// codexMessage reads a Codex notify payload's last-assistant-message.
func codexMessage(payload []byte) (string, error) {
	var p map[string]json.RawMessage
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", fmt.Errorf("notify payload is not a JSON object: %w", err)
	}
	raw, ok := p["last-assistant-message"]
	if !ok {
		return "", errors.New("notify payload has no last-assistant-message")
	}
	var msg *string
	if err := json.Unmarshal(raw, &msg); err != nil {
		return "", fmt.Errorf("last-assistant-message: %w", err)
	}
	if msg == nil {
		return "", nil
	}
	return *msg, nil
}

// claudeMessage reads a Claude Code Stop hook payload: the last assistant
// message of the transcript it names (or the payload's own
// last_assistant_message, which newer versions add).
func claudeMessage(payload []byte) (string, error) {
	var p struct {
		TranscriptPath       string  `json:"transcript_path"`
		LastAssistantMessage *string `json:"last_assistant_message"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", fmt.Errorf("Stop hook payload is not a JSON object: %w", err)
	}
	if p.LastAssistantMessage != nil && strings.TrimSpace(*p.LastAssistantMessage) != "" {
		return *p.LastAssistantMessage, nil
	}
	if p.TranscriptPath == "" {
		return "", errors.New("Stop hook payload names no transcript_path")
	}
	return TranscriptLastMessage(p.TranscriptPath)
}

// TranscriptLastMessage returns the text of the last assistant message in
// a Claude Code transcript (JSON lines). A message may be split over
// several lines with the same message id; their text blocks are joined.
// Lines of subagents (isSidechain) are skipped.
func TranscriptLastMessage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var curID string
	var cur []string
	found := false
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			id, texts, ok := transcriptTexts(line)
			if ok && len(texts) > 0 {
				if !found || id == "" || id != curID {
					cur = nil
				}
				curID, found = id, true
				cur = append(cur, texts...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if !found {
		return "", errors.New("the transcript has no assistant message")
	}
	return strings.Join(cur, "\n"), nil
}

// transcriptTexts returns an assistant line's message id and text blocks.
func transcriptTexts(line []byte) (id string, texts []string, ok bool) {
	var e struct {
		Type        string `json:"type"`
		IsSidechain bool   `json:"isSidechain"`
		Message     struct {
			ID      string          `json:"id"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.IsSidechain {
		return "", nil, false
	}
	var s string
	if json.Unmarshal(e.Message.Content, &s) == nil {
		return e.Message.ID, []string{s}, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return "", nil, false
	}
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	return e.Message.ID, texts, true
}

// WriteLastMessage writes a turn's final message to path through a
// temporary file and a rename, so a reader sees a whole message.
func WriteLastMessage(path, msg string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(msg); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
