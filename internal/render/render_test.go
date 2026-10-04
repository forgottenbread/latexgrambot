package render

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBuildDocument(t *testing.T) {
	t.Run("default preamble", func(t *testing.T) {
		doc := buildDocument("", `$x^2$`)
		if !strings.Contains(doc, "\\documentclass[preview,varwidth,border=4pt]{standalone}") {
			t.Errorf("missing document class:\n%s", doc)
		}
		if !strings.Contains(doc, defaultPackages) {
			t.Errorf("missing default packages:\n%s", doc)
		}
		if !strings.Contains(doc, "\\begin{document}\n$x^2$\n\\end{document}") {
			t.Errorf("expression not placed in the document body:\n%s", doc)
		}
	})

	t.Run("custom preamble extends packages", func(t *testing.T) {
		doc := buildDocument(`\usepackage{chemist}`, `H_2O`)
		if !strings.Contains(doc, defaultPackages) {
			t.Errorf("default packages missing:\n%s", doc)
		}
		if !strings.Contains(doc, "\\usepackage{chemist}") {
			t.Errorf("custom preamble missing:\n%s", doc)
		}
		if !strings.Contains(doc, "\\begin{document}\nH_2O\n\\end{document}") {
			t.Errorf("expression not placed in the document body:\n%s", doc)
		}
	})
}

func TestExtractError(t *testing.T) {
	log := strings.Join([]string{
		"This is pdfTeX, Version 3.141592653-2.6-1.40.26",
		"! Undefined control sequence.",
		"l.4 \\begin{document}",
		"              \\badcommand",
		"",
		"! LaTeX Error: File `missing.sty' not found.",
		"l.5 \\usepackage{missing}",
	}, "\n")

	excerpt := extractError(log)
	if !strings.Contains(excerpt, "! Undefined control sequence.") {
		t.Errorf("missing first error line in %q", excerpt)
	}
	if !strings.Contains(excerpt, "l.4 \\begin{document}") {
		t.Errorf("missing context line in %q", excerpt)
	}
	if strings.Contains(excerpt, "missing.sty") {
		t.Errorf("second error must not be reported with -halt-on-error behavior: %q", excerpt)
	}
}

func TestExtractErrorWithoutErrorLine(t *testing.T) {
	excerpt := extractError("some unrelated log content")
	if !strings.Contains(excerpt, "without an explicit error") {
		t.Errorf("unexpected excerpt: %q", excerpt)
	}
}

func TestRenderIntegration(t *testing.T) {
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex not installed")
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed")
	}

	r := New("pdflatex", "pdftoppm", 30*time.Second, 2, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := r.Render(ctx, "", `$\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}$`, 150)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if len(result.PNG) == 0 {
		t.Fatal("empty PNG")
	}
	if !strings.HasPrefix(string(result.PNG), "\x89PNG") {
		t.Fatal("output is not a PNG file")
	}
	width, height, err := pngSizeBytes(result.PNG)
	if err != nil {
		t.Fatalf("decode png size: %v", err)
	}
	if width < minRenderedSide && height < minRenderedSide {
		t.Errorf("png is %dx%d, want the smallest side at least %d", width, height, minRenderedSide)
	}
	if width > maxRenderedSide || height > maxRenderedSide {
		t.Errorf("png is %dx%d, want no side beyond %d", width, height, maxRenderedSide)
	}
	if len(result.PDF) == 0 || !strings.HasPrefix(string(result.PDF), "%PDF") {
		t.Fatal("output is not a PDF file")
	}
	if len(result.JPEG) == 0 || !strings.HasPrefix(string(result.JPEG), "\xff\xd8") {
		t.Fatal("output is not a JPEG file")
	}
	jpegWidth, jpegHeight, err := imageSizeBytes(result.JPEG)
	if err != nil {
		t.Fatalf("decode jpeg size: %v", err)
	}
	if result.JPEGWidth != jpegWidth || result.JPEGHeight != jpegHeight {
		t.Errorf("reported JPEG size %dx%d, decoded %dx%d", result.JPEGWidth, result.JPEGHeight, jpegWidth, jpegHeight)
	}
	if len(result.Thumbnail) == 0 || !strings.HasPrefix(string(result.Thumbnail), "\xff\xd8") {
		t.Fatal("output is not a JPEG thumbnail")
	}
	thumbWidth, thumbHeight, err := imageSizeBytes(result.Thumbnail)
	if err != nil {
		t.Fatalf("decode thumbnail size: %v", err)
	}
	if thumbWidth > inlineThumbnailSide || thumbHeight > inlineThumbnailSide {
		t.Errorf("thumbnail is %dx%d, want no side beyond %d", thumbWidth, thumbHeight, inlineThumbnailSide)
	}

	linked, err := r.Render(ctx, "", `\hypertarget{details}{Details}\par\hyperlink{details}{Go to details}`, 150)
	if err != nil {
		t.Fatalf("hyperlink render failed: %v", err)
	}
	if len(linked.PDF) == 0 || len(linked.PNG) == 0 || len(linked.JPEG) == 0 || len(linked.Thumbnail) == 0 {
		t.Fatal("hyperlink render returned an empty format")
	}
}

