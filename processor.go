package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	imagedraw "image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cloud.google.com/go/storage"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

var derivedObjectPattern = regexp.MustCompile(`-w\d{2,}`)

type Processor struct {
	cfg       Config
	storage   *storage.Client
	watermark *image.NRGBA
}

func NewProcessor(cfg Config, storageClient *storage.Client) (*Processor, error) {
	p := &Processor{
		cfg:     cfg,
		storage: storageClient,
	}
	if !cfg.EnableWatermark {
		return p, nil
	}

	wmBytes, err := os.ReadFile(cfg.WatermarkPath)
	if err != nil {
		return nil, fmt.Errorf("read watermark: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(wmBytes))
	if err != nil {
		return nil, fmt.Errorf("decode watermark: %w", err)
	}
	p.watermark = toNRGBA(img)
	return p, nil
}

func (p *Processor) Process(ctx context.Context, event storageEvent) error {
	if !isSupportedImage(event.Name) {
		log.Printf("skip unsupported object: %s", event.Name)
		return nil
	}
	if derivedObjectPattern.MatchString(event.Name) {
		log.Printf("skip derived object: %s", event.Name)
		return nil
	}

	reader, err := p.storage.Bucket(event.Bucket).Object(event.Name).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("open object: %w", err)
	}
	defer reader.Close()

	originalBytes, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read object: %w", err)
	}
	sourceImg, _, err := image.Decode(bytes.NewReader(originalBytes))
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	base := filepath.Base(event.Name)
	ext := strings.ToLower(filepath.Ext(base))
	nameWithoutExt := strings.TrimSuffix(base, filepath.Ext(base))
	baseDir := strings.TrimSuffix(event.Name, base)

	for _, target := range p.cfg.ResizeTargets {
		resized := resizeImage(sourceImg, target.Width)
		if p.cfg.EnableWatermark {
			resized = applyWatermark(resized, p.watermark, p.cfg.WatermarkScale, p.cfg.WatermarkMarginRatio, p.cfg.WatermarkOpacity)
		}

		mainObjectName := baseDir + nameWithoutExt + "-" + target.Label + ext
		mainBytes, err := encodeByExt(resized, ext)
		if err != nil {
			return fmt.Errorf("encode %s: %w", mainObjectName, err)
		}
		if err := p.uploadObject(ctx, event.Bucket, mainObjectName, contentTypeFromExt(ext), mainBytes); err != nil {
			return err
		}
	}

	return nil
}

func (p *Processor) uploadObject(ctx context.Context, bucketName, objectName, contentType string, payload []byte) error {
	writer := p.storage.Bucket(bucketName).Object(objectName).NewWriter(ctx)
	writer.ContentType = contentType
	writer.CacheControl = p.cfg.CacheControl

	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write object %s: %w", objectName, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close object %s: %w", objectName, err)
	}

	log.Printf("uploaded gs://%s/%s", bucketName, objectName)
	return nil
}

func isSupportedImage(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func resizeImage(src image.Image, targetWidth int) *image.NRGBA {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width == 0 || height == 0 || targetWidth <= 0 {
		return toNRGBA(src)
	}

	targetHeight := height * targetWidth / width
	if targetHeight <= 0 {
		targetHeight = 1
	}

	dst := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	return dst
}

func applyWatermark(base *image.NRGBA, watermark *image.NRGBA, scale, marginRatio, opacity float64) *image.NRGBA {
	if base == nil || watermark == nil {
		return base
	}
	if scale <= 0 {
		scale = 0.15
	}
	if marginRatio < 0 {
		marginRatio = 0
	}
	if opacity <= 0 {
		opacity = 1
	}
	if opacity > 1 {
		opacity = 1
	}

	targetWidth := int(float64(base.Bounds().Dx()) * scale)
	if targetWidth < 1 {
		targetWidth = 1
	}

	scaled := resizeImage(watermark, targetWidth)
	if opacity < 1 {
		scaled = adjustOpacity(scaled, opacity)
	}

	margin := int(float64(base.Bounds().Dx()) * marginRatio)
	x := base.Bounds().Dx() - scaled.Bounds().Dx() - margin
	y := base.Bounds().Dy() - scaled.Bounds().Dy() - margin
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	result := cloneNRGBA(base)
	rect := image.Rect(x, y, x+scaled.Bounds().Dx(), y+scaled.Bounds().Dy())
	imagedraw.Draw(result, rect, scaled, image.Point{}, imagedraw.Over)
	return result
}

func adjustOpacity(img *image.NRGBA, opacity float64) *image.NRGBA {
	out := cloneNRGBA(img)
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = uint8(float64(out.Pix[i]) * opacity)
	}
	return out
}

func encodeByExt(img image.Image, ext string) ([]byte, error) {
	var buf bytes.Buffer
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		if err := jpeg.Encode(&buf, flattenIfNeeded(img), &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
	case ".png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
	case ".gif":
		if err := gif.Encode(&buf, flattenIfNeeded(img), nil); err != nil {
			return nil, err
		}
	case ".tif", ".tiff":
		if err := tiff.Encode(&buf, img, nil); err != nil {
			return nil, err
		}
	default:
		if err := jpeg.Encode(&buf, flattenIfNeeded(img), &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func flattenIfNeeded(img image.Image) image.Image {
	nrgba := toNRGBA(img)
	if !hasAlpha(nrgba) {
		return nrgba
	}
	rgba := image.NewRGBA(nrgba.Bounds())
	imagedraw.Draw(rgba, rgba.Bounds(), image.NewUniform(image.White), image.Point{}, imagedraw.Src)
	imagedraw.Draw(rgba, rgba.Bounds(), nrgba, image.Point{}, imagedraw.Over)
	return rgba
}

func hasAlpha(img *image.NRGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return true
		}
	}
	return false
}

func toNRGBA(src image.Image) *image.NRGBA {
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	imagedraw.Draw(dst, dst.Bounds(), src, bounds.Min, imagedraw.Src)
	return dst
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}
