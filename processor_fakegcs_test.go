package main

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
)

func jpegFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcess_WithFakeGCS(t *testing.T) {
	jpg := jpegFixture(t)
	srv, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		Scheme: "http",
		InitialObjects: []fakestorage.Object{
			{
				ObjectAttrs: fakestorage.ObjectAttrs{
					BucketName: "test-bucket",
					Name:       "images/pipe.jpg",
				},
				Content: jpg,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	client := srv.Client()
	cfg := Config{
		ResizeTargets:   []ResizeTarget{{Label: "w480", Width: 24}},
		EnableWatermark: false,
		CacheControl:    "public, max-age=1",
	}
	p, err := NewProcessor(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = p.Process(ctx, storageEvent{Bucket: "test-bucket", Name: "images/pipe.jpg"})
	if err != nil {
		t.Fatal(err)
	}
}
