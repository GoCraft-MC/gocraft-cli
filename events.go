package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/GoCraft-MC/gocraft-abi/gcpkg"
)

// eventLayouts is what a compiler extracted about the events a plugin defines.
//
// §10 says the manifest block is derived, and this is the half that derives it:
// an annotation processor sees the classes, and only it knows that the second
// field is a double because the author declared it one. Written by hand, the
// block is the same layout in two places — the code and the manifest the build
// checks it against — free to disagree exactly where nothing compares them.
//
// JSON, because the processor writing it runs inside javac and putting a TOML
// encoder on that path to describe four fields would be absurd. It is read by
// one program, in the same build.
type eventLayouts struct {
	Version int            `json:"version"`
	Types   []layoutRecord `json:"types"`
	Events  []layoutEvent  `json:"events"`
}

type layoutRecord struct {
	Name   string        `json:"name"`
	Fields []layoutField `json:"fields"`
}

type layoutEvent struct {
	Type        string        `json:"type"`
	Cancellable bool          `json:"cancellable"`
	FailClosed  bool          `json:"failClosed"`
	Fields      []layoutField `json:"fields"`
}

type layoutField struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Mutable bool   `json:"mutable"`
}

// layoutVersion is the only version this build reads. A dump from a newer
// processor is refused by number rather than read as far as it happens to
// parse, because a field added later would silently not reach the manifest.
const layoutVersion = 1

func readEventLayouts(path string) (eventLayouts, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return eventLayouts{}, fmt.Errorf("read %s: %w", path, err)
	}
	var layouts eventLayouts
	if err := json.Unmarshal(raw, &layouts); err != nil {
		return eventLayouts{}, fmt.Errorf("%s: %w", path, err)
	}
	if layouts.Version != layoutVersion {
		return eventLayouts{}, fmt.Errorf(
			"%s declares version %d, this build reads %d; the plugin was compiled with a "+
				"gocraft-apt newer than this gocraft-cli",
			path, layouts.Version, layoutVersion)
	}
	return layouts, nil
}

// mergeEventLayouts appends what the compiler extracted to the author's
// manifest.
//
// Appended rather than re-encoded: a manifest is a file somebody wrote, with
// comments explaining choices, and rewriting it to add two blocks would throw
// all of that away. The result is decoded afterwards by the same loader the
// server uses, so an appended block that does not fit is refused here.
//
// A block the author also wrote by hand is refused rather than merged. Two
// descriptions of one layout is what this exists to remove, and picking one
// would mean deciding which of the two the author meant.
func mergeEventLayouts(source []byte, layouts eventLayouts, declared gcpkg.Manifest) ([]byte, error) {
	for _, record := range layouts.Types {
		for _, existing := range declared.Records {
			if existing.Name == record.Name {
				return nil, fmt.Errorf(
					"%s declares record %s and the compiler extracted it too; delete the "+
						"[[events.types]] block and let the build write it",
					gcpkg.ManifestFileName, record.Name)
			}
		}
	}
	for _, event := range layouts.Events {
		for _, existing := range declared.Provides {
			if existing.Type == event.Type {
				return nil, fmt.Errorf(
					"%s declares event %s and the compiler extracted it too; delete the "+
						"[[events.provides]] block and let the build write it",
					gcpkg.ManifestFileName, event.Type)
			}
		}
	}
	if len(layouts.Types) == 0 && len(layouts.Events) == 0 {
		return source, nil
	}

	var out strings.Builder
	out.Write(source)
	if len(source) > 0 && !strings.HasSuffix(string(source), "\n") {
		out.WriteString("\n")
	}
	out.WriteString("\n# Written by gocraft-cli from what the compiler saw. Do not edit: it is\n")
	out.WriteString("# derived from the annotated classes, and editing it here would put the\n")
	out.WriteString("# layout in two places again.\n")
	for _, record := range layouts.Types {
		out.WriteString("\n[[events.types]]\n")
		fmt.Fprintf(&out, "name = %s\n", quote(record.Name))
		writeFields(&out, record.Fields)
	}
	for _, event := range layouts.Events {
		out.WriteString("\n[[events.provides]]\n")
		fmt.Fprintf(&out, "type = %s\n", quote(event.Type))
		if event.Cancellable {
			out.WriteString("cancellable = true\n")
		}
		if event.FailClosed {
			out.WriteString("fail_closed = true\n")
		}
		writeFields(&out, event.Fields)
	}
	return []byte(out.String()), nil
}

func writeFields(out *strings.Builder, fields []layoutField) {
	if len(fields) == 0 {
		return
	}
	out.WriteString("fields = [\n")
	for _, field := range fields {
		fmt.Fprintf(out, "  { name = %s, type = %s", quote(field.Name), quote(field.Type))
		if field.Mutable {
			out.WriteString(", mutable = true")
		}
		out.WriteString(" },\n")
	}
	out.WriteString("]\n")
}

// quote writes a TOML basic string.
//
// Names and types have already been through the processor, which accepts only
// identifiers and dotted names, so nothing here needs escaping today. It is
// still done, because "the input cannot contain a quote" is the kind of thing
// that stays true until the day it does not.
func quote(value string) string {
	replaced := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(value)
	return `"` + replaced + `"`
}
