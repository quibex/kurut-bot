package starttrial

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/users"
)

type Handler struct {
	bot                 botApi
	tariffService       tariffService
	subscriptionService subscriptionService
	userService         userService
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	ts tariffService,
	ss subscriptionService,
	us userService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		tariffService:       ts,
		subscriptionService: ss,
		userService:         us,
		logger:              logger,
	}
}

func (h *Handler) Start(ctx context.Context, user *users.User, chatID int64) error {
	// Проверяем, использовал ли пользователь пробный период
	if user.UsedTrial {
		msg := tgbotapi.NewMessage(chatID, "❌ Вы уже использовали пробный период.\n\n"+
			"Используйте /buy чтобы выбрать платный тариф.")
		_, err := h.bot.Send(msg)
		return err
	}

	// Получаем пробный тариф (бесплатный)
	trialTariff, err := h.tariffService.GetTrialTariff(ctx)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка получения тарифов")
	}

	if trialTariff == nil {
		return h.sendError(chatID, "❌ Пробный период временно недоступен")
	}

	// Создаем подписку
	subReq := &subs.CreateSubscriptionRequest{
		UserID:    user.ID,
		TariffID:  trialTariff.ID,
		PaymentID: nil, // Без платежа для пробного периода
	}

	subscription, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create trial subscription", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания пробной подписки")
	}

	// Отмечаем что пользователь использовал пробный период
	err = h.userService.MarkTrialAsUsed(ctx, user.ID)
	if err != nil {
		h.logger.Error("Failed to mark trial as used", "error", err)
		// Не возвращаем ошибку, так как подписка уже создана
	}

	// Отправляем инструкции
	return h.sendConnectionInstructions(chatID, subscription, trialTariff.Name, trialTariff.DurationDays)
}

func (h *Handler) sendConnectionInstructions(chatID int64, subscription *subs.Subscription, tariffName string, durationDays int) error {
	messageText := fmt.Sprintf(
		"🎉 *Пробный период активирован\\!*\n\n"+
			"📅 Тариф: %s \\(%d дней\\)\n\n"+
			"🔗 *Ссылка подключения:*\n",
		escapeMarkdownV2(tariffName), durationDays)

	if subscription.MarzbanLink != "" {
		messageText += fmt.Sprintf("`%s`\n\n", subscription.MarzbanLink)
	} else {
		messageText += "❌ Ссылка подключения не готова\n\n"
	}

	messageText += "📋 *Инструкция по подключению:*\n\n"
	messageText += "📱 *1\\. Скачайте приложение v2RayTun:*\n"
	messageText += "• Android: [Google Play](https://play.google.com/store/apps/details?id=com.v2raytun.android)\n"
	messageText += "• iOS: [App Store](https://apps.apple.com/us/app/v2raytun/id6476628951)\n\n"
	messageText += "📋 *2\\. Настройте подключение:*\n"
	messageText += "• Скопируйте ссылку подключения выше\n"
	messageText += "• Откройте v2RayTun\n"
	messageText += "• Добавьте конфигурацию через \\\"Импорт из буфера\\\"\n\n"
	messageText += "⚠️ *Если v2RayTun не работает, используйте Happ:*\n"
	messageText += "• Android: [Google Play](https://play.google.com/store/apps/details?id=com.happproxy)\n"
	messageText += "• iOS: [App Store](https://apps.apple.com/us/app/happ\\-proxy\\-utility/id6504287215)\n\n"
	messageText += "💡 После окончания пробного периода используйте /buy для покупки платного тарифа"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 Посмотреть тарифы", "view_tariffs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Главное меню", "main_menu"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "MarkdownV2"
	msg.ReplyMarkup = keyboard
	msg.DisableWebPagePreview = true

	_, err := h.bot.Send(msg)
	return err
}

func escapeMarkdownV2(text string) string {
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	result := text
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

func (h *Handler) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := h.bot.Send(msg)
	return err
}
