package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type ExpirationCommand struct {
	bot            *tgbotapi.BotAPI
	subStorage     ExpirationSubStorage
	serverStorage  ExpirationServerStorage
	tariffService  ExpirationTariffService
	paymentService ExpirationPaymentService
	logger         *slog.Logger
}

type ExpirationSubStorage interface {
	ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error)
	ListExpiringSubscriptions(ctx context.Context, daysUntilExpiry int) ([]*subs.Subscription, error)
	UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
	GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error)
	ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
}

type ExpirationServerStorage interface {
	GetServer(ctx context.Context, criteria servers.GetCriteria) (*servers.Server, error)
	DecrementServerUsers(ctx context.Context, serverID int64) error
}

type ExpirationTariffService interface {
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
}

type ExpirationPaymentService interface {
	CreatePayment(ctx context.Context, p payment.Payment) (*payment.Payment, error)
}

func NewExpirationCommand(
	bot *tgbotapi.BotAPI,
	subStorage ExpirationSubStorage,
	serverStorage ExpirationServerStorage,
	tariffService ExpirationTariffService,
	paymentService ExpirationPaymentService,
	logger *slog.Logger,
) *ExpirationCommand {
	return &ExpirationCommand{
		bot:            bot,
		subStorage:     subStorage,
		serverStorage:  serverStorage,
		tariffService:  tariffService,
		paymentService: paymentService,
		logger:         logger,
	}
}

// ExecuteOverdue показывает просроченные подписки с кнопками
func (c *ExpirationCommand) ExecuteOverdue(ctx context.Context, chatID int64) error {
	subscriptions, err := c.subStorage.ListExpiredSubscriptions(ctx)
	if err != nil {
		c.logger.Error("Failed to list expired subscriptions", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки просроченных подписок")
		_, _ = c.bot.Send(msg)
		return err
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, "✅ Нет просроченных подписок")
		_, _ = c.bot.Send(msg)
		return nil
	}

	return c.sendOverdueList(ctx, chatID, subscriptions)
}

// ExecuteExpiring показывает истекающие сегодня подписки с кнопками
func (c *ExpirationCommand) ExecuteExpiring(ctx context.Context, chatID int64) error {
	subscriptions, err := c.subStorage.ListExpiringSubscriptions(ctx, 0) // 0 = сегодня
	if err != nil {
		c.logger.Error("Failed to list expiring subscriptions", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки истекающих подписок")
		_, _ = c.bot.Send(msg)
		return err
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, "✅ Нет подписок, истекающих сегодня")
		_, _ = c.bot.Send(msg)
		return nil
	}

	return c.sendExpiringList(ctx, chatID, subscriptions)
}

// HandleCallback обрабатывает callback кнопок exp_*
func (c *ExpirationCommand) HandleCallback(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery) error {
	chatID := callbackQuery.Message.Chat.ID
	messageID := callbackQuery.Message.MessageID
	callbackData := callbackQuery.Data

	// Парсим callback data: exp_dis:123 или exp_pay:123 или exp_chk:123
	parts := strings.Split(callbackData, ":")
	if len(parts) != 2 {
		return c.answerCallback(callbackQuery.ID, "Неверный формат")
	}

	subID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
	}

	action := parts[0]
	switch action {
	case "exp_dis":
		return c.handleDisable(ctx, callbackQuery, chatID, messageID, subID)
	case "exp_pay":
		return c.handleCreatePayment(ctx, callbackQuery, chatID, subID)
	case "exp_chk":
		return c.handleCheckPayment(ctx, callbackQuery, chatID, messageID, subID)
	default:
		return c.answerCallback(callbackQuery.ID, "Неизвестная команда")
	}
}

