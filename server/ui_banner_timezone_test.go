package server

import (
	"strings"
	"testing"
)

// TestUIRecordedReadingBannerTimezoneAndRelativeAge asserts that index.html
// uses Intl.DateTimeFormat and Intl.RelativeTimeFormat to display local time
// and relative age, while preserving the UTC timestamp in a tooltip (issue #627).
func TestUIRecordedReadingBannerTimezoneAndRelativeAge(t *testing.T) {
	page := uiPage(t)

	for _, want := range []string{
		"Intl.DateTimeFormat",
		"Intl.RelativeTimeFormat",
		"formatLocalDateTime",
		"formatRelativeAge",
		`title="UTC:`,
		`<time datetime=`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html missing %q for local timezone and relative age banner display", want)
		}
	}
}
