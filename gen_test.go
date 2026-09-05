package main

import (
	"strings"
	"testing"

	"github.com/GoCraft-MC/gocraft-abi/gcpkg"
)

const providerManifest = `id = "gocraft.example.java"
version = "0.1.0"
api = 1
runtime = "jvm"
entry = "gocraft.example.ExamplePlugin"

[[events.types]]
name = "gocraft.example.Tier"
fields = [
  { name = "label", type = "string" },
  { name = "price", type = "double", mutable = true },
]

[[events.provides]]
type = "gocraft.example/purchase"
cancellable = true
fields = [
  { name = "buyer", type = "PlayerRef" },
  { name = "tiers", type = "[]gocraft.example.Tier" },
  { name = "price", type = "double", mutable = true },
]
`

func provider(t *testing.T) (gcpkg.Manifest, names) {
	t.Helper()
	manifest, err := gcpkg.DecodeManifest(strings.NewReader(providerManifest))
	if err != nil {
		t.Fatal(err)
	}
	chosen, err := typeNames(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, chosen
}

// An event type is namespace/name and a record is dotted, and neither is an
// identifier. The last segment is what an author types.
func TestTypeNamesTakeTheLastSegment(t *testing.T) {
	_, chosen := provider(t)
	if got := chosen.events["gocraft.example/purchase"]; got != "Purchase" {
		t.Fatalf("event name = %q, want Purchase", got)
	}
	if got := chosen.records["gocraft.example.Tier"]; got != "Tier" {
		t.Fatalf("record name = %q, want Tier", got)
	}
}

// Refused rather than silently overwriting, because the second file written
// would be the one the compiler sees.
func TestTypeNamesRefuseACollision(t *testing.T) {
	manifest, err := gcpkg.DecodeManifest(strings.NewReader(`id = "fr.oreo.shop"
version = "0.1.0"
api = 1
runtime = "jvm"

[[events.provides]]
type = "fr.oreo.a/purchase"

[[events.provides]]
type = "fr.oreo.b/purchase"
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := typeNames(manifest); err == nil {
		t.Fatal("typeNames() accepted two events generating one name")
	}
}

func TestGeneratedGoCarriesTheWholeShape(t *testing.T) {
	manifest, chosen := provider(t)
	files, err := generateGo(manifest, chosen, "shopevents")
	if err != nil {
		t.Fatal(err)
	}
	source := files["events.gen.go"]

	// The struct, with the provider's mutability written where an author reads
	// it rather than left to the host to refuse at runtime.
	if !strings.Contains(source, "Price float64") {
		t.Fatalf("no price field:\n%s", source)
	}
	if !strings.Contains(source, "Tiers []Tier") {
		t.Fatalf("a list of records did not become a slice:\n%s", source)
	}
	if !strings.Contains(source, "Buyer *gocraft.PlayerRef") {
		t.Fatalf("a PlayerRef did not become a handle:\n%s", source)
	}
	// The point of PlayerRef being in the vocabulary: read from the dispatch,
	// so a subscriber is handed somebody it can answer.
	if !strings.Contains(source, "dispatch.Player(0)") {
		t.Fatalf("the player was not read from the dispatch:\n%s", source)
	}
	// A mutable scalar is compared before it is sent back, so an untouched one
	// costs no mutation.
	if !strings.Contains(source, "if event.Price != before2 {") {
		t.Fatalf("a mutable field is written back unconditionally:\n%s", source)
	}
	if !strings.Contains(source, "func OnPurchase(events *gocraft.Events") {
		t.Fatalf("no typed subscription:\n%s", source)
	}
	// It satisfies gocraft.CustomEvent, so the same file serves the plugin that
	// publishes the event and the one that receives it.
	for _, method := range []string{"EventType() string", "Fields() []gocraft.Value",
		"SetFields(fields []gocraft.Value) error"} {
		if !strings.Contains(source, method) {
			t.Fatalf("%s is missing:\n%s", method, source)
		}
	}
}

// Formatted here rather than left to the author, and a file that will not parse
// is a bug in the emitter reported as one.
func TestGeneratedGoIsGofmtClean(t *testing.T) {
	manifest, chosen := provider(t)
	files, err := generateGo(manifest, chosen, "shopevents")
	if err != nil {
		t.Fatalf("the emitter produced Go it cannot parse: %v", err)
	}
	if strings.Contains(files["events.gen.go"], "Tiers []Tier //") {
		t.Fatal("the output was not run through go/format")
	}
}

func TestGeneratedJavaCarriesTheWholeShape(t *testing.T) {
	manifest, chosen := provider(t)
	files, err := generateJava(manifest, chosen, "gocraft.example.shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Purchase.java", "PurchaseLayout.java", "Tier.java",
		"TierValues.java"} {
		if _, written := files[name]; !written {
			t.Fatalf("%s was not generated; got %v", name, files)
		}
	}

	// The class a handler declares as its parameter carries no annotation:
	// @PluginEvent is how an author declares an event they define, and a
	// subscriber defines nothing.
	event := files["Purchase.java"]
	if strings.Contains(event, "@PluginEvent") {
		t.Fatalf("a generated class claimed to define the event:\n%s", event)
	}
	if !strings.Contains(event, "private final java.util.List<Tier> tiers;") {
		t.Fatalf("an immutable list was not final:\n%s", event)
	}
	if !strings.Contains(event, "public void setPrice(double price)") {
		t.Fatalf("a mutable field has no setter:\n%s", event)
	}

	// The codec is resolved by name, so it has to sit beside the class and
	// implement what the runtime asks of it.
	codec := files["PurchaseLayout.java"]
	if !strings.Contains(codec, "public final class PurchaseLayout implements CustomEvent") {
		t.Fatalf("the codec is not what the runtime resolves:\n%s", codec)
	}
	if !strings.Contains(codec, "fr.gocraft.api.PlayerRef.of(fields.get(0), sink)") {
		t.Fatalf("the player was not bound to the dispatch:\n%s", codec)
	}
	if !strings.Contains(codec, "TierValues.decode(item, sink)") {
		t.Fatalf("a record was not read through its own codec:\n%s", codec)
	}
}
