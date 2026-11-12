# Agent notes

- Changes are planned with OpenSpec in `openspec/changes/<id>/`. Read the change's proposal, design and tasks before working on it.
- TLA+ models live in `formal/`. A model must pass TLC and each `*_broken_*` variant must be rejected. Never edit a model to make a check pass.
- Review rule: a pull request merges when CI is green and an independent review by a different agent model (Codex reviews Claude's work, Claude reviews Codex's) has no open findings. The coordinating agent merges it; the owner does not need to. There is no merge label.
- The owner's gate is approving an OpenSpec change (`tuckerbox approve`, or Approve on the board). After approval, building, review, shipping and archiving should need no human unless a run is blocked on a decision only the owner can make.
- Don't commit machine-specific details (host names, home paths) or agent command copies (`.claude/`, `.agents/`, `.opencode/` are ignored).
- Before pushing, run `go vet ./...`, `go test -race ./...` and `scripts/tla-check.sh` (the same checks CI runs). The shipped binary is built with `CGO_ENABLED=0`; cgo is only for the race detector.
