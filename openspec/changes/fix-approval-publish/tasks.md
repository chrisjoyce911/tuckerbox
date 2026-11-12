# Tasks

## 1. Approvals ref (approval-publish)

- [x] 1.1 Read and write `refs/tuckerbox/approvals` in `internal/approval`: fetch by name, `approvals/<change>.toml` (state, hash, settings_hash, by, at), one commit per operation with the previous as parent; tested against a local bare remote
- [x] 1.2 `approve` and `unapprove` in solo and fleet mode write the ref under the change lock and no longer write `tuckerbox.toml`
- [x] 1.3 Test "Approve from the board" (checkout unchanged, ref has the file)
- [x] 1.4 Push with `--force-with-lease` on the fetched version; on refusal fetch, re-apply the one file and push again, up to 5 tries with jitter
- [x] 1.5 Test "Two approvals at once" (two clones of one bare remote; both approvals on the ref)
- [x] 1.6 Run TLC on `formal/approvals.tla` and its broken variant (`scripts/tla-check.sh` locally, the `tla` job in CI): the model passes, the variant is rejected

## 2. Reading (approval-publish)

- [x] 2.1 Run gate and `board --json` read the fetched ref (board at most once a minute, cached); the gate fetches again under the change lock; ref entry wins, else the sidecar `[approval]`; a withdrawal is `none`
- [x] 2.2 Test "Approved on one machine, run on another"
- [x] 2.3 Test "Old approval in the sidecar"
- [x] 2.4 Test "Unapproved after an old approval"
- [x] 2.5 Test "Edited after approval" (hash compared with the files the run will use)
- [x] 2.6 `tuckerbox check` lists changes approved on the ref and in the sidecar with different hashes

## 3. Offline and cards (approval-publish)

- [x] 3.1 Keep an unpublished commit on the local ref when the remote can't be reached; list unpublished changes in the state directory; publish on the next approve, unapprove or board fetch by re-applying onto the remote's version
- [x] 3.2 Test "Approve on a train" (remote unreachable, then reachable)
- [x] 3.3 Card approval `published`; no publishing state without a remote; board mod shows `not published`
- [x] 3.4 Test "Card after publishing"
- [x] 3.5 `doctor` checks the approvals ref can be fetched and pushed

## 4. Team mode (approval-publish)

- [ ] 4.1 In team mode `approve` and `unapprove` write `tuckerbox.toml` as before and the gate ignores the ref
- [ ] 4.2 Test "Ref entry in a team repository"

## 5. Integration

- [x] 5.1 README: approving publishes at once; offline note; team mode
- [x] 5.2 `go vet ./...`, `go test -race ./...`, `node --test herdr/mods/tuckerbox-board/test/*.test.js`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Fold `approval-publish` into `change-approval` and `board-data` once `add-herdr-board` is archived.
- Archive this change after the implementation is accepted.
