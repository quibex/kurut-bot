package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/messages"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ExpirationNotificationService отвечает за отправку уведомлений о подписках
// Используется и командами (/overdue, /expiring) и воркером expiration
type ExpirationNotificationService struct {
	bot            *tgbotapi.BotAPI
	tariffService  ExpirationTariffService
	serverStorage  ExpirationServerStorage
	messageStorage ExpirationMessageStorage
	paymentService ExpirationPaymentService
	logger         *slog.Logger
}

// NewExpirationNotificationService создает новый сервис уведомлений
func NewExpirationNotificationService(
	bot *tgbotapi.BotAPI,
	tariffService ExpirationTariffService,
	serverStorage ExpirationServerStorage,
	messageStorage ExpirationMessageStorage,
	paymentService ExpirationPaymentService,
	logger *slog.Logger,
) *ExpirationNotificationService {
	return &ExpirationNotificationService{
		bot:            bot,
		tariffService:  tariffService,
		serverStorage:  serverStorage,
		messageStorage: messageStorage,
		paymentService: paymentService,
		logger:         logger,
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
	if tariff != nil {
		tariffName = tariff.Name
	}

	// Формируем строку пароля если есть сервер
	passwordLine := ""
	if server != nil && server.UIPassword != "" {
		passwordLine = fmt.Sprintf("\n🔐 Пароль: `%s`", server.UIPassword)
	}

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := GenerateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекла. Для продолжения работы необходимо оплатить подписку.")
		text = fmt.Sprintf(
			"⚠️ *Просроченная подписка*\n\n"+
				"📱 Клиент: [%s](%s)\n"+
				"📅 Тариф: %s%s",
			whatsapp, whatsappLink, tariffName, passwordLine)
	} else {
		text = fmt.Sprintf(
			"⚠️ *Просроченная подписка*\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s%s",
			whatsapp, tariffName, passwordLine)
	}

	// Кнопки до отключения: Сервер, Отключить
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

	sentMsg, err := s.bot.Send(msg)
	if err != nil {
		return err
	}

	// Сохраняем сообщение в БД для отслеживания конфликтов
	_, err = s.messageStorage.CreateSubscriptionMessage(ctx, submessages.SubscriptionMessage{
		SubscriptionID: sub.ID,
		ChatID:         chatID,
		MessageID:      sentMsg.MessageID,
		Type:           submessages.TypeOverdue,
		IsActive:       true,
	})
	if err != nil {
		s.logger.Error("Failed to save subscription message", "error", err, "sub_id", sub.ID)
	}

	return nil
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

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := GenerateWhatsAppLink(*sub.ClientWhatsApp, whatsappMsg)
		text = fmt.Sprintf(
			"%s\n\n"+
				"📱 Клиент: [%s](%s)\n"+
				"📅 Тариф: %s (%.0f ₽)",
			headerText, whatsapp, whatsappLink, tariffName, price)
	} else {
		text = fmt.Sprintf(
			"%s\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s (%.0f ₽)",
			headerText, whatsapp, tariffName, price)
	}

	// Формируем кнопки
	var rows [][]tgbotapi.InlineKeyboardButton

	// Кнопка "Сменить тариф"
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Сменить тариф", fmt.Sprintf("exp_tariff:%d", sub.ID)),
	))

	// Кнопки "Ссылка" и "Оплачено"
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔗 Ссылка", fmt.Sprintf("exp_link:%d", sub.ID)),
		tgbotapi.NewInlineKeyboardButtonData(s.paidButtonText(), fmt.Sprintf("exp_paid:%d", sub.ID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	msg.DisableWebPagePreview = true

	sentMsg, err := s.bot.Send(msg)
	if err != nil {
		return err
	}

	// Сохраняем сообщение в БД для отслеживания конфликтов
	_, err = s.messageStorage.CreateSubscriptionMessage(ctx, submessages.SubscriptionMessage{
		SubscriptionID: sub.ID,
		ChatID:         chatID,
		MessageID:      sentMsg.MessageID,
		Type:           submessages.TypeExpiring,
		IsActive:       true,
	})
	if err != nil {
		s.logger.Error("Failed to save subscription message", "error", err, "sub_id", sub.ID)
	}

	return nil
}

// paidButtonText возвращает текст кнопки в зависимости от режима оплаты
func (s *ExpirationNotificationService) paidButtonText() string {
	if s.paymentService.IsManualPayment() {
		return "✅ Оплачено"
	}
	return "✅ Проверить"
}

// GenerateWhatsAppLink генерирует ссылку на WhatsApp с предзаполненным сообщением
func GenerateWhatsAppLink(phone string, message string) string {
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")
	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}
