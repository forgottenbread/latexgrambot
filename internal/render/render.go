// Package render turns LaTeX expressions into PNG and PDF files using a
// locally installed TeX Live (pdflatex) and poppler (pdftoppm). No network
// service is involved.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"latexgrambot/internal/rich"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var renderTracer = otel.Tracer("latexgrambot/render")

const (
	// minRenderedSide is the minimum size of the smallest PNG side. Small
	// formulas rasterize to a few hundred pixels at any DPI; Telegram then
	// upscales and JPEG-recompresses them, blurring thin glyphs. Upscaling
	// the raster beforehand keeps the final picture crisp.
	minRenderedSide = 2560

	// maxRenderedSide guards against pathologically tall formulas.
	maxRenderedSide = 4096

	// inlineThumbnailSide is Telegram's conventional maximum thumbnail side.
	inlineThumbnailSide = 320
)

// documentClass is the fixed preview layout: the standalone class crops
// tightly around the content.
const documentClass = `\documentclass[preview,varwidth,border=4pt]{standalone}`

// defaultPackages is inserted into the document when the user has no custom
// preamble: the broad standard package set so documents compile without a
// custom preamble.
const defaultPackages = `\usepackage[utf8]{inputenc}
\usepackage[T1]{fontenc}
\usepackage{lmodern}
\usepackage{textcomp}
\usepackage{amsmath}
\usepackage{amssymb}
\usepackage{amsfonts}
\usepackage{amsthm}
\usepackage{amscd}
\usepackage{mathtools}
\usepackage{mathrsfs}
\usepackage{bm}
\usepackage{cancel}
\usepackage{braket}
\usepackage{siunitx}
\usepackage{mhchem}
\usepackage{chemfig}
\usepackage{systeme}
\usepackage{polynom}
\usepackage{wasysym}
\usepackage{stmaryrd}
\usepackage{pifont}
\usepackage{eurosym}
\usepackage{graphicx}
\usepackage{xcolor}
\usepackage{colortbl}
\usepackage{tikz}
\usepackage{pgfplots}
\pgfplotsset{compat=1.18}
\usepackage{tikz-cd}
\usepackage{tcolorbox}
\usepackage{qrcode}
\usepackage{float}
\usepackage{array}
\usepackage{multirow}
\usepackage{makecell}
\usepackage{booktabs}
\usepackage{tabularx}
\usepackage{longtable}
\usepackage{multicol}
\usepackage{enumitem}
\usepackage{caption}
\usepackage{subcaption}
\usepackage{wrapfig}
\usepackage{rotating}
\usepackage[normalem]{ulem}
\usepackage{soul}
\usepackage{listings}
\usepackage{algorithm}
\usepackage{algpseudocode}
\usepackage{microtype}
\usepackage{xspace}
\usepackage{etoolbox}
\usepackage{hyperref}`

// DefaultPreamble returns the preamble file used at image build time to
// preload the default class and packages into the "latexgrambot" TeX
// format, so renders no longer pay the package-load cost.
func DefaultPreamble() string {
	return documentClass + "\n" + defaultPackages + "\n\\endofdump\n"
}

// Result carries the compiled output. JPEG is used for URL-based Telegram
// inline photos, while PNG is used for direct chat replies.
type Result struct {
	PNG        []byte
	JPEG       []byte
	JPEGWidth  int
	JPEGHeight int
	Thumbnail  []byte
	PDF        []byte
}

// LatexError wraps a LaTeX compile failure and carries the log excerpt shown
// to the user.
type LatexError struct {
	Excerpt string
}

func (e *LatexError) Error() string { return "latex compilation failed" }

// Renderer compiles LaTeX documents locally.
type Renderer struct {
	Pdflatex string
	Pdftoppm string
	Timeout  time.Duration
	WorkDir  string // base directory for temp dirs; os.TempDir() when empty

	// Format is the preloaded TeX format (built from DefaultPreamble) used
	// when the default preamble is in effect. Empty disables it.
	Format string

	sem chan struct{}
}

