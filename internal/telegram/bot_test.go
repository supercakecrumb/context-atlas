package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	authkit "github.com/supercakecrumb/msgr-authkit"
)

type fakeIssuer struct {
	input authkit.CreateLoginLinkInput
	calls int
	err   error
}

func (f *fakeIssuer) CreateLoginLink(_ context.Context, input authkit.CreateLoginLinkInput) (authkit.CreateLoginLinkOutput, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return authkit.CreateLoginLinkOutput{}, f.err
	}
	return authkit.CreateLoginLinkOutput{LoginURL: "https://atlas.aurorass.art/login?auth_token=signed"}, nil
}

func testBot(ownerID int64, issuer LoginLinkIssuer, sent **tgbot.SendMessageParams) *Bot {
	return &Bot{
		ownerID: ownerID,
		issuer:  issuer,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		send: func(_ context.Context, params *tgbot.SendMessageParams) error {
			*sent = params
			return nil
		},
	}
}

func privateLoginUpdate(ownerID int64) *models.Update {
	return &models.Update{Message: &models.Message{
		Text: "/login",
		Chat: models.Chat{ID: 100, Type: models.ChatTypePrivate},
		From: &models.User{ID: ownerID, Username: "aurora", FirstName: "Aurora", LastName: "Kiel"},
	}}
}

func TestLoginIssuesLinkOnlyForPrivateOwner(t *testing.T) {
	issuer := &fakeIssuer{}
	var sent *tgbot.SendMessageParams
	bot := testBot(42, issuer, &sent)
	bot.handleLogin(context.Background(), nil, privateLoginUpdate(42))

	if issuer.calls != 1 || issuer.input.SubjectID != "42" || issuer.input.Messenger.ID != "telegram" || issuer.input.Audience != "admin" {
		t.Fatalf("unexpected login input: %+v", issuer.input)
	}
	if issuer.input.Identity == nil || issuer.input.Identity.MessengerUserID != "42" || issuer.input.Identity.Username != "aurora" {
		t.Fatalf("unexpected identity: %+v", issuer.input.Identity)
	}
	if sent == nil || sent.ChatID != int64(100) || sent.LinkPreviewOptions == nil || sent.LinkPreviewOptions.IsDisabled == nil || !*sent.LinkPreviewOptions.IsDisabled {
		t.Fatalf("unsafe Telegram reply: %+v", sent)
	}
}

func TestLoginIgnoresNonOwnerAndNonPrivateRequests(t *testing.T) {
	issuer := &fakeIssuer{}
	var sent *tgbot.SendMessageParams
	bot := testBot(42, issuer, &sent)

	bot.handleLogin(context.Background(), nil, privateLoginUpdate(7))
	group := privateLoginUpdate(42)
	group.Message.Chat.Type = models.ChatTypeGroup
	bot.handleLogin(context.Background(), nil, group)

	if issuer.calls != 0 || sent != nil {
		t.Fatalf("non-owner/private request issued a link: calls=%d sent=%+v", issuer.calls, sent)
	}
}

func TestLoginReportsIssuerFailureToOwner(t *testing.T) {
	issuer := &fakeIssuer{err: errors.New("offline")}
	var sent *tgbot.SendMessageParams
	bot := testBot(42, issuer, &sent)
	bot.handleLogin(context.Background(), nil, privateLoginUpdate(42))
	if sent == nil || sent.Text == "" || sent.LinkPreviewOptions != nil {
		t.Fatalf("failure reply = %+v", sent)
	}
}
