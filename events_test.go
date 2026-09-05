package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCraft-MC/gocraft-abi/gcpkg"
)

const shopManifest = `id = "fr.oreo.shop"
version = "0.1.0"
api = 1
runtime = "jvm"
entry = "fr.oreo.shop.ShopPlugin"
`

const purchaseLayouts = `{
  "version": 1,
  "types": [
    { "name": "fr.oreo.Tier", "fields": [
      { "name": "label", "type": "string", "mutable": false },
      { "name": "price", "type": "double", "mutable": true }
    ]}
  ],
  "events": [
    { "type": "fr.oreo.shop/purchase", "cancellable": true, "failClosed": false, "fields": [
      { "name": "buyer", "type": "PlayerRef", "mutable": false },
      { "name": "tiers", "type": "[]fr.oreo.Tier", "mutable": false },
      { "name": "price", "type": "double", "mutable": true }
    ]}
  ]
}`

func layoutFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The point of the whole exercise: an author annotates the classes and writes
// no layout at all, and what lands in the bundle is what the compiler saw.
func TestMergeWritesWhatTheCompilerExtracted(t *testing.T) {
	layouts, err := readEventLayouts(layoutFile(t, purchaseLayouts))
	if err != nil {
		t.Fatal(err)
	}
	declared, err := gcpkg.DecodeManifest(strings.NewReader(shopManifest))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeEventLayouts([]byte(shopManifest), layouts, declared)
	if err != nil {
		t.Fatalf("mergeEventLayouts() = %v", err)
	}

	// The author's own file is still in there, comments and all: appended, not
	// re-encoded.
	if !bytes.HasPrefix(merged, []byte(shopManifest)) {
		t.Fatalf("the author's manifest was rewritten:\n%s", merged)
	}
	// And the result is what the server's own loader reads.
	manifest, err := gcpkg.DecodeManifest(bytes.NewReader(merged))
	if err != nil {
		t.Fatalf("the merged manifest does not load: %v\n%s", err, merged)
	}
	if len(manifest.Records) != 1 || manifest.Records[0].Name != "fr.oreo.Tier" {
		t.Fatalf("records = %+v", manifest.Records)
	}
	if !manifest.Records[0].Fields[1].Mutable {
		t.Fatalf("record fields = %+v, want price mutable", manifest.Records[0].Fields)
	}
	if len(manifest.Provides) != 1 {
		t.Fatalf("provides = %+v", manifest.Provides)
	}
	event := manifest.Provides[0]
	if event.Type != "fr.oreo.shop/purchase" || !event.Cancellable || event.FailClosed {
		t.Fatalf("event = %+v", event)
	}
	want := []gcpkg.EventField{
		{Name: "buyer", Type: "PlayerRef"},
		{Name: "tiers", Type: "[]fr.oreo.Tier"},
		{Name: "price", Type: "double", Mutable: true},
	}
	for index, field := range event.Fields {
		if field != want[index] {
			t.Fatalf("field %d = %+v, want %+v", index, field, want[index])
		}
	}
}

// Two descriptions of one layout is what this exists to remove, so the build
// refuses rather than picking one.
func TestMergeRefusesABlockTheAuthorAlsoWrote(t *testing.T) {
	layouts, err := readEventLayouts(layoutFile(t, purchaseLayouts))
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"an event": shopManifest + "\n[[events.provides]]\ntype = \"fr.oreo.shop/purchase\"\n",
		"a record": shopManifest + "\n[[events.types]]\nname = \"fr.oreo.Tier\"\n" +
			"fields = [{ name = \"label\", type = \"string\" }]\n",
	} {
		declared, err := gcpkg.DecodeManifest(strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mergeEventLayouts([]byte(source), layouts, declared); err == nil {
			t.Fatalf("mergeEventLayouts() accepted %s written by hand", name)
		}
	}
}

// A plugin with no events at all leaves the manifest exactly as it was. Nothing
// worse than a build that rewrites a file to add nothing.
func TestMergeLeavesAManifestWithNothingToAdd(t *testing.T) {
	layouts, err := readEventLayouts(layoutFile(t, `{"version": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	declared, err := gcpkg.DecodeManifest(strings.NewReader(shopManifest))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeEventLayouts([]byte(shopManifest), layouts, declared)
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != shopManifest {
		t.Fatalf("merged = %q, want the manifest untouched", merged)
	}
}

// Refused by number rather than read as far as it happens to parse: a field
// added by a newer processor would otherwise silently not reach the manifest.
func TestReadEventLayoutsRefusesAnotherVersion(t *testing.T) {
	_, err := readEventLayouts(layoutFile(t, `{"version": 2}`))
	if err == nil {
		t.Fatal("readEventLayouts() accepted a version it does not read")
	}
	if !strings.Contains(err.Error(), "newer than this gocraft-cli") {
		t.Fatalf("readEventLayouts() error = %v", err)
	}
}
