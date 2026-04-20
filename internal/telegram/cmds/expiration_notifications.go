package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/webtokens"
	"kurut-bot/internal/telegram/messages"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// NotificationClientTokenStorage интерфейс для получения client tokens
type NotificationClientTokenStorage interface {
	GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64, partnerWhatsApp *string) (*webtokens.ClientToken, error)
}

// ExpirationNotificationService отвечает за отправку уведомлений о подписках
// Используется и командами (/overdue, /expiring) и воркером expiration
type ExpirationNotificationService struct {
	bot                *tgbotapi.BotAPI
	tariffService      ExpirationTariffService
	serverStorage      ExpirationServerStorage
	clientTokenStorage NotificationClientTokenStorage
	webDomain          string
	logger             *slog.Logger
}

// NewExpirationNotificationService создает новый сервис уведомлений
func NewExpirationNotificationService(
	bot *tgbotapi.BotAPI,
	tariffService ExpirationTariffService,
	serverStorage ExpirationServerStorage,
	clientTokenStorage NotificationClientTokenStorage,
	webDomain string,
	logger *slog.Logger,
) *ExpirationNotificationService {
	return &ExpirationNotificationService{
		bot:                bot,
		tariffService:      tariffService,
		serverStorage:      serverStorage,
		clientTokenStorage: clientTokenStorage,
		webDomain:          webDomain,
		logger:             logger,
	}
}

// SendOverdueSubscriptionMessage отправляет сообщение для одной просроченной подписки
func (s *ExpirationNotificationService) SendOverdueSubscriptionMessage(ctx context.Context, chatID int64, sub *subs.Subscription) error {
	tariff, _ := s.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

	var server *servers.Server
	if sub.ServerID != nil {
		server, _ = s.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
	}

	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	tariffName := "Неизвестный"
	price := 0.0
	if tariff != nil {
		tariffName = tariff.Name
		price = tariff.Price
	}

	// Формируем строку пароля если есть сервер
	passwordLine := ""
	if server != nil && server.UIPassword != "" {
		passwordLine = fmt.Sprintf("\n🔐 Пароль: `%s`", server.UIPassword)
	}

	// Получаем ссылку на личный кабинет клиента
	clientLink := ""
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		createdByTgID := int64(0)
		if sub.CreatedByTelegramID != nil {
			createdByTgID = *sub.CreatedByTelegramID
		}
		clientToken, err := s.clientTokenStorage.GetOrCreateClientToken(ctx, *sub.ClientWhatsApp, createdByTgID, nil)
		if err != nil {
			s.logger.Warn("Failed to get client token", "error", err, "whatsapp", *sub.ClientWhatsApp)
		} else {
			clientLink = fmt.Sprintf("%s/c/%s", s.webDomain, clientToken.Token)
		}
	}

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := GenerateWhatsAppLink(*sub.ClientWhatsApp, "")
		if clientLink != "" {
			text = fmt.Sprintf(
				"⚠️ *Просроченная подписка*\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 Тариф: %s (%.0f ₽)%s\n\n"+
					"🔗 [Ссылка для оплаты](%s)",
				whatsapp, whatsappLink, tariffName, price, passwordLine, clientLink)
		} else {
			text = fmt.Sprintf(
				"⚠️ *Просроченная подписка*\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 Тариф: %s (%.0f ₽)%s",
				whatsapp, whatsappLink, tariffName, price, passwordLine)
		}
	} else {
		text = fmt.Sprintf(
			"⚠️ *Просроченная подписка*\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s (%.0f ₽)%s",
			whatsapp, tariffName, price, passwordLine)
	}

	// Кнопки: Сервер (опционально) и Отключить
	var rows [][]tgbotapi.InlineKeyboardButton

	if server != nil && server.UIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🌐 Сервер", server.UIURL),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ Отключить", fmt.Sprintf("exp_dis:%d", sub.ID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	msg.DisableWebPagePreview = true

	_, err := s.bot.Send(msg)
	return err
}

// SendExpiringSubscriptionMessage отправляет сообщение для одной истекающей подписки
// daysUntilExpiry: 0 = сегодня, 3 = через 3 дня
func (s *ExpirationNotificationService) SendExpiringSubscriptionMessage(ctx context.Context, chatID int64, sub *subs.Subscription, daysUntilExpiry int) error {
	tariff, _ := s.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	tariffName := "Неизвестный"
	price := 0.0
	if tariff != nil {
		tariffName = tariff.Name
		price = tariff.Price
	}

	// Формируем заголовок в зависимости от количества дней
	var headerText string
	var whatsappMsg string
	switch daysUntilExpiry {
	case 0:
		headerText = "🔔 *Подписка истекает сегодня*"
		whatsappMsg = messages.WhatsAppMsgToday
	case 3:
		headerText = "⏰ *Подписка истекает через 3 дня*"
		whatsappMsg = messages.WhatsAppMsg3Days
	default:
		headerText = fmt.Sprintf("⏰ *Подписка истекает через %d дней*", daysUntilExpiry)
		whatsappMsg = messages.WhatsAppMsgToday
	}

	// Получаем ссылку на личный кабинет клиента
	clientLink := ""
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		createdByTgID := int64(0)
		if sub.CreatedByTelegramID != nil {
			createdByTgID = *sub.CreatedByTelegramID
		}
		clientToken, err := s.clientTokenStorage.GetOrCreateClientToken(ctx, *sub.ClientWhatsApp, createdByTgID, nil)
		if err != nil {
			s.logger.Warn("Failed to get client token", "error", err, "whatsapp", *sub.ClientWhatsApp)
		} else {
			clientLink = fmt.Sprintf("%s/c/%s", s.webDomain, clientToken.Token)
		}
	}

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := GenerateWhatsAppLink(*sub.ClientWhatsApp, whatsappMsg)
		if clientLink != "" {
			text = fmt.Sprintf(
				"%s\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 Тариф: %s (%.0f ₽)\n\n"+
					"🔗 [Ссылка для оплаты](%s)",
				headerText, whatsapp, whatsappLink, tariffName, price, clientLink)
		} else {
			text = fmt.Sprintf(
				"%s\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 Тариф: %s (%.0f ₽)",
				headerText, whatsapp, whatsappLink, tariffName, price)
		}
	} else {
		text = fmt.Sprintf(
			"%s\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s (%.0f ₽)",
			headerText, whatsapp, tariffName, price)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true

	_, err := s.bot.Send(msg)
	return err
}

// GenerateWhatsAppLink генерирует ссылку на WhatsApp с предзаполненным сообщением
func GenerateWhatsAppLink(phone string, message string) string {
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")
	if message == "" {
		return fmt.Sprintf("https://wa.me/%s", cleanPhone)
	}
	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}
