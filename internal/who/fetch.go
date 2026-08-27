package who

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var blockedIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// IPResolver is the small DNS seam used to protect requests against SSRF and
// make that protection independently testable.
type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// IPResolverFunc adapts a function to IPResolver.
type IPResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (fn IPResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return fn(ctx, host)
}

// FetchOptions configures a safe WHO CSV downloader. It never accepts a custom
// HTTP transport: that would make the DNS/IP safeguards optional.
type FetchOptions struct {
	Resolver IPResolver
	Limits   Limits
	Timeout  time.Duration
}

// Fetcher downloads a validated WHO CSV with network limits enforced on every
// DNS resolution and redirect.
type Fetcher struct {
	resolver IPResolver
	limits   Limits
	timeout  time.Duration
}

// NewFetcher creates a downloader with a 45-second whole-request timeout.
func NewFetcher(options FetchOptions) Fetcher {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if options.Timeout <= 0 {
		options.Timeout = 45 * time.Second
	}
	return Fetcher{
		resolver: resolver,
		limits:   options.Limits.normalized(),
		timeout:  options.Timeout,
	}
}

// FetchedArtifact is the bounded raw artifact used to build an import preview.
type FetchedArtifact struct {
	URL           string
	Bytes         []byte
	SHA256        string
	AccessedAt    time.Time
	ETag          string
	LastModified  string
	ContentType   string
	ContentLength int64
}

// DownloadDiscovery is the safe, bounded result of inspecting a WHO indicator
// page for its one matching DataDot CSV anchor.
type DownloadDiscovery struct {
	Page          IndicatorPage
	Download      DownloadLink
	AccessedAt    time.Time
	ETag          string
	LastModified  string
	ContentType   string
	ContentLength int64
}

// FetchedIndicator combines the discovered source page with the CSV artifact.
type FetchedIndicator struct {
	Discovery DownloadDiscovery
	Artifact  FetchedArtifact
}

