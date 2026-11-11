# Agent notes

- Changes are planned with OpenSpec in `openspec/changes/<id>/`. Read the change's proposal, design and tasks before working on it.
- TLA+ models live in `formal/`. A model must pass TLC and each `*_broken_*` variant must be rejected. Never edit a model to make a check pass.
- Review rule: CI must be green; the repository owner merges pull requests. There is no AI reviewer or merge label on this repository.
- Don't commit machine-specific details (host names, home paths) or agent command copies (`.claude/`, `.agents/`, `.opencode/` are ignored).
