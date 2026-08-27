package feedback

import (
	"context"
	"errors"
	"strings"
	"testing"

	snagbox "github.com/supercakecrumb/snagbox/client"
)

type fakeClient struct {
	request snagbox.ReportRequest
	err     error
}

func (f *fakeClient) ReportIssue(_ context.Context, req snagbox.ReportRequest) (*snagbox.Issue, error) {
	f.request = req
	return &snagbox.Issue{ID: "issue-1"}, f.err
}

func TestReporter(t *testing.T) {
	client := &fakeClient{}
	reporter := Reporter{client: client}
	if err := reporter.Report(context.Background(), "  map is blank  ", "/explore", "browser"); err != nil {
		t.Fatal(err)
	}
	if client.request.Text != "map is blank" || client.request.Meta["page"] != "/explore" {
		t.Fatalf("unexpected request: %+v", client.request)
	}
}

func TestReporterValidation(t *testing.T) {
	if err := (Reporter{}).Report(context.Background(), "hello", "/", "browser"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled error = %v", err)
	}
	reporter := Reporter{client: &fakeClient{}}
	if err := reporter.Report(context.Background(), strings.Repeat("x", maxMessageLength+1), "/", "browser"); err == nil {
		t.Fatal("expected length validation error")
	}
}
