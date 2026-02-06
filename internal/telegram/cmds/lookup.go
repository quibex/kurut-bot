package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/webtokens"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type LookupCommand struct {
	bot                *tgbotapi.BotAPI
	storage            LookupStorage
	clientTokenStorage LookupClientTokenStorage
	webDomain          string
	logger             *slog.Logger
}

type LookupStorage interface {
	SearchSubscriptionsByPhoneSuffix(ctx context.Context, suffix string) ([]storage.SubscriptionLookupResult, error)
}

type LookupClientTokenStorage interface {
	GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64) (*webtokens.ClientToken, error)
}

func NewLookupCommand(
	bot *tgbotapi.BotAPI,
	storage LookupStorage,
	clientTokenStorage LookupClientTokenStorage,
	webDomain string,
	logger *slog.Logger,
) *LookupCommand {
	return &LookupCommand{
		bot:                bot,
		storage:            storage,
		clientTokenStorage: clientTokenStorage,
		webDomain:          webDomain,
		logger:             logger,
	}
}

func (c *LookupCommand) Execute(ctx context.Context, chatID int64, phoneSuffix string) error {
	// Validate: extract only digits
	digits := extractDigits(phoneSuffix)
	if len(digits) < 3 || len(digits) > 6 {
		msg := tgbotapi.NewMessage(chatID, "Введите от 3 до 6 последних цифр номера.\nПример: /find 2706")
		_, _ = c.bot.Send(msg)
		return nil
	}

	results, err := c.storage.SearchSubscriptionsByPhoneSuffix(ctx, digits)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка поиска")
		_, _ = c.bot.Send(msg)
		return fmt.Errorf("search subscriptions by phone suffix: %w", err)
	}

	if len(results) == 0 {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Подписки с номером *...%s* не найдены", digits))
		msg.ParseMode = "Markdown"
		_, _ = c.bot.Send(msg)
		return nil
	}

	// Build client links for unique WhatsApp numbers
	clientLinks := make(map[string]string)
	for _, r := range results {
		if r.ClientWhatsApp == nil || *r.ClientWhatsApp == "" {
			continue
		}
		wa := *r.ClientWhatsApp
		if _, ok := clientLinks[wa]; ok {
			continue
		}
		token, err := c.clientTokenStorage.GetOrCreateClientToken(ctx, wa, chatID)
		if err != nil {
			c.logger.Error("Failed to get client token", "error", err, "whatsapp", wa)
			continue
		}
		clientLinks[wa] = fmt.Sprintf("%s/c/%s", c.webDomain, token.Token)
	}

	text := c.formatResults(results, digits, clientLinks)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	_, err = c.bot.Send(msg)
	return err
}

func (c *LookupCommand) formatResults(results []storage.SubscriptionLookupResult, suffix string, clientLinks map[string]string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("🔍 *Подписки по номеру ...%s*\n", suffix))
	b.WriteString(fmt.Sprintf("Найдено: %d\n", len(results)))

	for i, r := range results {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("━━━ *#%d* ━━━\n", r.ID))

		if r.ClientWhatsApp != nil {
			b.WriteString(fmt.Sprintf("📱 %s\n", *r.ClientWhatsApp))
		}

		b.WriteString(fmt.Sprintf("📋 Тариф: %s\n", r.TariffName))
		b.WriteString(fmt.Sprintf("📌 Статус: %s\n", formatStatus(r.Status)))

		if r.ServerName != nil {
			b.WriteString(fmt.Sprintf("🖥 Сервер: %s\n", *r.ServerName))
		}

		if r.ExpiresAt != nil {
			b.WriteString(fmt.Sprintf("⏳ Истекает: %s\n", formatDate(*r.ExpiresAt)))
		}

		if r.LastRenewedAt != nil {
			b.WriteString(fmt.Sprintf("🔄 Продлён: %s\n", formatDate(*r.LastRenewedAt)))
		}

		b.WriteString(fmt.Sprintf("📅 Создан: %s\n", formatDate(r.CreatedAt)))

		if r.ClientWhatsApp != nil {
			if link, ok := clientLinks[*r.ClientWhatsApp]; ok {
				b.WriteString(fmt.Sprintf("🔗 [Личный кабинет](%s)\n", link))
			}
		}

		// Limit to 10 results to avoid message being too long
		if i >= 9 {
			remaining := len(results) - 10
			if remaining > 0 {
				b.WriteString(fmt.Sprintf("\n...и ещё %d\n", remaining))
			}
			break
		}
	}

	return b.String()
}

func formatStatus(status string) string {
	switch status {
	case "active":
		return "✅ Активна"
	case "expired":
		return "⚠️ Просрочена"
	case "disabled":
		return "🚫 Отключена"
	case "pending":
		return "⏳ Ожидание"
	default:
		return status
	}
}

func formatDate(t time.Time) string {
	return t.Format("02.01.2006")
}

func extractDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