// New creates a Renderer that allows at most maxConcurrent simultaneous
// pdflatex runs.
func New(pdflatex, pdftoppm string, timeout time.Duration, maxConcurrent int, workDir string) *Renderer {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Renderer{
		Pdflatex: pdflatex,
		Pdftoppm: pdftoppm,
		Timeout:  timeout,
		WorkDir:  workDir,
		sem:      make(chan struct{}, maxConcurrent),
	}
}

// dangerousCommands are TeX primitives that read or write files; user input
// must never reach them.
var dangerousCommands = regexp.MustCompile(`\\(input|include|openin|openout|write|read|catcode|newwrite|immediate)\b`)

// normalizeExpression validates and trims user LaTeX without changing its
// mode. The bot has already applied the RICH_DEFAULT_MATH policy before it
// calls the renderer; wrapping everything here would incorrectly place text
// commands such as \hyperlink and \section inside display math.
func normalizeExpression(expression string) (string, error) {
	expr := strings.TrimSpace(expression)
	if expr == "" {
		return "", &LatexError{Excerpt: "the expression is empty"}
	}
	if dangerousCommands.MatchString(expr) {
		return "", &LatexError{Excerpt: "unsupported TeX command in the expression"}
	}
	return expr, nil
}

// stripOuterMathDelimiters removes matching outer math delimiters, so a
// forced math retry never nests delimiters.
func stripOuterMathDelimiters(expr string) string {
	for _, pair := range [][2]string{{`$$`, `$$`}, {`$`, `$`}, {`\[`, `\]`}, {`\(`, `\)`}} {
		if len(expr) >= len(pair[0])+len(pair[1]) &&
			strings.HasPrefix(expr, pair[0]) && strings.HasSuffix(expr, pair[1]) {
			return strings.TrimSpace(expr[len(pair[0]) : len(expr)-len(pair[1])])
		}
	}
	return expr
}

// Formats selects which outputs a render produces.
type Formats uint8

const (
	FormatPNG Formats = 1 << iota
	FormatJPEG
	FormatPDF
	FormatThumbnail
)

// Render compiles the expression and produces every format.
func (r *Renderer) Render(ctx context.Context, preamble, expression string, dpi int) (*Result, error) {
	return r.RenderFormats(ctx, preamble, expression, dpi, FormatPNG|FormatJPEG|FormatPDF|FormatThumbnail)
}

