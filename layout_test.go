package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCraft-MC/gocraft-abi/gcpkg"
)

// §10's purchase event, as a compiler would have dumped it.
func publishedLayouts() eventLayouts {
	return eventLayouts{
		Version: layoutVersion,
		Types: []layoutRecord{{Name: "fr.oreo.Tier", Fields: []layoutField{
			{Name: "label", Type: "string"},
			{Name: "price", Type: "double", Mutable: true},
		}}},
		Events: []layoutEvent{{Type: "fr.oreo.shop/purchase", Cancellable: true,
			Fields: []layoutField{
				{Name: "buyer", Type: "PlayerRef"},
				{Name: "tiers", Type: "[]fr.oreo.Tier"},
				{Name: "price", Type: "double", Mutable: true},
			}}},
	}
}

// Appending is the one change that breaks nobody: every index that existed
// still means what it meant, and a subscriber compiled against the shorter
// layout simply never reads the new one.
func TestAnAppendedFieldIsAllowed(t *testing.T) {
	current := publishedLayouts()
	current.Events[0].Fields = append(current.Events[0].Fields,
		layoutField{Name: "currency", Type: "string"})

	if err := checkLayoutDrift(publishedLayouts(), current); err != nil {
		t.Fatalf("checkLayoutDrift() = %v, want an append to be allowed", err)
	}
}

// The failure this whole file exists for. It is silent on the author's machine,
// because their own build regenerates both sides; it is the subscriber compiled
// last month that reads the price out of the field holding the name.
func TestAReorderedFieldIsRefused(t *testing.T) {
	current := publishedLayouts()
	current.Events[0].Fields[1], current.Events[0].Fields[2] =
		current.Events[0].Fields[2], current.Events[0].Fields[1]

	err := checkLayoutDrift(publishedLayouts(), current)
	if err == nil {
		t.Fatal("checkLayoutDrift() accepted two fields swapped")
	}
	if !strings.Contains(err.Error(), "field 1") {
		t.Fatalf("checkLayoutDrift() = %v, want the index named", err)
	}
}

func TestARemovedFieldIsRefused(t *testing.T) {
	current := publishedLayouts()
	current.Events[0].Fields = current.Events[0].Fields[:2]

	if err := checkLayoutDrift(publishedLayouts(), current); err == nil {
		t.Fatal("checkLayoutDrift() accepted a field being removed")
	}
}

// A whole event, and a whole record, break the same subscribers the same way.
func TestARemovedEventOrRecordIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current eventLayouts
	}{
		{"event", eventLayouts{Version: layoutVersion, Types: publishedLayouts().Types}},
		{"record", eventLayouts{Version: layoutVersion, Events: publishedLayouts().Events}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkLayoutDrift(publishedLayouts(), tc.current); err == nil {
				t.Fatalf("checkLayoutDrift() accepted a %s disappearing", tc.name)
			}
		})
	}
}

// The name never crosses the wire, so renaming a field in place breaks nobody
// and refusing it would be refusing something harmless.
func TestARenamedFieldIsAllowed(t *testing.T) {
	current := publishedLayouts()
	current.Events[0].Fields[2].Name = "total"

	if err := checkLayoutDrift(publishedLayouts(), current); err != nil {
		t.Fatalf("checkLayoutDrift() = %v, want a rename to be allowed", err)
	}
}

// Turning a mutable field final is not a reorder, but it breaks a subscriber in
// the same quiet way: the host starts refusing a write that used to land.
func TestAFieldTurnedFinalIsRefused(t *testing.T) {
	current := publishedLayouts()
	current.Events[0].Fields[2].Mutable = false

	if err := checkLayoutDrift(publishedLayouts(), current); err == nil {
		t.Fatal("checkLayoutDrift() accepted a mutable field becoming final")
	}
}

// A record inside an event is a layout too, one level down.
func TestAReorderedRecordIsRefused(t *testing.T) {
	current := publishedLayouts()
	current.Types[0].Fields[0], current.Types[0].Fields[1] =
		current.Types[0].Fields[1], current.Types[0].Fields[0]

	if err := checkLayoutDrift(publishedLayouts(), current); err == nil {
		t.Fatal("checkLayoutDrift() accepted a record's fields being swapped")
	}
}

// A layout declared by hand is a layout, and it is locked like any other.
//
// The manifest block and a compiler dump say the same four things about a
// field, so the conversion is checked for saying them rather than for its
// shape: what a Go plugin writes in plugin.toml has to arrive in the record the
// next build compares against.
func TestAManifestDeclaresALayout(t *testing.T) {
	layouts := manifestLayouts(gcpkg.Manifest{
		Records: []gcpkg.EventRecord{{Name: "fr.oreo.Tier", Fields: []gcpkg.EventField{
			{Name: "label", Type: "string"},
			{Name: "price", Type: "double", Mutable: true},
		}}},
		Provides: []gcpkg.EventDefinition{{
			Type: "fr.oreo.shop/purchase", Cancellable: true,
			Fields: []gcpkg.EventField{
				{Name: "buyer", Type: "PlayerRef"},
				{Name: "tiers", Type: "[]fr.oreo.Tier"},
				{Name: "price", Type: "double", Mutable: true},
			},
		}},
	})
	if err := checkLayoutDrift(publishedLayouts(), layouts); err != nil {
		t.Fatalf("manifestLayouts() did not reproduce the same layout: %v", err)
	}
	if err := checkLayoutDrift(layouts, publishedLayouts()); err != nil {
		t.Fatalf("manifestLayouts() did not reproduce the same layout: %v", err)
	}
}

