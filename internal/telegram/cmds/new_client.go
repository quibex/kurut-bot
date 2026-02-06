package cmds

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/webtokens"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type NewClientCommand struct {
	bot                 *tgbotapi.BotAPI
	clientTokenStorage  clientTokenStorage
	webDomain           string
	logger              *slog.Logger
}

type clientTokenStorage interface {
	GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64) (*webtokens.ClientToken, error)
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

// HandleWhatsAppInput обрабатывает ввод WhatsApp и генерирует ссылку
func (c *NewClientCommand) HandleWhatsAppInput(ctx context.Context, chatID int64, telegramID int64, whatsapp string) error {
	// Get or create client token (one permanent token per client)
	clientToken, err := c.clientTokenStorage.GetOrCreateClientToken(ctx, whatsapp, telegramID)
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

	msg := tgbotapi.NewMessage(chatID, text)

	_, err = c.bot.Send(msg)
	return err
}

func (c *NewClientCommand) sendError(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, "❌ "+text)
	_, err := c.bot.Send(msg)
	return err
}
