// Package telegram runs Context Atlas's owner-only Telegram login bot.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	authkit "github.com/supercakecrumb/msgr-authkit"
)

// LoginLinkIssuer is the bot-first portion of msgr-authkit used by /login.
type LoginLinkIssuer interface {
	CreateLoginLink(context.Context, authkit.CreateLoginLinkInput) (authkit.CreateLoginLinkOutput, error)
}

type sendMessage func(context.Context, *tgbot.SendMessageParams) error

// Bot serves exactly one private-chat /login command for the configured owner.
type Bot struct {
	client  *tgbot.Bot
	ownerID int64
	issuer  LoginLinkIssuer
	logger  *slog.Logger
	send    sendMessage
}

// New creates a Telegram bot but does not start polling. The caller should run
// Start in its application lifecycle.
func New(token string, ownerID int64, issuer LoginLinkIssuer, logger *slog.Logger) (*Bot, error) {
	if token == "" {
		return nil, fmt.Errorf("telegram: bot token is required")
	}
	if ownerID <= 0 {
		return nil, fmt.Errorf("telegram: owner ID is required")
	}
	if issuer == nil {
		return nil, fmt.Errorf("telegram: login issuer is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bot{ownerID: ownerID, issuer: issuer, logger: logger}
	client, err := tgbot.New(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: create bot: %w", err)
	}
	b.client = client
	b.send = func(ctx context.Context, params *tgbot.SendMessageParams) error {
		_, err := client.SendMessage(ctx, params)
		return err
	}
	// MatchTypeCommand strips the slash and any bot suffix before matching.
	client.RegisterHandler(tgbot.HandlerTypeMessageText, "login", tgbot.MatchTypeCommand, b.handleLogin)
	return b, nil
}

// Start blocks while polling Telegram until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) {
	b.client.Start(ctx)
}

// Client exposes the underlying library client for lifecycle observability.
func (b *Bot) Client() *tgbot.Bot { return b.client }

func (b *Bot) handleLogin(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.Chat.Type != models.ChatTypePrivate || update.Message.From == nil || update.Message.From.ID != b.ownerID {
		return
	}
	from := update.Message.From
	ownerSubjectID := strconv.FormatInt(b.ownerID, 10)
	link, err := b.issuer.CreateLoginLink(ctx, authkit.CreateLoginLinkInput{
		Messenger: authkit.NewMessenger("telegram"),
		Audience:  "admin",
		SubjectID: ownerSubjectID,
		Identity: &authkit.Identity{
			Messenger:       authkit.NewMessenger("telegram"),
			MessengerUserID: ownerSubjectID,
			Username:        from.Username,
			Name:            from.FirstName,
			Surname:         from.LastName,
		},
	})
	if err != nil {
		b.logger.Error("create telegram login link", "error", err)
		b.reply(ctx, update.Message.Chat.ID, "I couldn't create a login link. Please try /login again.")
		return
	}

	// Telegram otherwise prefetches the URL for a link preview, consuming this
	// one-time credential before its owner opens it.
	disabled := true
	if err := b.send(ctx, &tgbot.SendMessageParams{
		ChatID:             update.Message.Chat.ID,
		Text:               "Login link (valid for 10 minutes): " + link.LoginURL,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &disabled},
	}); err != nil {
		b.logger.Error("send telegram login link", "error", err)
	}
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if err := b.send(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		b.logger.Error("send telegram reply", "error", err)
	}
}
