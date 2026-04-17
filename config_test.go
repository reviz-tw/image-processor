package main

import "testing"

func TestParseResizeTargets(t *testing.T) {
	targets, err := ParseResizeTargets("w480,w800,w1200")
	if err != nil {
		t.Fatalf("ParseResizeTargets returned error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}
	if targets[1].Label != "w800" || targets[1].Width != 800 {
		t.Fatalf("unexpected target: %+v", targets[1])
	}
}

func TestParseResizeTargetsRejectsInvalidValue(t *testing.T) {
	if _, err := ParseResizeTargets("480"); err == nil {
		t.Fatal("expected invalid target to return an error")
	}
}
