// Package herdr holds the Herdr app mod Tuckerbox ships: the board, in
// mods/tuckerbox-board. Only the files the mod needs at run time are
// embedded: no tests, package.json, local dev.json or files herdr-app check
// writes (gpui-kit.d.ts, jsconfig.json).
package herdr

import "embed"

// ModName is the mod's folder name under the Herdr app's mods folder.
const ModName = "tuckerbox-board"

// Mod is the board mod; its files are under mods/tuckerbox-board.
//
//go:embed mods/tuckerbox-board/*.js mods/tuckerbox-board/mod.json mods/tuckerbox-board/keys.json mods/tuckerbox-board/*.svg mods/tuckerbox-board/README.md mods/tuckerbox-board/fixtures/*.json
var Mod embed.FS

// ModRoot is the folder in Mod that holds the mod's files.
const ModRoot = "mods/" + ModName
