package server

import (
	"strings"
	"testing"
)

func TestUICorridorCacheIsValidatedAndIsolated(t *testing.T) {
	page := uiHTML(t)

	for _, want := range []string{
		"const CORRIDOR_CACHE_PREFIX = 'wayfare:corridor-reading:v1:'",
		"return `${CORRIDOR_CACHE_PREFIX}${encodeURIComponent(code)}`",
		"function validCorridorReading(data, code)",
		"data.receive_asset?.code !== code",
		"function validTimestamp(value)",
		"function saveCorridorReading(code, data)",
		"localStorage.setItem(corridorCacheKey(code)",
		"function readCorridorReading(code)",
		"localStorage.getItem(corridorCacheKey(code))",
		"cached.corridor !== code",
		"readingRecordedAt(cached.reading) !== cached.recorded_at",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("offline cache is missing %q", want)
		}
	}

	measure := page[strings.Index(page, "async function measure()"):strings.Index(page, "// errorPanel")]
	load := page[strings.Index(page, "async function loadCorridor(to)"):strings.Index(page, "// On page load")]
	for name, source := range map[string]string{"measure": measure, "loadCorridor": load} {
		for _, want := range []string{
			"if (!r.ok) throw",
			"if (!validCorridorReading(data, to)) throw",
			"saveCorridorReading(to, data)",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s() does not include %q", name, want)
			}
		}
		if strings.Index(source, "if (!r.ok) throw") > strings.Index(source, "saveCorridorReading(to, data)") {
			t.Errorf("%s() can cache an unsuccessful HTTP response", name)
		}
		if strings.Index(source, "if (!validCorridorReading(data, to)) throw") > strings.Index(source, "saveCorridorReading(to, data)") {
			t.Errorf("%s() can cache an invalid corridor response", name)
		}
	}
}

func TestUICorridorCacheFallbackPreservesStaleSemantics(t *testing.T) {
	page := uiHTML(t)
	measure := page[strings.Index(page, "async function measure()"):strings.Index(page, "// errorPanel")]
	load := page[strings.Index(page, "async function loadCorridor(to)"):strings.Index(page, "// On page load")]
	for name, source := range map[string]string{"measure": measure, "loadCorridor": load} {
		for _, want := range []string{
			"const cached = readCorridorReading(to)",
			"recordedAt: cached.recorded_at",
			"fetchedAt: cached.fetched_at",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s() does not use its matching cached fallback %q", name, want)
			}
		}
	}

	for _, want := range []string{
		"readingRecordedAt(data)",
		"recorded_at: recordedAt",
		"function formatReadingAge(recordedAt)",
		"Date.now() - Date.parse(recordedAt)",
		"if (seconds < 60) return 'just now'",
		"if (seconds < 3600)",
		"if (seconds < 48 * 3600)",
		"const cached = readCorridorReading(to)",
		"fetchedAt: cached.fetched_at",
		"CACHED READING — NOT CURRENT MARKET DATA",
		"browser cached ${esc(fetchedAt)}",
		"formatReadingAge(recordedAt)",
		"render(data);",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("offline fallback is missing %q", want)
		}
	}

	cachedBranch := strings.Index(page, "const provenance = cached")
	liveBranch := strings.Index(page, ": d.live")
	if cachedBranch < 0 || liveBranch < 0 || cachedBranch > liveBranch {
		t.Error("cached provenance must be selected before the live response branch")
	}
	if !strings.Contains(page, "if (cached) {") || !strings.Contains(page, "If the instance is still waking up") {
		t.Error("a missing cache must preserve the existing unavailable/error message")
	}
}

func TestUICorridorCacheHandlesStorageAndRequestRaces(t *testing.T) {
	page := uiHTML(t)

	for _, want := range []string{
		"} catch (e) {\n    // A full or unavailable store must not prevent the reading from rendering.",
		"} catch (e) {\n    return null;",
		"let corridorRequest = 0;",
		"const requestId = ++corridorRequest;",
		"if (requestId !== corridorRequest || $('to').value !== to) return;",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("offline cache failure/race handling is missing %q", want)
		}
	}

	if strings.Count(page, "if (requestId !== corridorRequest || $('to').value !== to) return;") < 4 {
		t.Error("both request paths must ignore stale success and failure completions")
	}
}
