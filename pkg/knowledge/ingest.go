package knowledge

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	defaultFetchTimeout = 15 * time.Second
	maxFetchedBytes     = 5 << 20
	defaultPDFOCRDPI    = 200
	defaultPDFOCRPages  = 25
	defaultPDFOCRTime   = 10 * time.Minute
)

type Fetcher interface {
	Fetch(ctx context.Context, uri string) (FetchedSource, error)
}

type FetchedSource struct {
	URI          string
	CanonicalURI string
	Title        string
	Content      string
	ContentType  string
	ByteCount    int
	Extractor    string
	FetchedAt    time.Time
}

type HTTPFetcher struct {
	Client     *http.Client
	UserAgent  string
	Timeout    time.Duration
	Extraction TextExtractionOptions
}

type TextExtractionOptions struct {
	PDFOCR PDFOCROptions
}

type PDFOCROptions struct {
	Disabled         bool
	PDFToPPMCommand  string
	TesseractCommand string
	Language         string
	DPI              int
	MaxPages         int
	Timeout          time.Duration
}

func BuildSource(ctx context.Context, req AddSourceRequest, sourceID string, now time.Time, fetcher Fetcher) (Source, error) {
	kind := normalizeSourceKind(req.Kind)
	title := strings.TrimSpace(req.Title)
	uri := strings.TrimSpace(req.URI)
	content := cleanSourceContent(req.Content)
	source := Source{
		ID:        strings.TrimSpace(sourceID),
		Title:     title,
		Kind:      kind,
		URI:       uri,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
		Ingestion: SourceIngestion{
			State:     SourceStatusProcessing,
			Stage:     "received",
			StartedAt: now,
		},
	}
	if kind == SourceKindURL && content == "" {
		if uri == "" {
			return Source{}, fmt.Errorf("knowledge URL source URI is required")
		}
		if fetcher == nil {
			fetcher = HTTPFetcher{}
		}
		fetched, err := fetcher.Fetch(ctx, uri)
		if err != nil {
			source.Ingestion = SourceIngestion{
				State:       SourceStatusFailed,
				Stage:       "fetch",
				Error:       err.Error(),
				StartedAt:   now,
				CompletedAt: now,
			}
			source.Provenance = SourceProvenance{URI: uri, FetchedAt: now, Extractor: "http"}
			if source.Title == "" {
				source.Title = uri
			}
			return NormalizeSource(source)
		}
		source.URI = firstNonEmpty(fetched.CanonicalURI, fetched.URI, uri)
		source.Content = fetched.Content
		if source.Title == "" {
			source.Title = firstNonEmpty(fetched.Title, fetched.CanonicalURI, fetched.URI, uri)
		}
		source.Provenance = SourceProvenance{
			URI:          firstNonEmpty(fetched.URI, uri),
			CanonicalURI: fetched.CanonicalURI,
			ContentType:  fetched.ContentType,
			ByteCount:    fetched.ByteCount,
			FetchedAt:    fetched.FetchedAt,
			Extractor:    fetched.Extractor,
		}
	}
	source.Ingestion = SourceIngestion{
		State:     SourceStatusProcessing,
		Stage:     "text_extracted",
		Message:   "Source text extracted; waiting for language model analysis.",
		StartedAt: now,
	}
	if source.Provenance.Extractor == "" {
		source.Provenance.Extractor = extractorForKind(kind)
	}
	return NormalizeSource(source)
}