// handleDisable - кнопка "Отключил"
func (c *ExpirationCommand) handleDisable(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// 1. Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// 2. Установить статус disabled
	disabledStatus := subs.StatusDisabled
	_, err = c.subStorage.UpdateSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}}, subs.UpdateParams{
		Status: &disabledStatus,
	})
	if err != nil {
		c.logger.Error("Failed to disable subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Ошибка обновления")
	}

	// 3. Уменьшить current_users на сервере
	if sub.ServerID != nil {
		if err := c.serverStorage.DecrementServerUsers(ctx, *sub.ServerID); err != nil {
			c.logger.Error("Failed to decrement server users", "error", err, "server_id", *sub.ServerID)
		}
	}

	c.logger.Info("Subscription disabled", "sub_id", subID)

	// 4. Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "✅ Подписка отключена"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// 5. Обновить список (убрать эту подписку)
	return c.refreshOverdueMessage(ctx, chatID, messageID)
}

// handleCreatePayment - кнопка "Ссылка на оплату"
func (c *ExpirationCommand) handleCreatePayment(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, subID int64) error {
	// 1. Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// 2. Получить тариф для определения цены
	tariff, err := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})
	if err != nil || tariff == nil {
		c.logger.Error("Failed to get tariff", "error", err, "tariff_id", sub.TariffID)
		return c.answerCallback(callbackQuery.ID, "Тариф не найден")
	}

	// 3. Создать платеж
	paymentEntity := payment.Payment{
		UserID: sub.UserID,
		Amount: tariff.Price,
		Status: payment.StatusPending,
	}

	paymentObj, err := c.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		c.logger.Error("Failed to create payment", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Ошибка создания платежа")
	}

	if paymentObj.PaymentURL == nil || *paymentObj.PaymentURL == "" {
		c.logger.Error("Payment URL is empty", "payment_id", paymentObj.ID)
		return c.answerCallback(callbackQuery.ID, "Ссылка на оплату недоступна")
	}

	// 4. Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "Ссылка создана"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// 5. Отправить ссылку
	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	text := fmt.Sprintf(
		"💳 *Ссылка на оплату*\n\n"+
			"Клиент: `%s`\n"+
			"Тариф: %s\n"+
			"Сумма: %.0f ₽\n\n"+
			"🔗 %s",
		whatsapp, tariff.Name, tariff.Price, *paymentObj.PaymentURL)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, err = c.bot.Send(msg)
	return err
}

// handleCheckPayment - кнопка "Оплатил" (подтверждение и продление)
func (c *ExpirationCommand) handleCheckPayment(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// 1. Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// 2. Получить тариф для продления
	tariff, err := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})
	if err != nil || tariff == nil {
		c.logger.Error("Failed to get tariff", "error", err, "tariff_id", sub.TariffID)
		return c.answerCallback(callbackQuery.ID, "Тариф не найден")
	}

	// 3. Продлить подписку
	if err := c.subStorage.ExtendSubscription(ctx, subID, tariff.DurationDays); err != nil {
		c.logger.Error("Failed to extend subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Ошибка продления")
	}

	// 4. Установить статус active (если был expired)
	activeStatus := subs.StatusActive
	_, err = c.subStorage.UpdateSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}}, subs.UpdateParams{
		Status: &activeStatus,
	})
	if err != nil {
		c.logger.Error("Failed to update subscription status", "error", err, "sub_id", subID)
	}

	c.logger.Info("Subscription extended", "sub_id", subID, "days", tariff.DurationDays)

	// 5. Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "✅ Подписка продлена"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// 6. Отправить подтверждение
	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	text := fmt.Sprintf("✅ *Подписка продлена!*\n\nКлиент: `%s`\nПродлено на: %d дней",
		whatsapp, tariff.DurationDays)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, _ = c.bot.Send(msg)

	// 7. Обновить список истекающих
	return c.refreshExpiringMessage(ctx, chatID, messageID)
}

