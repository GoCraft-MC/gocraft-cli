package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The layout lock: what this plugin's events looked like the last time it was
// built, kept so the next build can refuse a change that breaks everyone who
// already compiled against them.
//
// §10 is categorical about this and it is the one mechanical check it asks for
// that was missing: "Field order defines the wire indexes. Reordering or
// removing a field silently shifts them for everyone who compiled against the
// previous version. The build keeps the previous layout in the project and
// refuses a reorder or a removal; appending at the end is fine."
//
// The failure it prevents is quiet, which is why it is worth a file. Swap two
// fields of an event and everything still works on the author's machine: their
// own build regenerates both sides. It is the subscriber compiled last month —
// possibly in another language, possibly in another repository — that reads the
// price out of the field that now holds the name, with nothing anywhere saying
// so.
//
// Named on the command line rather than derived from the packed directory,
// because that directory is not the project: the Gradle plugin stages a copy
// and empties it on every run, so a lock written there would be gone before the
// next build could read it. Whoever invokes the packer knows where the project
// is; the packer does not have to guess.
//
// The same JSON the annotation processor writes, so there is one description of
// a layout and no second decoder to keep in step. It is meant to be committed —
// it is the record of what this plugin has already published.

// checkLayoutDrift compares what the compiler just extracted against what the
// last build recorded.
//
// Appending is allowed and everything else is refused, which is §03's additive
// rule one level down. Both directions of a removal count: a field that is gone
// and an event that is gone break the same subscribers in the same way.
func checkLayoutDrift(previous, current eventLayouts) error {
	events := make(map[string]layoutEvent, len(current.Events))
	for _, event := range current.Events {
		events[event.Type] = event
	}
	for _, was := range previous.Events {
		now, still := events[was.Type]
		if !still {
			return fmt.Errorf("event %s was published and this build does not define it; "+
				"every subscriber compiled against it is still expecting it", was.Type)
		}
		if err := checkFieldDrift("event "+was.Type, was.Fields, now.Fields); err != nil {
			return err
		}
	}

	records := make(map[string]layoutRecord, len(current.Types))
	for _, record := range current.Types {
		records[record.Name] = record
	}
	for _, was := range previous.Types {
		now, still := records[was.Name]
		if !still {
			return fmt.Errorf("record %s was published and this build does not define it; "+
				"every event that carried one is a different shape now", was.Name)
		}
		if err := checkFieldDrift("record "+was.Name, was.Fields, now.Fields); err != nil {
			return err
		}
	}
	return nil
}

// checkFieldDrift compares one layout, index by index.
//
// The index is the whole contract — nothing on the wire carries a field name —
// so this compares positions and not sets. A field renamed in place is allowed
// and deliberately so: the name is the author's, it never crosses the socket,
// and refusing a rename would be refusing something that breaks nobody.
func checkFieldDrift(what string, was, now []layoutField) error {
	if len(now) < len(was) {
		return fmt.Errorf("%s had %d fields and now has %d; removing one shifts every "+
			"index after it for everyone already compiled against this plugin, so append "+
			"instead — or change the event's type if it is genuinely a different event",
			what, len(was), len(now))
	}
	for index, before := range was {
		after := now[index]
		if before.Type != after.Type {
			return fmt.Errorf("%s field %d was a %s and is now a %s; a subscriber compiled "+
				"against the old layout would read it as the old kind and get whatever "+
				"happens to be there", what, index, before.Type, after.Type)
		}
		if before.Mutable && !after.Mutable {
			return fmt.Errorf("%s field %d (%s) was mutable and is now final; a subscriber "+
				"that writes to it would have its change refused by the host, silently",
				what, index, before.Name)
		}
	}
	return nil
}

// readLayoutLock reads the recorded layout, reporting whether there was one.
//
// A missing file is the first build of a plugin that has never published
// anything, and there is nothing to compare against. That is not an error and
// must not be: making it one would mean an author cannot start.
func readLayoutLock(path string) (eventLayouts, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return eventLayouts{}, false, nil
	}
	if err != nil {
		return eventLayouts{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var locked eventLayouts
	if err := json.Unmarshal(raw, &locked); err != nil {
		return eventLayouts{}, false, fmt.Errorf("%s is not a layout lock: %w", path, err)
	}
	if locked.Version != layoutVersion {
		return eventLayouts{}, false, fmt.Errorf(
			"%s records version %d, this build reads %d", path, locked.Version, layoutVersion)
	}
	return locked, true, nil
}

// writeLayoutLock records what this build published.
//
// Written after the bundle, so a build that failed does not advance the record
// and let the next one through with a change nobody shipped. Sorted and
// indented, because it is a file a human reads in a diff — a reordering that
// shows up as a whole-file change tells a reviewer nothing.
func writeLayoutLock(path string, layouts eventLayouts) error {
	recorded := eventLayouts{
		Version: layoutVersion,
		Types:   append([]layoutRecord(nil), layouts.Types...),
		Events:  append([]layoutEvent(nil), layouts.Events...),
	}
	sort.Slice(recorded.Types, func(i, j int) bool {
		return recorded.Types[i].Name < recorded.Types[j].Name
	})
	sort.Slice(recorded.Events, func(i, j int) bool {
		return recorded.Events[i].Type < recorded.Events[j].Type
	})
	encoded, err := json.MarshalIndent(recorded, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// describeLock is what the build prints about the record it kept, so an author
// sees that a file they have to commit was written.
func describeLock(path string, existed bool) string {
	verb := "recorded"
	if existed {
		verb = "updated"
	}
	return fmt.Sprintf("%s: %s (commit it: it is what refuses a reordered layout)",
		filepath.ToSlash(path), verb)
}

// layoutLockName is the conventional file name, for a caller with nothing
// better to say.
const layoutLockName = "events.lock.json"

func defaultLayoutLock(directory string) string {
	return filepath.Join(strings.TrimSpace(directory), layoutLockName)
}
