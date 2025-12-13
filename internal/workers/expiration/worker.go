package expiration

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

// Worker handles sending notifications about expiring subscriptions
type Worker struct {
	storage       Storage
	serverStorage ServerStorage
	telegramBot   TelegramBot
	tariffService TariffService
	logger        *slog.Logger
	cron          *cron.Cron
}

// NewWorker creates a new expiration worker
func NewWorker(storage Storage, serverStorage ServerStorage, telegramBot TelegramBot, tariffService TariffService, logger *slog.Logger) *Worker {
	return &Worker{
		storage:       storage,
		serverStorage: serverStorage,
		telegramBot:   telegramBot,
		tariffService: tariffService,
		logger:        logger,
		cron:          cron.New(),
	}
}

// Name returns the worker name
func (w *Worker) Name() string {
	return "expiration"
}

// Start starts the expiration worker
func (w *Worker) Start() error {
	// Runs daily at 09:00
	_, err := w.cron.AddFunc("0 9 * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in expiration worker", "panic", r)
			}
		}()
		ctx := context.Background()
		w.logger.Info("Running expiration worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Expiration worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule expiration worker: %w", err)
	}

	w.cron.Start()
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping expiration worker")
	w.cron.Stop()
}

// RunNow runs the worker immediately (for manual testing)
func (w *Worker) RunNow(ctx context.Context) error {
	w.logger.Info("Manual run of expiration worker")
	return w.run(ctx)
}

// run executes the expiration logic
func (w *Worker) run(ctx context.Context) error {
	w.logger.Info("Starting expiration worker execution")

	// 1. Send notifications about subscriptions expiring TOMORROW (NEW)
	if err := w.sendExpiringTomorrowNotifications(ctx); err != nil {
		w.logger.Error("Failed to send expiring tomorrow notifications", "error", err)
	}

	// 2. Send notifications about subscriptions expiring today
	if err := w.sendExpiringTodayNotifications(ctx); err != nil {
		w.logger.Error("Failed to send expiring today notifications", "error", err)
	}

	// 3. Send notifications about overdue subscriptions (need to disable)
	if err := w.sendOverdueNotifications(ctx); err != nil {
		w.logger.Error("Failed to send overdue notifications", "error", err)
	}

	// 4. Mark expired subscriptions as expired in DB
	if err := w.markExpiredSubscriptions(ctx); err != nil {
		w.logger.Error("Failed to mark expired subscriptions", "error", err)
	}

	w.logger.Info("Expiration worker execution completed")
	return nil
}

// sendExpiringTomorrowNotifications sends notifications about subscriptions expiring tomorrow
func (w *Worker) sendExpiringTomorrowNotifications(ctx context.Context) error {
	expiringByAssistant, err := w.storage.ListExpiringTomorrowGroupedByAssistant(ctx)
	if err != nil {
		return fmt.Errorf("list expiring tomorrow: %w", err)
	}

	w.logger.Info("Found subscriptions expiring tomorrow", "assistants_count", len(expiringByAssistant))

	for assistantID, subscriptions := range expiringByAssistant {
		if err := w.sendExpiringTomorrowNotification(ctx, assistantID, subscriptions); err != nil {
			w.logger.Error("Failed to send expiring tomorrow notification",
				"assistant_id", assistantID,
				"error", err)
		}
	}

	return nil
}

// sendExpiringTodayNotifications sends notifications about subscriptions expiring today
func (w *Worker) sendExpiringTodayNotifications(ctx context.Context) error {
	expiringByAssistant, err := w.storage.ListExpiringTodayGroupedByAssistant(ctx)
	if err != nil {
		return fmt.Errorf("list expiring today: %w", err)
	}

	w.logger.Info("Found expiring subscriptions", "assistants_count", len(expiringByAssistant))

	for assistantID, subscriptions := range expiringByAssistant {
		if err := w.sendExpiringNotification(ctx, assistantID, subscriptions); err != nil {
			w.logger.Error("Failed to send expiring notification",
				"assistant_id", assistantID,
				"error", err)
		}
	}

	return nil
}