// fetchDownload downloads an already-discovered, validated DataDot CSV. It
// follows only a small number of equally-valid WHO blob redirects and never
// delegates DNS to a proxy.
func (fetcher Fetcher) fetchDownload(ctx context.Context, link DownloadLink) (FetchedArtifact, error) {
	validated, err := ValidateDownloadURL(link.URL)
	if err != nil || validated.IndicatorID != link.IndicatorID {
		return FetchedArtifact{}, fmt.Errorf("%w: discovered download link is not canonical", ErrInvalidDownloadURL)
	}
	link = validated
	fetcher = fetcher.normalized()
	if _, err := ResolvePublicHost(ctx, fetcher.resolver, WHOBlobHost); err != nil {
		return FetchedArtifact{}, err
	}

	client := fetcher.httpClient(WHOBlobHost, func(raw string) error {
		_, err := ValidateDownloadURL(raw)
		return err
	})

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
	if err != nil {
		return FetchedArtifact{}, fmt.Errorf("build WHO download request: %w", err)
	}
	request.Header.Set("Accept", "text/csv,application/csv;q=0.9,application/octet-stream;q=0.8")
	response, err := client.Do(request)
	if err != nil {
		return FetchedArtifact{}, fmt.Errorf("download WHO CSV: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return FetchedArtifact{}, fmt.Errorf("download WHO CSV: unexpected status %s", response.Status)
	}
	finalLink, err := ValidateDownloadURL(response.Request.URL.String())
	if err != nil {
		return FetchedArtifact{}, fmt.Errorf("validate redirected WHO CSV URL: %w", err)
	}
	if finalLink.IndicatorID != link.IndicatorID {
		return FetchedArtifact{}, fmt.Errorf("%w: redirected download %s does not match requested %s", ErrInvalidDownloadURL, finalLink.IndicatorID, link.IndicatorID)
	}
	if !isCSVContentType(response.Header.Get("Content-Type")) {
		return FetchedArtifact{}, fmt.Errorf("download WHO CSV: unexpected content type %q", response.Header.Get("Content-Type"))
	}
	if response.ContentLength > fetcher.limits.MaxBytes {
		return FetchedArtifact{}, fmt.Errorf("%w: declared %d bytes (limit %d)", ErrArtifactTooLarge, response.ContentLength, fetcher.limits.MaxBytes)
	}

	bytes, err := readCapped(response.Body, fetcher.limits.MaxBytes)
	if err != nil {
		return FetchedArtifact{}, err
	}
	sum := sha256.Sum256(bytes)
	return FetchedArtifact{
		URL:           finalLink.URL,
		Bytes:         bytes,
		SHA256:        fmt.Sprintf("%x", sum),
		AccessedAt:    time.Now().UTC(),
		ETag:          response.Header.Get("ETag"),
		LastModified:  response.Header.Get("Last-Modified"),
		ContentType:   response.Header.Get("Content-Type"),
		ContentLength: response.ContentLength,
	}, nil
}

// DiscoverDownload fetches the submitted canonical indicator page and returns
// its matching approved CSV anchor. It never derives a blob URL from the ID.
func (fetcher Fetcher) DiscoverDownload(ctx context.Context, rawPageURL string) (DownloadDiscovery, error) {
	page, err := ValidateIndicatorPageURL(rawPageURL)
	if err != nil {
		return DownloadDiscovery{}, err
	}
	fetcher = fetcher.normalized()
	if _, err := ResolvePublicHost(ctx, fetcher.resolver, WHODataHost); err != nil {
		return DownloadDiscovery{}, err
	}

	client := fetcher.httpClient(WHODataHost, func(raw string) error {
		_, err := ValidateIndicatorPageURL(raw)
		return err
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, page.URL, nil)
	if err != nil {
		return DownloadDiscovery{}, fmt.Errorf("build WHO indicator-page request: %w", err)
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")
	response, err := client.Do(request)
	if err != nil {
		return DownloadDiscovery{}, fmt.Errorf("download WHO indicator page: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return DownloadDiscovery{}, fmt.Errorf("download WHO indicator page: unexpected status %s", response.Status)
	}
	if !isHTMLContentType(response.Header.Get("Content-Type")) {
		return DownloadDiscovery{}, fmt.Errorf("download WHO indicator page: unexpected content type %q", response.Header.Get("Content-Type"))
	}
	if response.ContentLength > fetcher.limits.MaxBytes {
		return DownloadDiscovery{}, fmt.Errorf("%w: indicator page declared %d bytes (limit %d)", ErrArtifactTooLarge, response.ContentLength, fetcher.limits.MaxBytes)
	}
	body, err := readCapped(response.Body, fetcher.limits.MaxBytes)
	if err != nil {
		return DownloadDiscovery{}, err
	}
	finalPage, err := ValidateIndicatorPageURL(response.Request.URL.String())
	if err != nil {
		return DownloadDiscovery{}, fmt.Errorf("validate redirected WHO indicator page: %w", err)
	}
	if finalPage.IndicatorID != page.IndicatorID {
		return DownloadDiscovery{}, fmt.Errorf("%w: redirected indicator %s does not match requested %s", ErrInvalidIndicatorURL, finalPage.IndicatorID, page.IndicatorID)
	}
	download, err := discoverDownloadAnchor(finalPage, body)
	if err != nil {
		return DownloadDiscovery{}, err
	}
	return DownloadDiscovery{
		Page:          finalPage,
		Download:      download,
		AccessedAt:    time.Now().UTC(),
		ETag:          response.Header.Get("ETag"),
		LastModified:  response.Header.Get("Last-Modified"),
		ContentType:   response.Header.Get("Content-Type"),
		ContentLength: response.ContentLength,
	}, nil
}

// FetchFromIndicatorPage is the only convenience flow for a submitted WHO page:
// discover the actual anchor first, then download that exact validated link.
func (fetcher Fetcher) FetchFromIndicatorPage(ctx context.Context, rawPageURL string) (FetchedIndicator, error) {
	discovery, err := fetcher.DiscoverDownload(ctx, rawPageURL)
	if err != nil {
		return FetchedIndicator{}, err
	}
	artifact, err := fetcher.fetchDownload(ctx, discovery.Download)
	if err != nil {
		return FetchedIndicator{}, err
	}
	return FetchedIndicator{Discovery: discovery, Artifact: artifact}, nil
}

func (fetcher Fetcher) normalized() Fetcher {
	if fetcher.resolver == nil {
		fetcher.resolver = net.DefaultResolver
	}
	fetcher.limits = fetcher.limits.normalized()
	if fetcher.timeout <= 0 {
		fetcher.timeout = 45 * time.Second
	}
	return fetcher
}

func (fetcher Fetcher) httpClient(host string, validateRedirect func(string) error) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 15 * time.Second
	transport.DialContext = fetcher.dialContext(host)
	return &http.Client{
		Transport: transport,
		Timeout:   fetcher.timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > fetcher.limits.MaxRedirects {
				return fmt.Errorf("WHO request exceeded %d redirects", fetcher.limits.MaxRedirects)
			}
			if err := validateRedirect(request.URL.String()); err != nil {
				return err
			}
			_, err := ResolvePublicHost(request.Context(), fetcher.resolver, request.URL.Hostname())
			return err
		},
	}
}

func (fetcher Fetcher) dialContext(expectedHost string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("split WHO network address: %w", err)
		}
		if !strings.EqualFold(host, expectedHost) || port != "443" {
			return nil, fmt.Errorf("%w: attempted network target %q", ErrUnsafeAddress, address)
		}
		addresses, err := ResolvePublicHost(ctx, fetcher.resolver, host)
		if err != nil {
			return nil, err
		}

		dialer := &net.Dialer{Timeout: 15 * time.Second}
		var lastError error
		for _, candidate := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			lastError = err
		}
		return nil, fmt.Errorf("dial WHO host %s: %w", expectedHost, lastError)
	}
}

