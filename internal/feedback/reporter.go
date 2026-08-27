package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"

	snagbox "github.com/supercakecrumb/snagbox/client"
)

const maxMessageLength = 4000

var ErrDisabled = errors.New("feedback reporting is not configured")

type issueReporter interface {
	ReportIssue(context.Context, snagbox.ReportRequest) (*snagbox.Issue, error)
}

type Reporter struct {
	client issueReporter
}

func New(baseURL, token string) Reporter {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" {
		return Reporter{}
	}
	return Reporter{client: snagbox.New(baseURL, token)}
}

func (r Reporter) Report(ctx context.Context, message, page, userAgent string) error {
	message = strings.TrimSpace(message)
	if r.client == nil {
		return ErrDisabled
	}
	if message == "" {
		return errors.New("feedback message is required")
	}
	if len(message) > maxMessageLength {
		return fmt.Errorf("feedback message must be at most %d bytes", maxMessageLength)
	}
	_, err := r.client.ReportIssue(ctx, snagbox.ReportRequest{
		Text: message,
		Meta: map[string]any{
			"page":       strings.TrimSpace(page),
			"user_agent": strings.TrimSpace(userAgent),
			"source":     "context-atlas-web",
		},
	})
	if err != nil {
		return fmt.Errorf("report feedback: %w", err)
	}
	return nil
}
