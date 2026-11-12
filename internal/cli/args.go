package cli

import (
	"fmt"
	"strings"
)

// parseArgs splits args into flags and positional arguments. Flags may come
// before, between or after the positional ones, as --name, -name,
// --name=value or --name value; "--" ends the flags.
func parseArgs(args []string, bools map[string]*bool, values map[string]*string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if b, ok := bools[name]; ok {
			if hasVal {
				return nil, fmt.Errorf("flag --%s takes no value", name)
			}
			*b = true
			continue
		}
		if v, ok := values[name]; ok {
			if !hasVal {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag --%s needs a value", name)
				}
				i++
				val = args[i]
			}
			*v = val
			continue
		}
		return nil, fmt.Errorf("unknown flag %s", a)
	}
	return pos, nil
}
