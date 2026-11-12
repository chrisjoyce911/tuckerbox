package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/BurntSushi/toml"
)

// CredentialsFile is the name of the machine's token store, beside machine
// config.toml. It holds [forges."<host>"] token = "...", and nothing else.
const CredentialsFile = "credentials.toml"

// CredentialsPath returns the credentials file beside the machine config
// file machinePath, or "" when machinePath is "".
func CredentialsPath(machinePath string) string {
	if machinePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(machinePath), CredentialsFile)
}

// credentials is the parsed credentials file. Its tokens never appear in a
// Problem, a Value or any other printable field.
type credentials struct {
	info     FileInfo
	tokens   map[string]string // by host
	problems Problems
}

// credentialsGOOS is the OS whose file mode rules apply; tests change it.
var credentialsGOOS = runtime.GOOS

// readCredentials reads the credentials file at path. A missing file is
// no tokens. On macOS and Linux a file group or others can read is refused
// without being read, as SSH refuses a private key. Problems never quote
// the file's contents.
func readCredentials(path string) (*credentials, error) {
	c := &credentials{info: FileInfo{Path: path}}
	if path == "" {
		return c, nil
	}
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	c.info.Exists = true
	if credentialsGOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		c.problems = append(c.problems, Problem{File: path, Message: fmt.Sprintf(
			"readable by group or others (mode %04o); run `chmod 600 %s`; no token from it is used", st.Mode().Perm(), path)})
		return c, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c.info.Revision = Revision(data)
	var raw struct {
		Forges map[string]struct {
			Token string `toml:"token"`
		} `toml:"forges"`
	}
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		// The decoder's message may quote the text near the error, which
		// may be a token: give the line only.
		p := Problem{File: path, Message: "not valid TOML; it holds only [forges.\"<host>\"] token = \"...\" tables"}
		var pe toml.ParseError
		if errors.As(err, &pe) {
			p.Line = pe.Position.Line
		}
		c.problems = append(c.problems, p)
		c.info.Error = &c.problems[0]
		return c, nil
	}
	for _, k := range md.Undecoded() {
		c.problems = append(c.problems, Problem{File: path, Key: k.String(), Message: "unknown key; the credentials file holds only [forges.\"<host>\"] token"})
	}
	c.tokens = map[string]string{}
	for _, host := range slices.Sorted(maps.Keys(raw.Forges)) {
		key := JoinKey([]string{"forges", host, "token"})
		if raw.Forges[host].Token == "" {
			c.problems = append(c.problems, Problem{File: path, Key: key, Message: "empty token; set one with `tuckerbox config set --machine " + key + "` or remove the table"})
			continue
		}
		c.tokens[host] = raw.Forges[host].Token
	}
	return c, nil
}