// The hole this fix closes, end to end and in the direction it was open.
//
// A Go plugin has no annotation processor, so it declares its events in
// plugin.toml and the build was handed no dump. Every check below therefore did
// nothing at all, and swapping two lines of that block shipped — while the same
// swap in an annotated Java class was refused. It is the half where a swap is
// easiest and where nothing recompiles to notice.
func TestAHandDeclaredLayoutIsRefusedWhenItReorders(t *testing.T) {
	const header = `id = "fr.oreo.shop"
version = "1.0.0"
api = 1
runtime = "go"
entry = "bin/shop"
`
	const published = header + `
[[events.provides]]
type = "fr.oreo.shop/purchase"
fields = [
  { name = "buyer", type = "PlayerRef" },
  { name = "price", type = "double", mutable = true },
]
`
	const reordered = header + `
[[events.provides]]
type = "fr.oreo.shop/purchase"
fields = [
  { name = "price", type = "double", mutable = true },
  { name = "buyer", type = "PlayerRef" },
]
`
	directory := t.TempDir()
	lock := filepath.Join(directory, layoutLockName)
	source := filepath.Join(directory, "src")
	writeFile(t, filepath.Join(source, gcpkg.ManifestFileName), published)

	bundle := filepath.Join(directory, "shop.gcpkg")
	if _, _, code := runCLI("build", "-o", bundle, "-layout-lock", lock, source); code != exitOK {
		t.Fatalf("the first build of a hand-declared plugin failed with %d", code)
	}
	if _, locked, err := readLayoutLock(lock); err != nil || !locked {
		t.Fatalf("a hand-declared layout was not recorded: locked=%v err=%v", locked, err)
	}

	writeFile(t, filepath.Join(source, gcpkg.ManifestFileName), reordered)
	_, stderr, code := runCLI("build", "-o", bundle, "-layout-lock", lock, source)
	if code == exitOK {
		t.Fatal("build accepted two fields of a hand-declared event being swapped")
	}
	if !strings.Contains(stderr, "field 0") {
		t.Fatalf("build refused without naming the index: %s", stderr)
	}
}

// A plugin that defines no event publishes no layout, and gets no file. It used
// to get one recording nothing — which an author asking for the protection
// would have committed, and been protected by not at all.
func TestAPluginWithNoEventsIsGivenNoLock(t *testing.T) {
	directory := t.TempDir()
	lock := filepath.Join(directory, layoutLockName)
	source := filepath.Join(directory, "src")
	writeFile(t, filepath.Join(source, gcpkg.ManifestFileName), `id = "fr.oreo.quiet"
version = "1.0.0"
api = 1
runtime = "go"
entry = "bin/quiet"
`)
	if _, _, code := runCLI("build", "-o", filepath.Join(directory, "quiet.gcpkg"),
		"-layout-lock", lock, source); code != exitOK {
		t.Fatalf("build failed with %d", code)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("a plugin with no events was given a lock file: %v", err)
	}
}

// The first build of a plugin has nothing to compare against, and making that
// an error would mean an author cannot start.
func TestAMissingLockIsNotAnError(t *testing.T) {
	_, locked, err := readLayoutLock(filepath.Join(t.TempDir(), layoutLockName))
	if err != nil {
		t.Fatalf("readLayoutLock() = %v, want a first build to be allowed", err)
	}
	if locked {
		t.Fatal("readLayoutLock() reported a record that does not exist")
	}
}

// It is a file a human reads in a diff, so what is written has to be stable:
// two builds of the same layouts must produce the same bytes whatever order the
// compiler dumped them in.
func TestTheLockIsStableWhateverTheOrder(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")

	shuffled := publishedLayouts()
	shuffled.Events = append(shuffled.Events, layoutEvent{Type: "fr.oreo.shop/refund"})
	reversed := publishedLayouts()
	reversed.Events = append([]layoutEvent{{Type: "fr.oreo.shop/refund"}}, reversed.Events...)

	if err := writeLayoutLock(first, shuffled); err != nil {
		t.Fatal(err)
	}
	if err := writeLayoutLock(second, reversed); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadFile(first)
	right, _ := os.ReadFile(second)
	if string(left) != string(right) {
		t.Fatalf("writeLayoutLock() is order-dependent:\n%s\n%s", left, right)
	}
}

// Written back and read again, so the record a build keeps is one the next
// build accepts rather than something only this test's structs agree on.
func TestALockRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), layoutLockName)
	if err := writeLayoutLock(path, publishedLayouts()); err != nil {
		t.Fatal(err)
	}
	recorded, locked, err := readLayoutLock(path)
	if err != nil || !locked {
		t.Fatalf("readLayoutLock() = %v, %v", locked, err)
	}
	if err := checkLayoutDrift(recorded, publishedLayouts()); err != nil {
		t.Fatalf("a layout did not survive its own lock: %v", err)
	}
}
