package herdrmod

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The person's keys.json and layout files are edited in place: keys keep
// their order, so the file reads as it did with one entry more. encoding/json
// maps would sort them.

// obj is a JSON object with its keys in file order.
type obj struct {
	keys []string
	vals map[string]any
}

func (o *obj) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

func (o *obj) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *obj) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

func newObj() *obj { return &obj{vals: map[string]any{}} }

// parseOrdered reads one JSON value: objects as *obj, arrays as []any,
// numbers as json.Number.
func parseOrdered(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, err := readValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("more than one JSON value")
	}
	return v, nil
}

func readValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := newObj()
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := readValue(d)
				if err != nil {
					return nil, err
				}
				o.set(k, v)
			}
			_, err := d.Token()
			return o, err
		case '[':
			a := []any{}
			for d.More() {
				v, err := readValue(d)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err := d.Token()
			return a, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return t, nil
	}
}

// encodeOrdered writes v with two-space indents and a final newline.
func encodeOrdered(v any) []byte {
	var b strings.Builder
	writeValue(&b, v, "")
	b.WriteByte('\n')
	return []byte(b.String())
}

func writeValue(b *strings.Builder, v any, indent string) {
	in := indent + "  "
	switch v := v.(type) {
	case *obj:
		if len(v.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range v.keys {
			b.WriteString(in)
			b.Write(scalar(k))
			b.WriteString(": ")
			writeValue(b, v.vals[k], in)
			if i < len(v.keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent + "}")
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, x := range v {
			b.WriteString(in)
			writeValue(b, x, in)
			if i < len(v)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent + "]")
	default:
		b.Write(scalar(v))
	}
}

func scalar(v any) []byte {
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}
