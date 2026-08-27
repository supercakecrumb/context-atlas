package who

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	ErrInvalidIndicatorURL = errors.New("invalid WHO indicator URL")
	ErrInvalidDownloadURL  = errors.New("invalid WHO DataDot download URL")
	ErrCuratedSchema       = errors.New("curated dataset schema changed")
	ErrUnsupportedSchema   = errors.New("unsupported WHO DataDot CSV schema")
	ErrMalformedCSV        = errors.New("malformed CSV")
	ErrPreviewInvalid      = errors.New("invalid import preview")
	ErrArtifactTooLarge    = errors.New("WHO artifact exceeds size limit")
	ErrUnsafeAddress       = errors.New("unsafe network address")
)

var indicatorPartPattern = regexp.MustCompile(`^[A-Za-z0-9]{7}$`)
var downloadFilePattern = regexp.MustCompile(`^([A-Za-z0-9]{7})_ALL_LATEST\.csv$`)

// IndicatorPage is the normalized form of an allowed WHO indicator page URL.
type IndicatorPage struct {
	URL          string
	CollectionID string
	IndicatorID  string
}

// DownloadLink is the normalized form of an allowed public DataDot CSV URL.
type DownloadLink struct {
	URL         string
	IndicatorID string
}

// ValidateIndicatorPageURL accepts only canonical HTTPS data.who.int indicator
// pages. Query parameters are deliberately rejected: a submission is a stable
// source page, not a filtered browser view.
func ValidateIndicatorPageURL(raw string) (IndicatorPage, error) {
	u, err := parseHTTPSURL(raw, WHODataHost, ErrInvalidIndicatorURL)
	if err != nil {
		return IndicatorPage{}, err
	}
	if u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return IndicatorPage{}, fmt.Errorf("%w: canonical pages cannot include query, fragment, or escaped path", ErrInvalidIndicatorURL)
	}

	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if (len(parts) != 3 && len(parts) != 4) || parts[0] != "indicators" || parts[1] != "i" || !indicatorPartPattern.MatchString(parts[len(parts)-1]) {
		return IndicatorPage{}, fmt.Errorf("%w: expected /indicators/i/<indicator> or /indicators/i/<collection>/<indicator>", ErrInvalidIndicatorURL)
	}
	collectionID := ""
	if len(parts) == 4 {
		if !indicatorPartPattern.MatchString(parts[2]) {
			return IndicatorPage{}, fmt.Errorf("%w: invalid collection identifier", ErrInvalidIndicatorURL)
		}
		collectionID = strings.ToUpper(parts[2])
	}
	indicatorID := strings.ToUpper(parts[len(parts)-1])
	canonicalPath := "/indicators/i/"
	if collectionID != "" {
		canonicalPath += collectionID + "/"
	}
	canonicalPath += indicatorID
	return IndicatorPage{
		URL:          "https://" + WHODataHost + canonicalPath,
		CollectionID: collectionID,
		IndicatorID:  indicatorID,
	}, nil
}

// ValidateDownloadURL accepts only the fixed public WHO DataDot CSV route.
// Query strings are rejected so signed or redirected URLs cannot be persisted as
// source metadata by accident.
func ValidateDownloadURL(raw string) (DownloadLink, error) {
	u, err := parseHTTPSURL(raw, WHOBlobHost, ErrInvalidDownloadURL)
	if err != nil {
		return DownloadLink{}, err
	}
	if u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return DownloadLink{}, fmt.Errorf("%w: downloads cannot include query, fragment, or escaped path", ErrInvalidDownloadURL)
	}
	if !strings.HasPrefix(u.Path, WHOBlobPathPrefix) {
		return DownloadLink{}, fmt.Errorf("%w: unexpected blob path", ErrInvalidDownloadURL)
	}
	name := strings.TrimPrefix(u.Path, WHOBlobPathPrefix)
	match := downloadFilePattern.FindStringSubmatch(name)
	if len(match) != 2 {
		return DownloadLink{}, fmt.Errorf("%w: expected <indicator>_ALL_LATEST.csv", ErrInvalidDownloadURL)
	}
	indicatorID := strings.ToUpper(match[1])
	return DownloadLink{
		URL:         "https://" + WHOBlobHost + WHOBlobPathPrefix + indicatorID + "_ALL_LATEST.csv",
		IndicatorID: indicatorID,
	}, nil
}

func parseHTTPSURL(raw, host string, base error) (*url.URL, error) {
	if strings.TrimSpace(raw) != raw || raw == "" {
		return nil, fmt.Errorf("%w: URL is empty or padded", base)
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", base, err)
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Opaque != "" || u.User != nil || u.Port() != "" || !strings.EqualFold(u.Hostname(), host) || !strings.EqualFold(u.Host, host) {
		return nil, fmt.Errorf("%w: URL must be HTTPS on %s without credentials or a port", base, host)
	}
	return u, nil
}
