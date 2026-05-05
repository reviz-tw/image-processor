package main

import "testing"

func TestIsSupportedImage(t *testing.T) {
	if !isSupportedImage("images/demo.JPG") {
		t.Fatal("expected JPG to be supported")
	}
	if !isSupportedImage("images/demo.webp") {
		t.Fatal("expected user-uploaded lowercase webp to be supported")
	}
	if isSupportedImage("images/demo.webP") {
		t.Fatal("expected generated mixed-case webP to be skipped")
	}
	if isSupportedImage("images/demo.txt") {
		t.Fatal("expected TXT to be unsupported")
	}
}

func TestDerivedObjectPattern(t *testing.T) {
	if !derivedObjectPattern.MatchString("images/demo-w800.jpg") {
		t.Fatal("expected derived pattern to match resized image")
	}
	if derivedObjectPattern.MatchString("images/demo.jpg") {
		t.Fatal("expected original image not to match derived pattern")
	}
}