// sendOverdueNotifications sends notifications about overdue subscriptions
func (w *Worker) sendOverdueNotifications(ctx context.Context) error {
	overdueByAssistant, err := w.storage.ListOverdueSubscriptionsGroupedByAssistant(ctx)
	if err != nil {
		return fmt.Errorf("list overdue: %w", err)
	}

	w.logger.Info("Found overdue subscriptions", "assistants_count", len(overdueByAssistant))

	for assistantID, subscriptions := range overdueByAssistant {
		if err := w.sendOverdueNotification(ctx, assistantID, subscriptions); err != nil {
			w.logger.Error("Failed to send overdue notification",
				"assistant_id", assistantID,
				"error", err)
		}
	}

	return nil
}

// sendExpiringNotification sends a notification about expiring subscriptions to an assistant
func (w *Worker) sendExpiringNotification(ctx context.Context, assistantTelegramID int64, subscriptions []*subs.Subscription) error {
	if len(subscriptions) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🔔 *Подписки истекают сегодня:*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		tariff, _ := w.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

		whatsapp := "Не указан"
		if sub.ClientWhatsApp != nil {
			whatsapp = *sub.ClientWhatsApp
		}

		tariffName := "Неизвестный"
		if tariff != nil {
			tariffName = tariff.Name
		}

		expiresAt := "Не указано"
		if sub.ExpiresAt != nil {
			expiresAt = sub.ExpiresAt.Format("02.01.2006")
		}

		sb.WriteString(fmt.Sprintf("%d. Клиент: `%s`\n", i+1, whatsapp))
		sb.WriteString(fmt.Sprintf("   Тариф: %s\n", tariffName))
		sb.WriteString(fmt.Sprintf("   Истекает: %s\n\n", expiresAt))

		// Кнопки (3 в ряд)
		row := []tgbotapi.InlineKeyboardButton{}

		// 1. WhatsApp
		if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
			whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекает сегодня. Хотите продлить?")
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("💬", whatsappLink))
		}

		// 2. Ссылка на оплату
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("💳 Оплата", fmt.Sprintf("exp_pay:%d", sub.ID)))

		// 3. Оплатил
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✅ Оплатил", fmt.Sprintf("exp_chk:%d", sub.ID)))

		if len(row) > 0 {
			allRows = append(allRows, row)
		}
	}

	sb.WriteString("Напишите клиентам о продлении подписки.")

	msg := tgbotapi.NewMessage(assistantTelegramID, sb.String())
	msg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		msg.ReplyMarkup = keyboard
	}

	_, err := w.telegramBot.Send(msg)
	return err
}

// sendExpiringTomorrowNotification sends a notification about subscriptions expiring tomorrow
func (w *Worker) sendExpiringTomorrowNotification(ctx context.Context, assistantTelegramID int64, subscriptions []*subs.Subscription) error {
	if len(subscriptions) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🔔 *Подписки истекают завтра:*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		tariff, _ := w.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

		whatsapp := "Не указан"
		if sub.ClientWhatsApp != nil {
			whatsapp = *sub.ClientWhatsApp
		}

		tariffName := "Неизвестный"
		if tariff != nil {
			tariffName = tariff.Name
		}

		expiresAt := "Не указано"
		if sub.ExpiresAt != nil {
			expiresAt = sub.ExpiresAt.Format("02.01.2006")
		}

		sb.WriteString(fmt.Sprintf("%d. Клиент: `%s`\n", i+1, whatsapp))
		sb.WriteString(fmt.Sprintf("   Тариф: %s\n", tariffName))
		sb.WriteString(fmt.Sprintf("   Истекает: %s\n\n", expiresAt))

		// Кнопки (3 в ряд)
		row := []tgbotapi.InlineKeyboardButton{}

		// 1. WhatsApp
		if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
			whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекает завтра. Хотите продлить?")
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("💬", whatsappLink))
		}

		// 2. Ссылка на оплату
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("💳 Оплата", fmt.Sprintf("exp_pay:%d", sub.ID)))

		// 3. Оплатил
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✅ Оплатил", fmt.Sprintf("exp_chk:%d", sub.ID)))

		if len(row) > 0 {
			allRows = append(allRows, row)
		}
	}

	sb.WriteString("Напишите клиентам о продлении подписки.")

	msg := tgbotapi.NewMessage(assistantTelegramID, sb.String())
	msg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		msg.ReplyMarkup = keyboard
	}

	_, err := w.telegramBot.Send(msg)
	return err
}

