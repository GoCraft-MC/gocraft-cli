package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
