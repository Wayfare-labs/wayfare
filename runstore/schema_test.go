package runstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The schemas are the language-neutral form of the record shape (#698): a
// researcher can parse data/*.ndjson with any JSON tooling instead of reading
// Go. That only helps if the schemas are true, so this test validates every
// committed record against the schema for its version. A field added to a
// record without updating the schema fails here, in CI, rather than in someone
// else's parser months later.

const schemasDir = "../docs/schemas"

func loadSchema(t *testing.T, version int) *Schema {
	t.Helper()
	path := filepath.Join(schemasDir, fmt.Sprintf("runstore-v%d.json", version))
	schema, err := LoadSchema(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return schema
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// committedRecords returns every record in data/*.ndjson, keyed by version.
func committedRecords(t *testing.T) map[int][]map[string]any {
	t.Helper()
	files, err := filepath.Glob("../data/*.ndjson")
	if err != nil || len(files) == 0 {
		t.Fatalf("no committed records to validate: %v", err)
	}
	sort.Strings(files)

	out := map[int][]map[string]any{}
	for _, file := range files {
		f := mustOpen(t, file)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
		line := 0
		for scanner.Scan() {
			line++
			raw := scanner.Bytes()
			if len(raw) == 0 {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatalf("%s:%d: %v", file, line, err)
			}
			version, ok := record["version"].(float64)
			if !ok {
				t.Fatalf("%s:%d: record has no numeric version", file, line)
			}
			out[int(version)] = append(out[int(version)], record)
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	return out
}

func TestCommittedRecordsMatchTheirSchema(t *testing.T) {
	records := committedRecords(t)

	checked := 0
	for version, group := range records {
		schema := loadSchema(t, version)
		for i, record := range group {
			if errs := schema.Validate(record); len(errs) > 0 {
				t.Errorf("version %d record %d (%v) does not match runstore-v%d.json: %v",
					version, i, record["corridor"], version, errs)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("validated no records; the glob found nothing")
	}
}

func TestEveryLoadableVersionHasASchema(t *testing.T) {
	// A version the loader accepts but no schema describes is a gap a third
	// party discovers the hard way.
	for version := 1; version <= Version; version++ {
		path := filepath.Join(schemasDir, fmt.Sprintf("runstore-v%d.json", version))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("version %d is loadable but has no schema at %s", version, path)
		}
	}
}

func TestSchemaRejectsAFieldTheRecordDoesNotHave(t *testing.T) {
	// The schema is only worth publishing if it is closed: an extra field is
	// how a fork silently drifts from the format described here.
	schema := loadSchema(t, 1)
	records := committedRecords(t)[1]
	if len(records) == 0 {
		t.Skip("no version 1 records committed")
	}

	record := map[string]any{}
	for k, v := range records[0] {
		record[k] = v
	}
	record["unexpected_field"] = true

	if errs := schema.Validate(record); len(errs) == 0 {
		t.Fatal("schema accepted an unknown field")
	}
}

func TestSchemaRejectsANumberWhereMoneyBelongs(t *testing.T) {
	schema := loadSchema(t, 1)
	records := committedRecords(t)[1]
	if len(records) == 0 {
		t.Skip("no version 1 records committed")
	}

	record := map[string]any{}
	for k, v := range records[0] {
		record[k] = v
	}
	record["floor_loss_pct"] = 27.15 // a JSON number, not a decimal string

	if errs := schema.Validate(record); len(errs) == 0 {
		t.Fatal("schema accepted a float for floor_loss_pct")
	}
}