// ResolvePublicHost resolves a hostname and fails closed when any result is
// private, reserved, loopback, or otherwise unsuitable for an outbound fetch.
func ResolvePublicHost(ctx context.Context, resolver IPResolver, host string) ([]net.IPAddr, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: %s resolved to no addresses", ErrUnsafeAddress, host)
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return nil, fmt.Errorf("%w: %s resolved to %s", ErrUnsafeAddress, host, address.IP)
		}
	}
	return addresses, nil
}

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedIPPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func isCSVContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/csv", "application/csv", "application/octet-stream", "binary/octet-stream", "text/plain":
		return true
	default:
		return false
	}
}

func isHTMLContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}

func discoverDownloadAnchor(page IndicatorPage, body []byte) (DownloadLink, error) {
	base, err := url.Parse(page.URL)
	if err != nil {
		return DownloadLink{}, fmt.Errorf("parse validated WHO page URL: %w", err)
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	candidates := make(map[string]DownloadLink)
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if err := tokenizer.Err(); err != io.EOF {
				return DownloadLink{}, fmt.Errorf("parse WHO indicator page: %w", err)
			}
			if len(candidates) == 0 {
				return DownloadLink{}, fmt.Errorf("%w: WHO page has no matching DataDot CSV anchor", ErrInvalidDownloadURL)
			}
			if len(candidates) != 1 {
				return DownloadLink{}, fmt.Errorf("%w: WHO page has %d matching DataDot CSV anchors", ErrInvalidDownloadURL, len(candidates))
			}
			for _, candidate := range candidates {
				return candidate, nil
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if !strings.EqualFold(token.Data, "a") {
				continue
			}
			for _, attribute := range token.Attr {
				if !strings.EqualFold(attribute.Key, "href") || strings.TrimSpace(attribute.Val) == "" {
					continue
				}
				candidateURL, err := base.Parse(strings.TrimSpace(attribute.Val))
				if err != nil {
					continue
				}
				candidate, err := ValidateDownloadURL(candidateURL.String())
				if err != nil || candidate.IndicatorID != page.IndicatorID {
					continue
				}
				candidates[candidate.URL] = candidate
			}
		}
	}
}

func readCapped(reader io.Reader, maxBytes int64) ([]byte, error) {
	bytes, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read WHO CSV: %w", err)
	}
	if int64(len(bytes)) > maxBytes {
		return nil, fmt.Errorf("%w: received more than %d bytes", ErrArtifactTooLarge, maxBytes)
	}
	return bytes, nil
}
