package forge

import (
	"bytes"
	"io"
)

// Log keeps a bounded tail while retaining the original omitted-line count.
// Downloads are streamed so even a very large CI log cannot exhaust memory.
type Log struct {
	Text         string
	OmittedLines int
	Truncated    bool
}

func readLog(reader io.Reader) (Log, error) {
	const limit = 256 << 10
	tail := make([]byte, 0, limit)
	chunk := make([]byte, 32<<10)
	total, newlines := 0, 0
	last := byte(0)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			data := chunk[:n]
			total += n
			newlines += bytes.Count(data, []byte{'\n'})
			last = data[n-1]
			if drop := len(tail) + n - limit; drop > 0 {
				copy(tail, tail[drop:])
				tail = tail[:len(tail)-drop]
			}
			tail = append(tail, data...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return Log{}, err
		}
	}
	truncated := total > len(tail)
	if truncated {
		if first := bytes.IndexByte(tail, '\n'); first >= 0 {
			tail = tail[first+1:]
		}
	}
	lines := newlines
	if total > 0 && last != '\n' {
		lines++
	}
	kept := bytes.Count(tail, []byte{'\n'})
	if len(tail) > 0 && tail[len(tail)-1] != '\n' {
		kept++
	}
	return Log{Text: string(tail), OmittedLines: max(0, lines-kept), Truncated: truncated}, nil
}
