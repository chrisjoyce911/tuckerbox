package agent

import "unicode/utf16"

// MaxInlineCommand is the longest command line, measured as Windows would
// quote it, that may carry the brief inline. Windows allows 32,767 UTF-16
// characters; the rest leaves room for the program's full path and flags a
// permission profile adds. The same limit applies on every platform, so a
// brief that works on one works on all.
const MaxInlineCommand = 24000

// windowsCmdLineLen returns the length, in UTF-16 code units, of argv joined
// into one Windows command line with the quoting rules of syscall.EscapeArg
// (which os/exec uses on Windows). It is the same code on every platform.
func windowsCmdLineLen(argv []string) int {
	n := 0
	for i, a := range argv {
		if i > 0 {
			n++
		}
		n += utf16Len(windowsEscapeArg(a))
	}
	return n
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// windowsEscapeArg quotes s as syscall.EscapeArg does on Windows.
func windowsEscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	needsBackslash, hasSpace := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			needsBackslash = true
		case ' ', '\t':
			hasSpace = true
		}
	}
	if !needsBackslash && !hasSpace {
		return s
	}
	if !needsBackslash {
		return `"` + s + `"`
	}
	b := make([]byte, 0, len(s)+8)
	if hasSpace {
		b = append(b, '"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		default:
			slashes = 0
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b = append(b, '\\')
			}
			b = append(b, '\\')
		}
		b = append(b, c)
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			b = append(b, '\\')
		}
		b = append(b, '"')
	}
	return string(b)
}