func (f HTTPFetcher) Fetch(ctx context.Context, uri string) (FetchedSource, error) {
	parsed, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return FetchedSource{}, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return FetchedSource{}, fmt.Errorf("only http and https URLs can be fetched")
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return FetchedSource{}, err
	}
	req.Header.Set("Accept", "text/html,text/plain,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("User-Agent", firstNonEmpty(f.UserAgent, "homelabd/1.0 Knowledge ingestion"))
	resp, err := client.Do(req)
	if err != nil {
		return FetchedSource{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchedSource{}, fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchedBytes+1))
	if err != nil {
		return FetchedSource{}, err
	}
	if len(body) > maxFetchedBytes {
		return FetchedSource{}, fmt.Errorf("source exceeds %d byte fetch limit", maxFetchedBytes)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	title, content, extractor, err := ExtractFetchedText(ctx, body, contentType, f.Extraction)
	if err != nil {
		return FetchedSource{}, err
	}
	if content == "" {
		return FetchedSource{}, fmt.Errorf("fetched source did not contain extractable text")
	}
	return FetchedSource{
		URI:          uri,
		CanonicalURI: resp.Request.URL.String(),
		Title:        title,
		Content:      content,
		ContentType:  contentType,
		ByteCount:    len(body),
		Extractor:    extractor,
		FetchedAt:    time.Now().UTC(),
	}, nil
}

var (
	titlePattern     = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	headingPattern   = regexp.MustCompile(`(?is)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	scriptPattern    = regexp.MustCompile(`(?is)<(script|style|noscript|svg)[^>]*>.*?</(script|style|noscript|svg)>`)
	tableCellPattern = regexp.MustCompile(`(?is)</?(td|th)[^>]*>`)
	blockTagPattern  = regexp.MustCompile(`(?is)</?(article|aside|blockquote|body|br|dd|div|dl|dt|figcaption|figure|footer|header|li|main|nav|ol|p|pre|section|table|tbody|td|tfoot|th|thead|tr|ul)[^>]*>`)
	tagPattern       = regexp.MustCompile(`(?is)<[^>]+>`)
	spacePattern     = regexp.MustCompile(`[ \t\r\f\v]+`)
	pdfStreamPattern = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
)

func ExtractFetchedText(ctx context.Context, body []byte, contentType string, options TextExtractionOptions) (string, string, string, error) {
	if strings.Contains(contentType, "pdf") || looksLikePDF(body) {
		content, extractor, err := extractPDFText(ctx, body, options.PDFOCR)
		if err != nil {
			return "", "", "", err
		}
		return "", content, extractor, nil
	}
	raw := string(body)
	if strings.Contains(contentType, "html") || looksLikeHTMLContent(raw) {
		title := ""
		if match := titlePattern.FindStringSubmatch(raw); len(match) > 1 {
			title = cleanWhitespace(html.UnescapeString(stripTags(match[1])))
		}
		text := htmlToStructuredText(raw)
		text = html.UnescapeString(text)
		text = cleanSourceContent(text)
		return title, text, "html", nil
	}
	if contentType == "" || strings.Contains(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") {
		return "", cleanSourceContent(raw), "plain-text", nil
	}
	return "", "", "", fmt.Errorf("unsupported content type %q", contentType)
}

func extractPDFText(ctx context.Context, body []byte, options PDFOCROptions) (string, string, error) {
	content, err := extractEmbeddedPDFText(body)
	if err == nil && content != "" {
		return content, "pdf", nil
	}
	if options.Disabled {
		if err != nil {
			return "", "", err
		}
		return "", "", fmt.Errorf("PDF source did not contain extractable text")
	}
	ocrText, ocrErr := extractPDFTextWithOCR(ctx, body, options)
	if ocrErr != nil {
		if err != nil {
			return "", "", fmt.Errorf("%v; %v", err, ocrErr)
		}
		return "", "", ocrErr
	}
	return ocrText, "pdf+ocr", nil
}

func extractEmbeddedPDFText(body []byte) (string, error) {
	var builder strings.Builder
	streams := pdfStreamPattern.FindAllSubmatchIndex(body, -1)
	for _, match := range streams {
		if len(match) < 4 {
			continue
		}
		dictStart := match[0] - 512
		if dictStart < 0 {
			dictStart = 0
		}
		dict := string(body[dictStart:match[0]])
		stream := body[match[2]:match[3]]
		if strings.Contains(dict, "/FlateDecode") {
			inflated, err := inflatePDFStream(stream)
			if err != nil {
				continue
			}
			stream = inflated
		}
		if text := extractPDFTextOperands(stream); text != "" {
			builder.WriteByte(' ')
			builder.WriteString(text)
		}
	}
	if builder.Len() == 0 {
		builder.WriteString(extractPDFTextOperands(body))
	}
	content := cleanExtractedText(builder.String())
	if content == "" {
		return "", fmt.Errorf("extract PDF text: no text operators found")
	}
	return content, nil
}

func extractPDFTextWithOCR(ctx context.Context, body []byte, options PDFOCROptions) (string, error) {
	options = normalizedPDFOCROptions(options)
	pdftoppm, err := exec.LookPath(options.PDFToPPMCommand)
	if err != nil {
		return "", fmt.Errorf("PDF OCR unavailable: %s not found in PATH", options.PDFToPPMCommand)
	}
	tesseract, err := exec.LookPath(options.TesseractCommand)
	if err != nil {
		return "", fmt.Errorf("PDF OCR unavailable: %s not found in PATH", options.TesseractCommand)
	}
	workDir, err := os.MkdirTemp("", "homelabd-pdf-ocr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workDir)
	pdfPath := filepath.Join(workDir, "source.pdf")
	if err := os.WriteFile(pdfPath, body, 0o600); err != nil {
		return "", err
	}
	ocrCtx := ctx
	cancel := func() {}
	if options.Timeout > 0 {
		ocrCtx, cancel = context.WithTimeout(ctx, options.Timeout)
	}
	defer cancel()
	prefix := filepath.Join(workDir, "page")
	args := []string{
		"-r", strconv.Itoa(options.DPI),
		"-png",
		"-f", "1",
		"-l", strconv.Itoa(options.MaxPages),
		pdfPath,
		prefix,
	}
	if output, err := runExtractionCommand(ocrCtx, pdftoppm, args...); err != nil {
		return "", fmt.Errorf("PDF OCR rasterisation failed: %s", commandErrorMessage(err, output))
	}
	images, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return "", err
	}
	sort.Strings(images)
	if len(images) == 0 {
		return "", fmt.Errorf("PDF OCR rasterisation produced no page images")
	}
	var builder strings.Builder
	for _, image := range images {
		args := []string{image, "stdout", "-l", options.Language, "--dpi", strconv.Itoa(options.DPI)}
		output, err := runExtractionCommand(ocrCtx, tesseract, args...)
		if err != nil {
			return "", fmt.Errorf("PDF OCR recognition failed: %s", commandErrorMessage(err, output))
		}
		if builder.Len()+len(output.Stdout) > maxFetchedBytes {
			return "", fmt.Errorf("PDF OCR text exceeds %d byte limit", maxFetchedBytes)
		}
		builder.WriteByte(' ')
		builder.Write(output.Stdout)
	}
	text := cleanExtractedText(builder.String())
	if text == "" {
		return "", fmt.Errorf("PDF OCR completed but no text was recognised")
	}
	return text, nil
}

func normalizedPDFOCROptions(options PDFOCROptions) PDFOCROptions {
	if strings.TrimSpace(options.PDFToPPMCommand) == "" {
		options.PDFToPPMCommand = "pdftoppm"
	}
	if strings.TrimSpace(options.TesseractCommand) == "" {
		options.TesseractCommand = "tesseract"
	}
	if strings.TrimSpace(options.Language) == "" {
		options.Language = "eng"
	}
	if options.DPI <= 0 {
		options.DPI = defaultPDFOCRDPI
	}
	if options.MaxPages <= 0 {
		options.MaxPages = defaultPDFOCRPages
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultPDFOCRTime
	}
	return options
}

type extractionCommandOutput struct {
	Stdout []byte
	Stderr []byte
}

func runExtractionCommand(ctx context.Context, name string, args ...string) (extractionCommandOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := extractionCommandOutput{Stdout: bytes.TrimSpace(stdout.Bytes()), Stderr: bytes.TrimSpace(stderr.Bytes())}
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}

func commandErrorMessage(err error, output extractionCommandOutput) string {
	var parts []string
	if len(output.Stdout) > 0 {
		parts = append(parts, string(output.Stdout))
	}
	if len(output.Stderr) > 0 {
		parts = append(parts, string(output.Stderr))
	}
	message := strings.TrimSpace(strings.Join(parts, "\n"))
	if message == "" {
		message = err.Error()
	}
	return message
}

func inflatePDFStream(stream []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(stream))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxFetchedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFetchedBytes {
		return nil, fmt.Errorf("PDF extracted text exceeds %d byte limit", maxFetchedBytes)
	}
	return data, nil
}

func extractPDFTextOperands(data []byte) string {
	var values []string
	for index := 0; index < len(data); index++ {
		switch data[index] {
		case '(':
			value, next := readPDFLiteralString(data, index+1)
			if value != "" {
				values = append(values, value)
			}
			index = next
		case '<':
			if index+1 < len(data) && data[index+1] == '<' {
				continue
			}
			value, next := readPDFHexString(data, index+1)
			if value != "" {
				values = append(values, value)
			}
			index = next
		}
	}
	return strings.Join(values, " ")
}

func readPDFLiteralString(data []byte, index int) (string, int) {
	var builder strings.Builder
	depth := 1
	for index < len(data) {
		value := data[index]
		index++
		if value == '\\' {
			decoded, next := decodePDFEscape(data, index)
			if decoded != 0 {
				builder.WriteByte(decoded)
			}
			index = next
			continue
		}
		if value == '(' {
			depth++
		}
		if value == ')' {
			depth--
			if depth == 0 {
				return builder.String(), index
			}
		}
		builder.WriteByte(value)
	}
	return builder.String(), len(data)
}

func decodePDFEscape(data []byte, index int) (byte, int) {
	if index >= len(data) {
		return '\\', index
	}
	value := data[index]
	index++
	switch value {
	case 'n':
		return '\n', index
	case 'r':
		return '\r', index
	case 't':
		return '\t', index
	case 'b':
		return '\b', index
	case 'f':
		return '\f', index
	case '(', ')', '\\':
		return value, index
	case '\n':
		return 0, index
	case '\r':
		if index < len(data) && data[index] == '\n' {
			index++
		}
		return 0, index
	}
	if value >= '0' && value <= '7' {
		start := index - 1
		for index < len(data) && index-start < 3 && data[index] >= '0' && data[index] <= '7' {
			index++
		}
		parsed, err := strconv.ParseInt(string(data[start:index]), 8, 16)
		if err == nil {
			return byte(parsed), index
		}
	}
	return value, index
}

func readPDFHexString(data []byte, index int) (string, int) {
	start := index
	for index < len(data) && data[index] != '>' {
		index++
	}
	raw := strings.Map(func(value rune) rune {
		if value == ' ' || value == '\n' || value == '\r' || value == '\t' || value == '\f' {
			return -1
		}
		return value
	}, string(data[start:index]))
	if len(raw)%2 == 1 {
		raw += "0"
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return "", index
	}
	if len(decoded) >= 2 && decoded[0] == 0xfe && decoded[1] == 0xff {
		return decodeUTF16BE(decoded[2:]), index
	}
	return string(decoded), index
}

func decodeUTF16BE(data []byte) string {
	values := make([]uint16, 0, len(data)/2)
	for index := 0; index+1 < len(data); index += 2 {
		values = append(values, uint16(data[index])<<8|uint16(data[index+1]))
	}
	return string(utf16.Decode(values))
}

func stripTags(value string) string {
	return tagPattern.ReplaceAllString(value, " ")
}

func htmlToStructuredText(value string) string {
	value = scriptPattern.ReplaceAllString(value, " ")
	value = headingPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := headingPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return "\n" + cleanWhitespace(stripTags(match)) + "\n"
		}
		level, _ := strconv.Atoi(parts[1])
		if level <= 0 || level > 6 {
			level = 2
		}
		text := cleanWhitespace(html.UnescapeString(stripTags(parts[2])))
		if text == "" {
			return "\n"
		}
		return "\n" + strings.Repeat("#", level) + " " + text + "\n"
	})
	value = tableCellPattern.ReplaceAllString(value, " | ")
	value = blockTagPattern.ReplaceAllString(value, "\n")
	value = stripTags(value)
	return value
}

func cleanExtractedText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = spacePattern.ReplaceAllString(value, " ")
	return cleanWhitespace(value)
}

func looksLikeHTMLContent(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<body") || strings.Contains(lower, "<article")
}

func looksLikePDF(body []byte) bool {
	return len(body) >= 5 && string(body[:5]) == "%PDF-"
}

func extractorForKind(kind string) string {
	switch normalizeSourceKind(kind) {
	case SourceKindURL:
		return "url"
	case SourceKindFile:
		return "file-text"
	case SourceKindNote:
		return "note"
	case SourceKindEmail:
		return "email"
	case SourceKindMCP:
		return "connected-resource"
	default:
		return "plain-text"
	}
}