// sendOverdueNotification sends a notification about overdue subscriptions to an assistant
func (w *Worker) sendOverdueNotification(ctx context.Context, assistantTelegramID int64, subscriptions []*subs.Subscription) error {
	if len(subscriptions) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("⚠️ *Подписки требуют отключения (не оплачено):*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		// Получаем информацию о сервере
		var server *servers.Server
		if sub.ServerID != nil {
			server, _ = w.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
		}

		whatsapp := "Не указан"
		if sub.ClientWhatsApp != nil {
			whatsapp = *sub.ClientWhatsApp
		}

		userID := "Не указан"
		if sub.GeneratedUserID != nil {
			userID = *sub.GeneratedUserID
		}

		password := "N/A"
		serverName := "N/A"
		var serverURL string
		if server != nil {
			password = server.UIPassword
			serverName = server.Name
			serverURL = server.UIURL
		}

		daysOverdue := 0
		if sub.ExpiresAt != nil {
			daysOverdue = int(time.Since(*sub.ExpiresAt).Hours() / 24)
		}

		sb.WriteString(fmt.Sprintf("%d. Клиент: `%s`\n", i+1, whatsapp))
		sb.WriteString(fmt.Sprintf("   User ID: `%s`\n", userID))
		sb.WriteString(fmt.Sprintf("   Пароль: `%s`\n", password))
		sb.WriteString(fmt.Sprintf("   Сервер: %s\n", serverName))
		sb.WriteString(fmt.Sprintf("   Просрочено: %d дн.\n\n", daysOverdue))

		// Кнопки для этой подписки (3 в ряд)
		row := []tgbotapi.InlineKeyboardButton{}

		// 1. WhatsApp
		if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
			whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекла. Для продолжения работы необходимо оплатить подписку.")
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("💬", whatsappLink))
		}

		// 2. Сервер (URL кнопка)
		if serverURL != "" {
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("🌐", serverURL))
		}

		// 3. Отключил (callback)
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✅ Отключил", fmt.Sprintf("exp_dis:%d", sub.ID)))

		if len(row) > 0 {
			allRows = append(allRows, row)
		}
	}

	sb.WriteString("Отключите клиентов в WireGuard и напомните об оплате.")

	msg := tgbotapi.NewMessage(assistantTelegramID, sb.String())
	msg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		msg.ReplyMarkup = keyboard
	}

	_, err := w.telegramBot.Send(msg)
	return err
}

// markExpiredSubscriptions marks expired subscriptions as expired in DB
func (w *Worker) markExpiredSubscriptions(ctx context.Context) error {
	subscriptions, err := w.storage.ListExpiredSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("list expired subscriptions: %w", err)
	}

	w.logger.Info("Marking expired subscriptions", "count", len(subscriptions))

	expiredStatus := subs.StatusExpired
	for _, sub := range subscriptions {
		criteria := subs.GetCriteria{IDs: []int64{sub.ID}}
		params := subs.UpdateParams{Status: &expiredStatus}

		_, err := w.storage.UpdateSubscription(ctx, criteria, params)
		if err != nil {
			w.logger.Error("Failed to expire subscription",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}

		w.logger.Info("Subscription expired",
			"subscription_id", sub.ID,
			"user_id", sub.UserID)
	}

	return nil
}

// generateWhatsAppLink generates a WhatsApp link with pre-filled message
func generateWhatsAppLink(phone string, message string) string {
	// Remove + from the beginning for WhatsApp API
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")

	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}
