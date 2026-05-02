package knowledge

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultFetchTimeout = 15 * time.Second
	maxFetchedBytes     = 5 << 20
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
	Client    *http.Client
	UserAgent string
	Timeout   time.Duration
}

func BuildSource(ctx context.Context, req AddSourceRequest, sourceID string, now time.Time, fetcher Fetcher) (Source, error) {
	kind := normalizeSourceKind(req.Kind)
	title := strings.TrimSpace(req.Title)
	uri := strings.TrimSpace(req.URI)
	content := cleanWhitespace(req.Content)
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
		State:       SourceStatusReady,
		Stage:       "indexed",
		Message:     "Source is indexed and available for retrieval.",
		StartedAt:   now,
		CompletedAt: now,
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
	title, content, extractor, err := extractFetchedText(body, contentType)
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
	titlePattern  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	scriptPattern = regexp.MustCompile(`(?is)<(script|style|noscript|svg)[^>]*>.*?</(script|style|noscript|svg)>`)
	tagPattern    = regexp.MustCompile(`(?is)<[^>]+>`)
	spacePattern  = regexp.MustCompile(`[ \t\r\f\v]+`)
)

func extractFetchedText(body []byte, contentType string) (string, string, string, error) {
	raw := string(body)
	if strings.Contains(contentType, "pdf") || looksLikePDF(body) {
		return "", "", "", fmt.Errorf("PDF URL ingestion is not available yet; add extracted text or a text file source")
	}
	if strings.Contains(contentType, "html") || looksLikeHTMLContent(raw) {
		title := ""
		if match := titlePattern.FindStringSubmatch(raw); len(match) > 1 {
			title = cleanWhitespace(html.UnescapeString(stripTags(match[1])))
		}
		text := scriptPattern.ReplaceAllString(raw, " ")
		text = stripTags(text)
		text = html.UnescapeString(text)
		text = cleanExtractedText(text)
		return title, text, "html", nil
	}
	if contentType == "" || strings.Contains(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") {
		return "", cleanExtractedText(raw), "plain-text", nil
	}
	return "", "", "", fmt.Errorf("unsupported content type %q", contentType)
}

func stripTags(value string) string {
	return tagPattern.ReplaceAllString(value, " ")
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
	default:
		return "plain-text"
	}
}
