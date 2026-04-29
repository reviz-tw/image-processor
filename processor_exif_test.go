package main

import (
	"testing"

	"github.com/rwcarlsen/goexif/tiff"
)

func TestExifWalker_Walk(t *testing.T) {
	t.Parallel()
	w := &exifWalker{Data: make(map[string]interface{})}
	tag := &tiff.Tag{}
	if err := w.Walk("SomeField", tag); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Data["SomeField"]; !ok {
		t.Fatal("expected key in map")
	}
}
