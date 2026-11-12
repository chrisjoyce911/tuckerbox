# Spec Delta

## Purpose

Defines how `tuckerbox herdr install` and `tuckerbox herdr uninstall` keep the person's Herdr files and the install record in step when two run at once, when a step fails part way, and when install runs again with another key.

## ADDED Requirements

### Requirement: One install at a time per Herdr home
`tuckerbox herdr install` and `uninstall` SHALL hold an exclusive OS file lock on `.tuckerbox-install.lock` in the Herdr home from before they read the install record until after they write it. A second one SHALL wait up to 30 seconds, then exit non-zero saying another install or uninstall is running, and change nothing. A process that dies SHALL release the lock.

#### Scenario: Keys and drawer installed at once
- **WHEN** `tuckerbox herdr install --keys` and `tuckerbox herdr install --drawer` run at the same time in one Herdr home
- **THEN** both succeed one after the other, the record holds both the key and the drawer, and `tuckerbox herdr uninstall` then removes both

#### Scenario: Lock held too long
- **WHEN** an install holds the lock for more than 30 seconds and a second install starts
- **THEN** the second exits non-zero, says another install or uninstall is running, and changes no file

### Requirement: Failed record write rolls back
When writing the install record fails, install SHALL put every file it changed in that run back to its bytes from before the run, remove a layout it cloned in that run, show the layout that was shown before, keep the old record, and exit non-zero naming the record file.

#### Scenario: Record write fails after the key
- **WHEN** `tuckerbox herdr install --keys` has changed `keys.json` and replacing `.tuckerbox-install.json` then fails
- **THEN** `keys.json` is byte for byte as before the run, the record is as before, and install exits non-zero naming the record file

### Requirement: Failed drawer step after a clone
When the drawer step fails after `layout.clone` made and showed a clone, install SHALL show the layout that was shown before, remove the clone, record no drawer, and exit non-zero. If that clean-up fails, it SHALL record the clone so `uninstall` can remove it, and say so.

#### Scenario: Clone unreadable
- **WHEN** install clones `main` to `main-tuckerbox` and then can't parse `layouts/main-tuckerbox.json`
- **THEN** `main` is shown again, `layouts/main-tuckerbox.json` is gone, the record has no drawer, and install exits non-zero

#### Scenario: Clean-up fails
- **WHEN** the drawer step fails after cloning and removing the clone also fails
- **THEN** the record holds the clone, the output says to run `tuckerbox herdr uninstall`, and install exits non-zero

### Requirement: Re-install with another key
When the record holds a toggle key and install runs with `--keys --key <key>` naming a different key, install SHALL check the new key for clashes as on a first install. If it is free, it SHALL remove the old binding, add the new one, record the new key, and keep the record's `keys.json` from before the first install. On a clash it SHALL change nothing and exit non-zero.

#### Scenario: New key
- **WHEN** install bound `cmd-shift-k` and the person runs `tuckerbox herdr install --keys --key f8` with `f8` free
- **THEN** `keys.json` binds `f8` to the board and no longer binds `cmd-shift-k` to it, and the record names `f8`

#### Scenario: New key clashes
- **WHEN** install bound `cmd-shift-k` and `f8` is already bound to `app.palette`
- **THEN** `tuckerbox herdr install --keys --key f8` leaves `keys.json` and the record unchanged, reports the clash and exits non-zero

#### Scenario: Uninstall after changing the key
- **WHEN** the person installed with `--keys`, re-installed with `--keys --key f8`, and changed nothing else
- **THEN** `tuckerbox herdr uninstall` leaves `keys.json` byte for byte as before the first install
