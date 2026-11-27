package main

import (
	"strings"
	"testing"
)

func TestExtractCommandsUsesOnlyAnnotatedBlocksInOrder(t *testing.T) {
	doc := strings.Join([]string{
		"Run this manually: go run ./cmd/ladder -to NGNC",
		"```bash",
		"go run ./cmd/wayfared -verify-store -data ./data",
		"```",
		"```bash walkthrough-ci",
		"make test",
		"```",
		"```bash walkthrough-ci",
		"go run ./cmd/hop-analysis -snapshots ./testdata/snapshots",
		"```",
	}, "\n")

	got, err := extractCommands(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"make test",
		"go run ./cmd/hop-analysis -snapshots ./testdata/snapshots",
	}
	if len(got) != len(want) {
		t.Fatalf("extracted %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractCommandsRejectsMalformedAnnotations(t *testing.T) {
	for _, fence := range []string{
		"```sh walkthrough-ci",
		"```bash unsafe walkthrough-ci",
		"```bash walkthrough-ci extra",
		"````bash walkthrough-ci",
	} {
		t.Run(fence, func(t *testing.T) {
			doc := fence + "\nmake test\n```"
			if _, err := extractCommands(strings.NewReader(doc)); err == nil {
				t.Fatal("malformed annotation was accepted")
			}
		})
	}
}

func TestExtractCommandsRejectsEmptyBlocks(t *testing.T) {
	for _, body := range []string{"", "\n  \n# comment\n"} {
		doc := "```bash walkthrough-ci\n" + body + "```"
		if _, err := extractCommands(strings.NewReader(doc)); err == nil {
			t.Errorf("empty annotated block %q was accepted", body)
		}
	}
}

func TestExtractCommandsRejectsUnsupportedShell(t *testing.T) {
	for _, command := range []string{
		"go run ./cmd/ladder -to NGNC",
		"make test && go run ./cmd/ladder",
		"curl https://example.com",
	} {
		doc := "```bash walkthrough-ci\n" + command + "\n```"
		if _, err := extractCommands(strings.NewReader(doc)); err == nil {
			t.Errorf("unsupported command %q was accepted", command)
		}
	}
}

func TestExtractCommandsIgnoresUnannotatedBlocks(t *testing.T) {
	doc := "````bash\n```bash walkthrough-ci\nmake test\n```\n````\n"
	got, err := extractCommands(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("unannotated command blocks yielded %v", got)
	}
}
