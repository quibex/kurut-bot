package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"kurut-bot/internal/stories/webtokens"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type NewClientCommand struct {
	bot                *tgbotapi.BotAPI
	clientTokenStorage clientTokenStorage
	webDomain          string
	logger             *slog.Logger
}

type clientTokenStorage interface {
	GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64, partnerWhatsApp *string) (*webtokens.ClientToken, error)
}

func NewNewClientCommand(
	bot *tgbotapi.BotAPI,
	clientTokenStorage clientTokenStorage,
	webDomain string,
	logger *slog.Logger,
) *NewClientCommand {
	return &NewClientCommand{
		bot:                bot,
		clientTokenStorage: clientTokenStorage,
		webDomain:          webDomain,
		logger:             logger,
	}
}

// Execute запрашивает WhatsApp клиента
func (c *NewClientCommand) Execute(ctx context.Context, chatID int64) error {
	text := "📱 *Новый клиент*\n\nВведите WhatsApp клиента (например, +996700123456):"

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"

	// Добавляем кнопку отмены
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel"),
		),
	)
	msg.ReplyMarkup = keyboard

	_, err := c.bot.Send(msg)
	return err
}

// ShowPartnerQuestion показывает вопрос о партнере
func (c *NewClientCommand) ShowPartnerQuestion(ctx context.Context, chatID int64, messageID *int) error {
	text := "🤝 Есть партнёр, который привёл этого клиента?\n\n" +
		"(партнёрская статистика, без бонусов)"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, есть", "nc_partner_yes"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет", "nc_partner_no"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("◀️ Отменить", "cancel"),
		),
	)

	if messageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *messageID, text)
		editMsg.ReplyMarkup = &keyboard
		_, err := c.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	_, err := c.bot.Send(msg)
	return err
}

// HandlePartnerInput обрабатывает ввод номера партнера
func (c *NewClientCommand) HandlePartnerInput(ctx context.Context, chatID int64, telegramID int64, whatsapp string, partnerWhatsApp string) error {
	var partner *string

	// If partnerWhatsApp is not empty, validate it
	if partnerWhatsApp != "" {
		// Нормализуем и валидируем номер партнера
		partnerWhatsApp = normalizePhone(partnerWhatsApp)

		if !isValidPhoneNumber(partnerWhatsApp) {
			return c.sendError(chatID, "❌ Неверный формат номера партнера. Введите номер телефона")
		}

		partner = &partnerWhatsApp
	}

	// Get or create client token with or without partner
	return c.finalizeClientLink(ctx, chatID, telegramID, whatsapp, partner)
}

// FinalizeClientLink генерирует финальную ссылку для клиента
func (c *NewClientCommand) finalizeClientLink(ctx context.Context, chatID int64, telegramID int64, whatsapp string, partnerWhatsApp *string) error {
	// Get or create client token (one permanent token per client)
	clientToken, err := c.clientTokenStorage.GetOrCreateClientToken(ctx, whatsapp, telegramID, partnerWhatsApp)
	if err != nil {
		c.logger.Error("Failed to get or create client token", "error", err)
		return c.sendError(chatID, "Ошибка создания ссылки")
	}

	// Build client link
	clientLink := fmt.Sprintf("%s/c/%s", c.webDomain, clientToken.Token)

	text := fmt.Sprintf(
		"✅ Ссылка для клиента создана\n\n"+
			"📱 WhatsApp: %s\n\n"+
			"🔗 Ссылка:\n%s\n\n"+
			"Эта ссылка постоянная для данного клиента.\n"+
			"Клиент сможет купить новую подписку или продлить существующую.",
		whatsapp, clientLink)

	if partnerWhatsApp != nil {
		text += fmt.Sprintf("\n\n🤝 Партнёр: %s", *partnerWhatsApp)
	}

	msg := tgbotapi.NewMessage(chatID, text)

	_, err = c.bot.Send(msg)
	return err
}

func (c *NewClientCommand) sendError(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, "❌ "+text)
	_, err := c.bot.Send(msg)
	return err
}

// normalizePhone очищает номер телефона, оставляя только цифры
func normalizePhone(phone string) string {
	// Remove all non-digit characters
	re := regexp.MustCompile(`\D`)
	cleaned := re.ReplaceAllString(phone, "")
	return cleaned
}

// isValidPhoneNumber проверяет что нормализованный номер телефона валиден
func isValidPhoneNumber(normalizedPhone string) bool {
	// International phone numbers: 7-15 digits
	return len(normalizedPhone) >= 7 && len(normalizedPhone) <= 15
}