func pngSizeBytes(data []byte) (int, int, error) {
	return imageSizeBytes(data)
}

func imageSizeBytes(data []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

func TestRenderErrorIntegration(t *testing.T) {
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex not installed")
	}

	r := New("pdflatex", "pdftoppm", 30*time.Second, 2, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err := r.Render(ctx, "", `\undefinedcommandxyz`, 150)
	latexErr, ok := err.(*LatexError)
	if !ok {
		t.Fatalf("expected *LatexError, got %T: %v", err, err)
	}
	if !strings.Contains(latexErr.Excerpt, "! Undefined control sequence") {
		t.Errorf("unexpected excerpt: %q", latexErr.Excerpt)
	}
}

func TestRenderPreWrapsBareMath(t *testing.T) {
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex not installed")
	}
	r := New("pdflatex", "pdftoppm", 30*time.Second, 2, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Bare math is wrapped up front, so a single compile succeeds.
	result, err := r.Render(ctx, "", `\int_0^1 x\,dx`, 150)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if len(result.PNG) == 0 || len(result.PDF) == 0 {
		t.Fatal("empty output")
	}
}

func TestRenderRetriesMissingDollarForMath(t *testing.T) {
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex not installed")
	}
	r := New("pdflatex", "pdftoppm", 30*time.Second, 2, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A math environment is not pre-wrapped; the compile fails once with
	// "Missing $ inserted" and the retry wraps the whole expression.
	result, err := r.Render(ctx, "", "\\begin{matrix}a&b\\\\c&d\\end{matrix} \\int_0^1 x\\,dx", 150)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if len(result.PNG) == 0 || len(result.PDF) == 0 {
		t.Fatal("empty output")
	}
}

func TestRenderNoRetryForMixedInput(t *testing.T) {
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex not installed")
	}
	r := New("pdflatex", "pdftoppm", 30*time.Second, 2, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Mixed text with a math-only command inside is not retried: the error
	// is reported to the user as-is.
	_, err := r.Render(ctx, "", "\\begin{itemize}\\item \\int\\end{itemize}", 150)
	latexErr, ok := err.(*LatexError)
	if !ok {
		t.Fatalf("expected *LatexError, got %T: %v", err, err)
	}
	if !strings.Contains(latexErr.Excerpt, "Missing $ inserted") {
		t.Errorf("unexpected excerpt: %q", latexErr.Excerpt)
	}
}

func TestDefaultPreamble(t *testing.T) {
	preamble := DefaultPreamble()
	for _, fragment := range []string{
		"\\documentclass[preview,varwidth,border=4pt]{standalone}",
		"\\usepackage{amsmath}",
		"\\usepackage{tikz}",
		"\\endofdump",
	} {
		if !strings.Contains(preamble, fragment) {
			t.Errorf("preamble missing %q", fragment)
		}
	}
}

func TestNormalizeExpression(t *testing.T) {
	cases := map[string]string{
		`\mathbb{ R }`:      `\mathbb{ R }`,
		`$\mathbb{R}$`:      `$\mathbb{R}$`,
		`$$x^2 + y^2$$`:     `$$x^2 + y^2$$`,
		`\[ \frac{1}{2} \]`: `\[ \frac{1}{2} \]`,
		`\(a+b\)`:           `\(a+b\)`,
		"  x  ":             `x`,
		`\begin{align} a &= b \\ c &= d \end{align}`: `\begin{align} a &= b \\ c &= d \end{align}`,
	}
	for input, want := range cases {
		got, err := normalizeExpression(input)
		if err != nil {
			t.Fatalf("normalizeExpression(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeExpression(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeExpression(`\input{/etc/passwd}`); err == nil {
		t.Fatal("dangerous command was accepted")
	}
	if _, err := normalizeExpression("   "); err == nil {
		t.Fatal("empty expression was accepted")
	}
}
