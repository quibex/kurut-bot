package starttrial

import (
	"context"
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
	l10n                localizer
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	ts tariffService,
	ss subscriptionService,
	us userService,
	l10n localizer,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		tariffService:       ts,
		subscriptionService: ss,
		userService:         us,
		l10n:                l10n,
		logger:              logger,
	}
}

func (h *Handler) Start(ctx context.Context, user *users.User, chatID int64) error {
	// Проверяем, использовал ли пользователь пробный период
	if user.UsedTrial {
		msg := tgbotapi.NewMessage(chatID, h.l10n.Get(user.Language, "trial.already_used", nil))
		_, err := h.bot.Send(msg)
		return err
	}

	// Получаем пробный тариф (бесплатный)
	trialTariff, err := h.tariffService.GetTrialTariff(ctx)
	if err != nil {
		return h.sendError(chatID, user.Language, h.l10n.Get(user.Language, "trial.error_getting_tariffs", nil))
	}

	if trialTariff == nil {
		return h.sendError(chatID, user.Language, h.l10n.Get(user.Language, "trial.unavailable", nil))
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
		return h.sendError(chatID, user.Language, h.l10n.Get(user.Language, "trial.error_creating", nil))
	}

	// Отмечаем что пользователь использовал пробный период
	err = h.userService.MarkTrialAsUsed(ctx, user.ID)
	if err != nil {
		h.logger.Error("Failed to mark trial as used", "error", err)
		// Не возвращаем ошибку, так как подписка уже создана
	}

	// Отправляем инструкции
	return h.sendConnectionInstructions(chatID, subscription, trialTariff.Name, trialTariff.DurationDays, user.Language)
}

func (h *Handler) sendConnectionInstructions(chatID int64, subscription *subs.Subscription, tariffName string, durationDays int, lang string) error {
	messageText := h.l10n.Get(lang, "subscription.success_trial", map[string]interface{}{
		"tariff_name": escapeMarkdownV2(tariffName),
		"duration":    durationDays,
	})

	if subscription.MarzbanLink != "" {
		messageText += "\n`" + subscription.MarzbanLink + "`"
	} else {
		messageText += "\n\n" + h.l10n.Get(lang, "subscription.link_not_ready", nil)
	}

	messageText += "\n\n" + h.l10n.Get(lang, "subscription.instructions", nil) + "\n\n"
	messageText += h.l10n.Get(lang, "subscription.trial_note", nil)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(h.l10n.Get(lang, "buttons.view_tariffs", nil), "view_tariffs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(h.l10n.Get(lang, "buttons.main_menu", nil), "main_menu"),
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

func (h *Handler) sendError(chatID int64, lang, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := h.bot.Send(msg)
	return err
}