// RenderFormats compiles the expression inside the given preamble and
// produces only the requested formats, so callers never pay for
// rasterization they do not use. The expression is normalised (math
// delimiters stripped, plain expressions wrapped in math mode) and inserted
// between \begin{document} and \end{document}.
func (r *Renderer) RenderFormats(ctx context.Context, preamble, expression string, dpi int, formats Formats) (result *Result, err error) {
	normalized, err := normalizeExpression(expression)
	if err != nil {
		return nil, err
	}
	ctx, span := renderTracer.Start(ctx, "render",
		trace.WithAttributes(
			attribute.Int("dpi", dpi),
			attribute.Int("expression.length", len(expression)),
			attribute.Int("expression.normalized_length", len(normalized)),
			attribute.Int("preamble.length", len(preamble)),
		),
	)
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "render failed")
		}
	}()

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	workdir, err := os.MkdirTemp(r.WorkDir, "latexgrambot-")
	if err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	texPath := filepath.Join(workdir, "document.tex")
	// Preloaded format: with the default preamble the class and packages
	// are already in the format, so only the body is compiled.
	useFormat := r.Format != "" && strings.TrimSpace(preamble) == ""
	compile := func(body string) error {
		document := buildDocument(preamble, body)
		if useFormat {
			document = "\\begin{document}\n" + body + "\n\\end{document}\n"
		}
		if err := os.WriteFile(texPath, []byte(document), 0o600); err != nil {
			return fmt.Errorf("write document: %w", err)
		}
		// -no-shell-escape is the default, spelled out here on purpose: the
		// \write18 escapes must never be enabled for user-supplied LaTeX.
		args := []string{
			"-interaction=nonstopmode", "-halt-on-error", "-no-shell-escape",
			"-output-directory=" + workdir,
		}
		if useFormat {
			args = append(args, "-fmt="+r.Format)
		}
		return r.run(ctx, workdir, r.Pdflatex, append(args, "document.tex")...)
	}

	// Entirely-math expressions are put into math mode up front: waiting
	// for the "Missing $ inserted" error would compile every formula twice.
	firstAttempt := normalized
	if rich.IsMathExpression(expression) && !rich.ContainsMathEnvironment(expression) {
		firstAttempt = `\[` + stripOuterMathDelimiters(strings.TrimSpace(normalized)) + `\]`
	}

	if err := compile(firstAttempt); err != nil {
		logContent, readErr := os.ReadFile(filepath.Join(workdir, "document.log"))
		if readErr != nil {
			return nil, err
		}
		// One retry for cases the up-front check missed: when the compiler
		// only complained about missing math delimiters and the whole
		// expression is math. If the retry fails too, the original error is
		// reported so the user decides what to do.
		if strings.Contains(string(logContent), "Missing $ inserted") &&
			rich.IsMathExpression(expression) && firstAttempt == normalized {
			forced := `\[` + stripOuterMathDelimiters(strings.TrimSpace(expression)) + `\]`
			for _, name := range []string{"document.aux", "document.log", "document.pdf"} {
				_ = os.Remove(filepath.Join(workdir, name))
			}
			if retryErr := compile(forced); retryErr != nil {
				return nil, &LatexError{Excerpt: extractError(string(logContent))}
			}
		} else {
			return nil, &LatexError{Excerpt: extractError(string(logContent))}
		}
	}

	pdfPath := filepath.Join(workdir, "document.pdf")
	result = &Result{}
	if formats&FormatPDF != 0 {
		result.PDF, err = os.ReadFile(pdfPath)
		if err != nil {
			return nil, fmt.Errorf("read compiled pdf: %w", err)
		}
	}
	if formats&FormatPNG != 0 {
		pngPath := filepath.Join(workdir, "document.png")
		if err := r.renderRaster(ctx, workdir, dpi, pdfPath, pngPath, "png"); err != nil {
			return nil, err
		}
		result.PNG, err = os.ReadFile(pngPath)
		if err != nil {
			return nil, fmt.Errorf("read rendered png: %w", err)
		}
	}
	if formats&FormatJPEG != 0 {
		jpegPath := filepath.Join(workdir, "document.jpg")
		if err := r.renderRaster(ctx, workdir, dpi, pdfPath, jpegPath, "jpeg"); err != nil {
			return nil, err
		}
		result.JPEGWidth, result.JPEGHeight, err = imageSize(jpegPath)
		if err != nil {
			return nil, fmt.Errorf("inspect rendered jpeg: %w", err)
		}
		result.JPEG, err = os.ReadFile(jpegPath)
		if err != nil {
			return nil, fmt.Errorf("read rendered jpeg: %w", err)
		}
	}
	if formats&FormatThumbnail != 0 {
		thumbnailPath := filepath.Join(workdir, "document-thumb.jpg")
		if err := r.renderThumbnail(ctx, workdir, dpi, pdfPath, thumbnailPath); err != nil {
			return nil, err
		}
		result.Thumbnail, err = os.ReadFile(thumbnailPath)
		if err != nil {
			return nil, fmt.Errorf("read rendered thumbnail: %w", err)
		}
	}
	return result, nil
}

// renderRaster rasterizes the first PDF page to a PNG or JPEG, re-rendering
// it upscaled when the natural size is too small for a crisp Telegram
// display. Rasterizing the JPEG directly avoids decoding and re-encoding a
// full-size PNG in Go.
func (r *Renderer) renderRaster(ctx context.Context, workdir string, dpi int, pdfPath, outPath, format string) error {
	base := filepath.Join(workdir, "document")
	args := []string{"-" + format, "-r", strconv.Itoa(dpi), "-singlefile", "-f", "1", "-l", "1"}
	if format == "jpeg" {
		// Baseline JPEGs avoid partial/progressive decode artifacts in some
		// Telegram clients while remaining comfortably below the 5 MB inline
		// photo limit for normal formula renders.
		args = append(args, "-jpegopt", "quality=90,progressive=n,optimize=y")
	}
	if err := r.run(ctx, workdir, r.Pdftoppm, append(args, pdfPath, base)...); err != nil {
		return err
	}
	width, height, err := imageSize(outPath)
	if err != nil {
		return fmt.Errorf("inspect rendered %s: %w", format, err)
	}
	small := min(width, height)
	large := max(width, height)
	scale := 1.0
	if small > 0 && small < minRenderedSide {
		scale = float64(minRenderedSide) / float64(small)
		if scale*float64(large) > maxRenderedSide {
			scale = float64(maxRenderedSide) / float64(large)
		}
	}
	if scale <= 1 {
		return nil
	}
	scaledWidth := int(math.Round(float64(width) * scale))
	args = append(args,
		"-scale-to-x", strconv.Itoa(scaledWidth), "-scale-to-y", "-1")
	return r.run(ctx, workdir, r.Pdftoppm, append(args, pdfPath, base)...)
}

