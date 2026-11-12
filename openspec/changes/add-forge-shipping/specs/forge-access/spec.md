# Spec Delta

## Purpose

Defines how Tuckerbox finds a repository's forge (GitHub or GitLab) from its remote, reaches the forge's API, and where the API token comes from.

## ADDED Requirements

### Requirement: Forge from the remote URL
Tuckerbox SHALL find the forge from the push URL of the shipping remote (`origin` unless `[shipping] remote` names another). It SHALL read HTTPS, SSH and `user@host:path` URLs, and resolve SSH host aliases. Host `github.com` SHALL mean GitHub and `gitlab.com` GitLab. The project is the URL's path without `.git`, including GitLab subgroups.

#### Scenario: GitHub over SSH
- **WHEN** the remote is `git@github.com:sam/app.git`
- **THEN** the forge is GitHub, project `sam/app`, API `https://api.github.com`

#### Scenario: GitLab subgroup
- **WHEN** the remote is `https://gitlab.com/team/tools/app.git`
- **THEN** the forge is GitLab, project `team/tools/app`, API `https://gitlab.com/api/v4`

### Requirement: Other forge hosts
For any other host, repository config `[forge] kind` (`github` or `gitlab`) or machine config `[forges."<host>"] kind` SHALL name the forge. The API URL SHALL default to `https://<host>/api/v4` for GitLab and `https://<host>/api/v3` for GitHub. `[forge] api_url` MAY change it, but only to a URL on the remote's own host. Unknown hosts SHALL refuse to ship.

#### Scenario: Self-hosted GitLab
- **WHEN** the remote's host is `git.example.org` and repository config sets `[forge] kind = "gitlab"`
- **THEN** Tuckerbox uses the GitLab API at `https://git.example.org/api/v4`

#### Scenario: Unknown host
- **WHEN** the remote's host is not `github.com` or `gitlab.com` and no config names its kind
- **THEN** `tuckerbox check` reports it, and a run in `mr` or `merge` mode refuses to start and names `[forge] kind`

#### Scenario: API URL on another host
- **WHEN** repository config sets `[forge] api_url` to a host other than the remote's
- **THEN** `tuckerbox check` fails and no request is sent to that host

### Requirement: Token sources
Tuckerbox SHALL take a forge token, in order, from: the variable named by machine config `[forges."<host>"] token_env`; then `GH_TOKEN` or `GITHUB_TOKEN` for `github.com`, `GITLAB_TOKEN` for `gitlab.com`; then, if `borrow_cli_token = true` for that host, the token `gh` or `glab` stores. A default variable SHALL NOT be sent to any other host.

#### Scenario: Default variable stays on its host
- **WHEN** `GITLAB_TOKEN` is set and the remote is on `git.example.org` with no `token_env` for that host
- **THEN** Tuckerbox finds no token and does not send `GITLAB_TOKEN` there

#### Scenario: Named variable
- **WHEN** machine config sets `[forges."git.example.org"] token_env = "WORK_GITLAB_TOKEN"`
- **THEN** Tuckerbox sends that variable's value to `git.example.org` only

### Requirement: Borrowing a CLI token
With `borrow_cli_token = true` for a host, and no token found before it, Tuckerbox SHALL ask `gh auth token --hostname <host>` (GitHub) or `glab config get token --host <host>` (GitLab) for the stored token. It SHALL only read: it never logs in, refreshes or writes their config. It SHALL NOT use `gh` or `glab` for anything else. The setting SHALL default to `false`.

#### Scenario: Borrow from gh
- **WHEN** `borrow_cli_token = true` for `github.com`, no variable is set, and `gh` holds a token
- **THEN** Tuckerbox uses that token and `doctor` reports its source as `gh`

#### Scenario: Borrowing off
- **WHEN** `borrow_cli_token` is not set and no variable is set
- **THEN** Tuckerbox does not run `gh` or `glab`, and `doctor` says a `gh` token exists and how to allow it

### Requirement: Tokens are never shown or passed on
Tuckerbox SHALL NOT print, log or record a token, in output, logs, briefs, history or run state, and SHALL mask it if it appears in text it saves, such as a CI log. It SHALL NOT add a token it read to an agent's environment. `git push` SHALL use git's own credentials, not the API token.

#### Scenario: Token in a job log
- **WHEN** a failed job's log contains the token Tuckerbox uses for that forge
- **THEN** the saved log and the fix step's brief show it masked

#### Scenario: Agent environment
- **WHEN** the token came from `token_env` or `gh`, and a fix step starts
- **THEN** the agent's environment holds no variable Tuckerbox added for that token

### Requirement: Checking forge access
`tuckerbox doctor`, run in a repository with `ship = "mr"` or `"merge"`, SHALL report the forge, the project, the token's source (never the token), and whether the token can read the project and push to it. A missing or rejected token SHALL be reported with the keys that would supply one.

#### Scenario: Token rejected
- **WHEN** the forge answers 401 to the token from `GITHUB_TOKEN`
- **THEN** `doctor` says the `GITHUB_TOKEN` token was rejected for `github.com`
