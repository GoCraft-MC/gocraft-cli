package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCraft-MC/gocraft-abi/gcpkg"
)

// genCommand writes the types a plugin needs in order to subscribe to somebody
// else's events.
//
// The alternative is what every plugin system ends up with: the subscriber
// declares its own copy of the event and keeps it in step by hand. Two
// descriptions of one layout, and the wire carries no names — so a copy whose
// fields drifted decodes silently wrong, reading a price where a label is.
//
// Generating from the provider's own manifest means there is one description.
// Not two that agree today; one. A subscriber rebuilt after the provider
// changed its event either compiles against the new shape or fails to compile,
// which is the only honest answer a build can give.
func genCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	language := flags.String("lang", "", "go or java")
	pkg := flags.String("package", "", "the package the generated types belong to")
	output := flags.String("o", ".", "directory to write into")
	flags.Usage = func() {
		fmt.Fprint(stderr, `Usage: gocraft-cli gen -lang <go|java> -package <name> [-o <dir>] <bundle|dir>

Writes the event types a provider declares, so a plugin subscribing to them
compiles against the provider's own description rather than a copy of it.

The input is the plugin you depend on: its .gcpkg, or the source directory its
plugin.toml sits in.
`)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 1 || *language == "" || *pkg == "" {
		flags.Usage()
		return exitUsage
	}

	manifest, err := readAnyManifest(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	if len(manifest.Provides) == 0 {
		fmt.Fprintf(stderr, "%s declares no events, so there is nothing to generate\n",
			flags.Arg(0))
		return exitFailure
	}
	names, err := typeNames(manifest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	var files map[string]string
	switch *language {
	case "go":
		files, err = generateGo(manifest, names, *pkg)
	case "java":
		files, err = generateJava(manifest, names, *pkg)
	default:
		fmt.Fprintf(stderr, "unknown language %q, want go or java\n", *language)
		return exitUsage
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	written, err := writeGenerated(*output, files)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	fmt.Fprintf(stdout, "%s: %d event(s), %d record(s) -> %s\n",
		manifest.ID, len(manifest.Provides), len(manifest.Records), *output)
	for _, name := range written {
		fmt.Fprintln(stdout, "  "+name)
	}
	return exitOK
}

// readAnyManifest takes the plugin you depend on however you have it: the
// bundle you were given, or the directory you built it from.
func readAnyManifest(path string) (gcpkg.Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return gcpkg.Manifest{}, err
	}
	if info.IsDir() {
		return readSourceManifest(path)
	}
	bundle, err := gcpkg.Open(path)
	if err != nil {
		return gcpkg.Manifest{}, err
	}
	return bundle.Manifest, nil
}

// names maps a manifest name to the type name generated for it.
type names struct {
	events  map[string]string
	records map[string]string
}

// typeNames decides what each event and record is called in the generated code.
//
// An event type is namespace/name and a record is dotted, and neither is a
// legal identifier — so the last segment is taken and capitalised. Two that
// land on the same name are refused rather than silently overwriting each
// other, because the second file written would be the one the compiler sees.
func typeNames(manifest gcpkg.Manifest) (names, error) {
	chosen := names{events: map[string]string{}, records: map[string]string{}}
	taken := map[string]string{}
	claim := func(name, source string) (string, error) {
		if previous, clash := taken[name]; clash {
			return "", fmt.Errorf("%s and %s would both generate %s; rename one of them",
				previous, source, name)
		}
		taken[name] = source
		return name, nil
	}
	for _, record := range manifest.Records {
		name, err := claim(identifier(lastSegment(record.Name)), record.Name)
		if err != nil {
			return names{}, err
		}
		chosen.records[record.Name] = name
	}
	for _, event := range manifest.Provides {
		_, after, _ := strings.Cut(event.Type, "/")
		name, err := claim(identifier(lastSegment(after)), event.Type)
		if err != nil {
			return names{}, err
		}
		chosen.events[event.Type] = name
	}
	return chosen, nil
}

// lowerFirst is the receiver-name convention both emitters use for a helper
// derived from a type name.
func lowerFirst(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func lastSegment(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index+1:]
	}
	return name
}

// identifier turns a manifest name into one an author can type. Dashes and
// underscores separate words, which is what both languages do with them.
func identifier(name string) string {
	var out strings.Builder
	upper := true
	for _, character := range name {
		switch {
		case character == '-' || character == '_':
			upper = true
		case upper:
			out.WriteString(strings.ToUpper(string(character)))
			upper = false
		default:
			out.WriteRune(character)
		}
	}
	return out.String()
}

// writeGenerated puts the files on disk, creating the directory if it is not
// there. The names come back sorted so a build log reads the same twice.
func writeGenerated(directory string, files map[string]string) ([]string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	written := make([]string, 0, len(files))
	for name := range files {
		written = append(written, name)
	}
	sort.Strings(written)
	for _, name := range written {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			return nil, err
		}
	}
	return written, nil
}

// fieldsOf resolves one field list into what the emitters need: the parsed
// type, and the record it names when it names one.
func fieldsOf(manifest gcpkg.Manifest, fields []gcpkg.EventField) []resolved {
	out := make([]resolved, 0, len(fields))
	for _, field := range fields {
		parsed, _ := gcpkg.ParseFieldType(field.Type)
		out = append(out, resolved{EventField: field, Type: parsed})
	}
	return out
}

type resolved struct {
	gcpkg.EventField
	Type gcpkg.FieldType
}
