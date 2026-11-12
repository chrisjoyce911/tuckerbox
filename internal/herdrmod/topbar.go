package herdrmod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const BarID = "tuckerbox-bar"
const PanelID = "tuckerbox-panel"

var errNoTabs = errors.New("no tabs block in a row or column; layout unchanged; add the printed block by hand")

func boardBlock(id, view string) *obj {
	p := newObj()
	p.set("view", view)
	if view == "bar" {
		p.set("block", id)
	}
	c := newObj()
	c.set("mod", "tuckerbox-board")
	c.set("props", p)
	b := newObj()
	b.set("id", id)
	b.set("type", "block")
	b.set("content", c)
	if view == "bar" {
		z := newObj()
		z.set("px", 0)
		b.set("size", z)
	}
	return b
}

func findBlock(o *obj, id string) *obj {
	if o.vals["id"] == id {
		return o
	}
	for _, key := range []string{"children", "floating"} {
		cs, _ := o.vals[key].([]any)
		for _, c := range cs {
			if b, ok := c.(*obj); ok {
				if hit := findBlock(b, id); hit != nil {
					return hit
				}
			}
		}
	}
	return nil
}

func addBar(o *obj) bool {
	cs, _ := o.vals["children"].([]any)
	for i, c := range cs {
		b, ok := c.(*obj)
		if !ok {
			continue
		}
		if b.vals["id"] == "tabs" && b.vals["type"] == "block" {
			switch o.vals["type"] {
			case "row":
				next := append([]any{}, cs[:i+1]...)
				next = append(next, boardBlock(BarID, "bar"))
				next = append(next, cs[i+1:]...)
				o.set("children", next)
				return true
			case "column":
				row := newObj()
				row.set("id", "tuckerbox-topbar")
				row.set("type", "row")
				size := newObj()
				size.set("px", 42)
				row.set("size", size)
				weight := newObj()
				weight.set("weight", 1)
				b.set("size", weight)
				row.set("children", []any{b, boardBlock(BarID, "bar")})
				cs[i] = row
				return true
			}
		}
		if addBar(b) {
			return true
		}
	}
	return false
}

func installTopbar(home string, rec *Record, t *txn) (string, error) {
	name := shownLayout(home)
	if rec.Drawer != nil {
		name = rec.Drawer.Layout
	}
	before, root, err := readLayout(layoutPath(home, name))
	if errors.Is(err, fs.ErrNotExist) && rec.Drawer != nil {
		// A person may delete an installed clone. There is nothing left to
		// undo there, so rebuild the record from the currently shown layout.
		shown := shownLayout(home)
		if shown == rec.Drawer.Layout && rec.Drawer.ClonedFrom != "" {
			shown = rec.Drawer.ClonedFrom
		}
		rec.Drawer = nil
		name = shown
		before, root, err = readLayout(layoutPath(home, name))
	}
	if err != nil {
		return "", err
	}
	if findBlock(root, BarID) != nil {
		return "the top bar is already in layout " + name, nil
	}
	if !addBar(root) {
		return string(encodeOrdered(boardBlock(BarID, "bar"))), errNoTabs
	}
	d := rec.Drawer
	if d == nil {
		d = &DrawerRecord{Layout: name, Before: string(before)}
		if shippedLayout(home, name) {
			as := name + "-tuckerbox"
			for i := 2; ; i++ {
				if _, e := os.Stat(layoutPath(home, as)); errors.Is(e, fs.ErrNotExist) {
					break
				}
				as = fmt.Sprintf("%s-tuckerbox-%d", name, i)
			}
			if err := t.clone(name, as); err != nil {
				return "", err
			}
			d.Layout, d.ClonedFrom = as, name
			cloned, cloneRoot, err := readLayout(layoutPath(home, as))
			if err != nil {
				return "", err
			}
			if !addBar(cloneRoot) {
				return "", errors.New("cloned layout has no supported tabs block")
			}
			root = cloneRoot
			d.Before = string(cloned)
		}
	} else {
		if len(d.Blocks) == 0 {
			d.Blocks = []string{DrawerID}
		}
		if sum(before) != d.After {
			base, e := parseOrdered(before)
			if e != nil {
				return "", e
			}
			removeLayoutBlocks(base.(*obj), d.Blocks)
			d.Before = string(encodeOrdered(base))
		}
	}
	msg := "added the top bar to layout " + d.Layout
	blocks := []string{BarID}
	if stack := findBlock(root, "panel-body"); stack != nil && stack.vals["type"] == "stack" {
		if findBlock(root, PanelID) == nil {
			cs, _ := stack.vals["children"].([]any)
			stack.set("children", append(cs, boardBlock(PanelID, "panel")))
			blocks = append(blocks, PanelID)
		}
	} else {
		msg += "; no panel-body stack: skipped side panel"
	}
	after := encodeOrdered(root)
	if err := t.write("layouts/"+d.Layout+".json", after); err != nil {
		return "", err
	}
	d.After = sum(after)
	d.Blocks = append(d.Blocks, blocks...)
	rec.Drawer = d
	return msg, nil
}

// Remove only recorded blocks. Keep containers in edited layouts: their
// settings and any siblings may belong to the person. Unchanged layouts
// restore their original bytes instead.
func removeLayoutBlocks(o *obj, ids []string) {
	for _, key := range []string{"children", "floating"} {
		cs, ok := o.vals[key].([]any)
		if !ok {
			continue
		}
		keep := []any{}
		for _, c := range cs {
			b, ok := c.(*obj)
			if !ok {
				keep = append(keep, c)
				continue
			}
			remove := false
			for _, id := range ids {
				if b.vals["id"] == id {
					remove = true
				}
			}
			if remove {
				continue
			}
			removeLayoutBlocks(b, ids)
			keep = append(keep, b)
		}
		if key == "floating" && len(keep) == 0 {
			o.del(key)
		} else {
			o.set(key, keep)
		}
	}
}
