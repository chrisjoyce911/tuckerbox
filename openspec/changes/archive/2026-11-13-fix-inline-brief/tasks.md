# Tasks

## 1. Placeholders (agent-launch: Agents by command template)

- [x] 1.1 Add `{brief_file}` (path) and make `{brief}` the brief's text in `internal/agent` and the config placeholder list; drop `{prompt}`; built-in Claude and Codex pass `{brief}`; `tuckerbox check` requires `{brief}` or `{brief_file}` in a custom agent; update the starter config comment and the README example to `{brief_file}`
- [x] 1.2 Test "Custom agent"
- [x] 1.3 Test "Custom agent that wants a file" (no brief text in the command)
- [x] 1.4 Test "Agent with neither"

## 2. Brief inline (agent-launch: Step brief)

- [x] 2.1 Expand `{brief}` to the rendered brief; keep writing the brief file and `TUCKERBOX_BRIEF`
- [x] 2.2 Test "Build after rejection"
- [x] 2.3 Test "Agent reads no file" (fake agent that asserts it got the whole text and does not open the state folder)

## 3. Long briefs (agent-launch: Long briefs)

- [x] 3.1 Measure the expanded command as a Windows command line; above 24,000 characters send the brief on standard input (Claude: drop the argument; Codex: `-`) from a goroutine, then close it; custom agents get the read-the-file instruction and a log note
- [x] 3.2 Test "Long brief to Claude" (fake `claude` asserts no prompt argument and the whole brief on stdin)
- [x] 3.3 Test "Long brief to Codex" (fake `codex` asserts `-` and the whole brief on stdin)
- [x] 3.4 Test "Long brief to a custom agent"
- [x] 3.5 Test "Windows command line limit" (runs on the Windows CI job with a 40,000-character brief)

## 4. Logs (agent-launch: Brief text kept out of logs)

- [x] 4.1 Show the brief argument as `<brief: N chars, <file>>` in the step log's start line and in `Outcome.Argv`
- [x] 4.2 Test "Log line"