// renderThumbnail creates the dedicated small JPEG used only by Telegram's
// inline-result picker. It preserves the formula's aspect ratio and only
// shrinks oversized previews; short expressions are never enlarged.
func (r *Renderer) renderThumbnail(ctx context.Context, workdir string, dpi int, pdfPath, outPath string) error {
	base := strings.TrimSuffix(outPath, filepath.Ext(outPath))
	args := []string{
		"-jpeg", "-jpegopt", "quality=82,progressive=n,optimize=y",
		"-r", strconv.Itoa(dpi), "-singlefile", "-f", "1", "-l", "1",
	}
	if err := r.run(ctx, workdir, r.Pdftoppm, append(args, pdfPath, base)...); err != nil {
		return fmt.Errorf("render inline thumbnail: %w", err)
	}
	width, height, err := imageSize(outPath)
	if err != nil {
		return fmt.Errorf("inspect inline thumbnail: %w", err)
	}
	// Only shrink oversized previews. pdftoppm's -scale-to also upscales
	// small pages, which would unnecessarily enlarge short expressions.
	if width > inlineThumbnailSide || height > inlineThumbnailSide {
		scaledArgs := append(args, "-scale-to", strconv.Itoa(inlineThumbnailSide))
		if err := r.run(ctx, workdir, r.Pdftoppm, append(scaledArgs, pdfPath, base)...); err != nil {
			return fmt.Errorf("scale inline thumbnail: %w", err)
		}
		width, height, err = imageSize(outPath)
		if err != nil {
			return fmt.Errorf("inspect scaled inline thumbnail: %w", err)
		}
	}
	if width > inlineThumbnailSide || height > inlineThumbnailSide {
		return fmt.Errorf("inline thumbnail is unexpectedly large: %dx%d", width, height)
	}
	return nil
}

func imageSize(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

// buildDocument assembles the full .tex source. User preamble additions are
// loaded after the broad default package set, so setting custom definitions
// cannot accidentally remove core packages such as hyperref.
func buildDocument(preamble, expression string) string {
	pkgs := defaultPackages
	if strings.TrimSpace(preamble) != "" {
		pkgs += "\n" + strings.TrimSpace(preamble)
	}
	return documentClass + "\n" +
		pkgs + "\n" +
		"\\begin{document}\n" +
		expression + "\n" +
		"\\end{document}\n"
}

// extractError pulls the first "! ..." error line and its "l.NN ..." context
// line from the pdflatex log.
func extractError(log string) string {
	var first, context string
	for _, line := range strings.Split(log, "\n") {
		trimmed := strings.TrimSpace(line)
		if first == "" && strings.HasPrefix(trimmed, "!") {
			first = trimmed
			continue
		}
		if first != "" && strings.HasPrefix(trimmed, "l.") {
			context = trimmed
			break
		}
	}
	if first == "" {
		return "compilation failed without an explicit error message"
	}
	if context == "" {
		return first
	}
	return first + "\n" + context
}

// run executes a command in dir, inheriting the timeout from ctx. Home,
// TMPDIR and TEXMFOUTPUT point into the workdir so TeX never needs to write
// outside it.
func (r *Renderer) run(ctx context.Context, dir, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+dir,
		"TMPDIR="+dir,
		"TEXMFOUTPUT="+dir,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", filepath.Base(bin), ctx.Err())
		}
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return fmt.Errorf("%s is not available: %w", bin, execErr)
		}
		return fmt.Errorf("%s failed: %w (%s)", filepath.Base(bin), err, firstLine(output.String()))
	}
	return nil
}

// firstLine returns the first non-empty line of s, or an empty string.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
