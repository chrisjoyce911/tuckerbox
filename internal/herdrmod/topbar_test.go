package herdrmod

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func topbarHome(t *testing.T) (string, Env, *fakeHerdr) {
	t.Helper()
	home := herdrHome(t)
	b, err := os.ReadFile("testdata/main.json")
	if err != nil {
		t.Fatal(err)
	}
	write(t, home, "layouts/main.json", string(b))
	// Use the fake window's canonical shared.json formatting for snapshots.
	write(t, home, "shared.json", `{"layout":"main"}`)
	e, f := extrasEnv(t, home, "linux")
	return home, e, f
}

func TestTopbarShippedLayoutAndPanel(t *testing.T) {
	home, e, f := topbarHome(t)
	before := read(t, home, "layouts/main.json")
	if _, err := InstallExtras(context.Background(), e, Options{Topbar: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, home, "layouts/main.json"); got != before {
		t.Fatal("shipped layout changed")
	}
	if shownLayout(home) != "main-tuckerbox" {
		t.Fatal("clone not shown")
	}
	_, root, err := readLayout(layoutPath(home, "main-tuckerbox"))
	if err != nil {
		t.Fatal(err)
	}
	row := findBlock(root, "tuckerbox-topbar")
	if row == nil {
		t.Fatal("no row")
	}
	cs := row.vals["children"].([]any)
	if cs[0].(*obj).vals["id"] != "tabs" || cs[1].(*obj).vals["id"] != BarID {
		t.Fatal("bar not after tabs")
	}
	stack := findBlock(root, "panel-body").vals["children"].([]any)
	var ids []string
	for _, c := range stack {
		ids = append(ids, c.(*obj).vals["id"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"files", "changes", PanelID}) {
		t.Fatal(ids)
	}
	if len(f.calls) != 1 {
		t.Fatal(f.calls)
	}
	r := readRec(t, home)
	if !reflect.DeepEqual(r.Drawer.Blocks, []string{BarID, PanelID}) {
		t.Fatal(r.Drawer)
	}
}

func TestTopbarNoTabs(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "linux")
	before := snapshot(t, home)
	msg, err := InstallExtras(context.Background(), e, Options{Topbar: true})
	if !errors.Is(err, errNoTabs) || !strings.Contains(strings.Join(msg, ""), `"view": "bar"`) {
		t.Fatalf("%v %v", msg, err)
	}
	if len(f.calls) != 0 || read(t, home, "layouts/main.json") != before["layouts/main.json"] {
		t.Fatal("layout changed")
	}
}

func TestTopbarDrawerUninstallRoundTrip(t *testing.T) {
	for _, order := range []string{"together", "bar-first", "drawer-first"} {
		t.Run(order, func(t *testing.T) {
			home, e, f := topbarHome(t)
			before := snapshot(t, home)
			ctx := context.Background()
			var err error
			if order == "together" {
				_, err = InstallExtras(ctx, e, Options{Topbar: true, Drawer: true})
			} else {
				_, err = InstallExtras(ctx, e, Options{Topbar: order == "bar-first", Drawer: order == "drawer-first"})
				if err == nil {
					_, err = InstallExtras(ctx, e, Options{Topbar: order == "drawer-first", Drawer: order == "bar-first"})
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != 1 {
				t.Fatalf("more than one clone: %v", f.calls)
			}
			if _, err = Uninstall(ctx, e); err != nil {
				t.Fatal(err)
			}
			if got := snapshot(t, home); !reflect.DeepEqual(got, before) {
				t.Fatalf("roundtrip changed home: %v", got)
			}
		})
	}
}

func TestTopbarRowAndMissingPanel(t *testing.T) {
	home, e, _ := topbarHome(t)
	write(t, home, "shared.json", `{"layout":"mine"}`)
	write(t, home, "layouts/mine.json", `{"id":"root","type":"row","children":[{"id":"tabs","type":"block"},{"id":"clock","type":"block"}]}`)
	msg, err := InstallExtras(context.Background(), e, Options{Topbar: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(msg, ""), "skipped side panel") {
		t.Fatal(msg)
	}
	_, root, _ := readLayout(layoutPath(home, "mine"))
	cs := root.vals["children"].([]any)
	if len(cs) != 3 || cs[1].(*obj).vals["id"] != BarID {
		t.Fatal("not after tabs")
	}
}

func TestTopbarUninstallKeepsLayoutEdits(t *testing.T) {
	home, e, _ := topbarHome(t)
	ctx := context.Background()
	if _, err := InstallExtras(ctx, e, Options{Topbar: true, Drawer: true}); err != nil {
		t.Fatal(err)
	}
	_, root, _ := readLayout(layoutPath(home, "main-tuckerbox"))
	root.set("background", "custom")
	if err := os.WriteFile(layoutPath(home, "main-tuckerbox"), encodeOrdered(root), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(ctx, e); err != nil {
		t.Fatal(err)
	}
	_, root, err := readLayout(layoutPath(home, "main-tuckerbox"))
	if err != nil {
		t.Fatal(err)
	}
	if root.vals["background"] != "custom" {
		t.Fatal("lost edit")
	}
	for _, id := range []string{BarID, PanelID, DrawerID} {
		if findBlock(root, id) != nil {
			t.Fatalf("left %s", id)
		}
	}
	if findBlock(root, "tabs") == nil {
		t.Fatal("lost tabs")
	}
}

func TestTopbarCloneFailureRollsBack(t *testing.T) {
	home, e, f := topbarHome(t)
	before := snapshot(t, home)
	f.cloneText = "{ not json"
	if _, err := InstallExtras(context.Background(), e, Options{Topbar: true}); err == nil {
		t.Fatal("accepted malformed clone")
	}
	if got := snapshot(t, home); !reflect.DeepEqual(got, before) {
		t.Fatalf("not restored: %v", got)
	}
}

func TestTopbarPreservesCloneMetadata(t *testing.T) {
	home, e, f := topbarHome(t)
	_, root, _ := readLayout(layoutPath(home, "main"))
	root.set("cloned_from", "main")
	f.cloneText = string(encodeOrdered(root))
	if _, err := InstallExtras(context.Background(), e, Options{Topbar: true}); err != nil {
		t.Fatal(err)
	}
	_, root, _ = readLayout(layoutPath(home, "main-tuckerbox"))
	if root.vals["cloned_from"] != "main" {
		t.Fatal("lost clone metadata")
	}
}

func TestTopbarExtendsLegacyDrawerRecord(t *testing.T) {
	home, e, _ := topbarHome(t)
	ctx := context.Background()
	if _, err := InstallExtras(ctx, e, Options{Drawer: true}); err != nil {
		t.Fatal(err)
	}
	rec := readRec(t, home)
	rec.Drawer.Blocks = nil
	if err := writeRecord(home, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallExtras(ctx, e, Options{Topbar: true}); err != nil {
		t.Fatal(err)
	}
	rec = readRec(t, home)
	if !reflect.DeepEqual(rec.Drawer.Blocks, []string{DrawerID, BarID, PanelID}) {
		t.Fatal(rec.Drawer.Blocks)
	}
}