// sendOverdueList отправляет список просроченных подписок с кнопками
func (c *ExpirationCommand) sendOverdueList(ctx context.Context, chatID int64, subscriptions []*subs.Subscription) error {
	var sb strings.Builder
	sb.WriteString("⚠️ *Просроченные подписки:*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		// Получаем сервер
		var server *servers.Server
		if sub.ServerID != nil {
			server, _ = c.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
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

	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		msg.ReplyMarkup = keyboard
	}

	_, err := c.bot.Send(msg)
	return err
}

// sendExpiringList отправляет список истекающих подписок с кнопками
func (c *ExpirationCommand) sendExpiringList(ctx context.Context, chatID int64, subscriptions []*subs.Subscription) error {
	var sb strings.Builder
	sb.WriteString("🔔 *Подписки истекают сегодня:*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		tariff, _ := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

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

	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		msg.ReplyMarkup = keyboard
	}

	_, err := c.bot.Send(msg)
	return err
}

// refreshOverdueMessage обновляет сообщение со списком просроченных
func (c *ExpirationCommand) refreshOverdueMessage(ctx context.Context, chatID int64, messageID int) error {
	subscriptions, err := c.subStorage.ListExpiredSubscriptions(ctx)
	if err != nil {
		c.logger.Error("Failed to list expired subscriptions", "error", err)
		return err
	}

	if len(subscriptions) == 0 {
		// Удаляем кнопки и обновляем текст
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, "✅ Все просроченные подписки обработаны!")
		_, err = c.bot.Send(editMsg)
		return err
	}

	// Пересоздаём сообщение с обновлённым списком
	var sb strings.Builder
	sb.WriteString("⚠️ *Просроченные подписки:*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		var server *servers.Server
		if sub.ServerID != nil {
			server, _ = c.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
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

		row := []tgbotapi.InlineKeyboardButton{}
		if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
			whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекла. Для продолжения работы необходимо оплатить подписку.")
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("💬", whatsappLink))
		}
		if serverURL != "" {
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("🌐", serverURL))
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✅ Отключил", fmt.Sprintf("exp_dis:%d", sub.ID)))

		if len(row) > 0 {
			allRows = append(allRows, row)
		}
	}

	sb.WriteString("Отключите клиентов в WireGuard и напомните об оплате.")

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, sb.String())
	editMsg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		editMsg.ReplyMarkup = &keyboard
	}

	_, err = c.bot.Send(editMsg)
	return err
}

// refreshExpiringMessage обновляет сообщение со списком истекающих
func (c *ExpirationCommand) refreshExpiringMessage(ctx context.Context, chatID int64, messageID int) error {
	subscriptions, err := c.subStorage.ListExpiringSubscriptions(ctx, 1)
	if err != nil {
		c.logger.Error("Failed to list expiring subscriptions", "error", err)
		return err
	}

	if len(subscriptions) == 0 {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, "✅ Все истекающие подписки обработаны!")
		_, err = c.bot.Send(editMsg)
		return err
	}

	var sb strings.Builder
	sb.WriteString("🔔 *Подписки истекают сегодня:*\n\n")

	var allRows [][]tgbotapi.InlineKeyboardButton

	for i, sub := range subscriptions {
		tariff, _ := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

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

		row := []tgbotapi.InlineKeyboardButton{}
		if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
			whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекает сегодня. Хотите продлить?")
			row = append(row, tgbotapi.NewInlineKeyboardButtonURL("💬", whatsappLink))
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("💳 Оплата", fmt.Sprintf("exp_pay:%d", sub.ID)))
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✅ Оплатил", fmt.Sprintf("exp_chk:%d", sub.ID)))

		if len(row) > 0 {
			allRows = append(allRows, row)
		}
	}

	sb.WriteString("Напишите клиентам о продлении подписки.")

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, sb.String())
	editMsg.ParseMode = "Markdown"
	if len(allRows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(allRows...)
		editMsg.ReplyMarkup = &keyboard
	}

	_, err = c.bot.Send(editMsg)
	return err
}

// answerCallback отвечает на callback query
func (c *ExpirationCommand) answerCallback(callbackID string, text string) error {
	callback := tgbotapi.NewCallback(callbackID, text)
	_, err := c.bot.Request(callback)
	return err
}

// generateWhatsAppLink генерирует ссылку на WhatsApp с предзаполненным сообщением
func generateWhatsAppLink(phone string, message string) string {
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")
	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}
