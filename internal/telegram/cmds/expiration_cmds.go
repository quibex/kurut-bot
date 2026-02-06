package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/webtokens"
	"kurut-bot/internal/telegram/messages"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type ExpirationCommand struct {
	bot                 *tgbotapi.BotAPI
	subStorage          ExpirationSubStorage
	serverStorage       ExpirationServerStorage
	tariffService       ExpirationTariffService
	paymentService      ExpirationPaymentService
	messageStorage      ExpirationMessageStorage
	clientTokenStorage  ExpirationClientTokenStorage
	notificationService *ExpirationNotificationService
	webDomain           string
	logger              *slog.Logger
}

type ExpirationClientTokenStorage interface {
	GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64) (*webtokens.ClientToken, error)
}

type ExpirationSubStorage interface {
	ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error)
	ListExpiringSubscriptions(ctx context.Context, daysUntilExpiry int) ([]*subs.Subscription, error)
	ListExpiredSubscriptionsByAssistant(ctx context.Context, assistantTelegramID *int64) ([]*subs.Subscription, error)
	ListExpiringSubscriptionsByAssistant(ctx context.Context, assistantTelegramID *int64, daysUntilExpiry int) ([]*subs.Subscription, error)
	UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
	GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error)
	ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
	UpdateSubscriptionTariff(ctx context.Context, subscriptionID int64, tariffID int64) error
}

type ExpirationServerStorage interface {
	GetServer(ctx context.Context, criteria servers.GetCriteria) (*servers.Server, error)
	// IncrementServerUsers и DecrementServerUsers больше не нужны - счетчик считается динамически
}

type ExpirationTariffService interface {
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
}

type ExpirationPaymentService interface {
	CreatePayment(ctx context.Context, p payment.Payment) (*payment.Payment, error)
	CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
	IsManualPayment() bool
}

type ExpirationMessageStorage interface {
	CreateSubscriptionMessage(ctx context.Context, msg submessages.SubscriptionMessage) (*submessages.SubscriptionMessage, error)
	GetSubscriptionMessageByID(ctx context.Context, id int64) (*submessages.SubscriptionMessage, error)
	GetSubscriptionMessageByChatAndMessageID(ctx context.Context, chatID int64, messageID int) (*submessages.SubscriptionMessage, error)
	ListActiveSubscriptionMessages(ctx context.Context, subscriptionID int64) ([]*submessages.SubscriptionMessage, error)
	DeactivateSubscriptionMessage(ctx context.Context, id int64) error
	DeactivateAllSubscriptionMessages(ctx context.Context, subscriptionID int64) error
	UpdateSelectedTariff(ctx context.Context, id int64, tariffID *int64) error
	UpdatePaymentID(ctx context.Context, id int64, paymentID *int64) error
}

func NewExpirationCommand(
	bot *tgbotapi.BotAPI,
	subStorage ExpirationSubStorage,
	serverStorage ExpirationServerStorage,
	tariffService ExpirationTariffService,
	paymentService ExpirationPaymentService,
	messageStorage ExpirationMessageStorage,
	clientTokenStorage ExpirationClientTokenStorage,
	webDomain string,
	notificationService *ExpirationNotificationService,
	logger *slog.Logger,
) *ExpirationCommand {
	return &ExpirationCommand{
		bot:                 bot,
		subStorage:          subStorage,
		serverStorage:       serverStorage,
		tariffService:       tariffService,
		paymentService:      paymentService,
		messageStorage:      messageStorage,
		clientTokenStorage:  clientTokenStorage,
		webDomain:           webDomain,
		notificationService: notificationService,
		logger:              logger,
	}
}

func (c *ExpirationCommand) paidButtonText() string {
	if c.paymentService.IsManualPayment() {
		return "✅ Оплачено"
	}
	return "✅ Проверить"
}

// ExecuteOverdue показывает просроченные подписки с кнопками
// assistantTelegramID nil = показать все (для админов)
func (c *ExpirationCommand) ExecuteOverdue(ctx context.Context, chatID int64, assistantTelegramID *int64) error {
	subscriptions, err := c.subStorage.ListExpiredSubscriptionsByAssistant(ctx, assistantTelegramID)
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

	return c.sendOverdueMessages(ctx, chatID, subscriptions)
}

// ExecuteExpiring показывает истекающие сегодня подписки с кнопками
// assistantTelegramID nil = показать все (для админов)
func (c *ExpirationCommand) ExecuteExpiring(ctx context.Context, chatID int64, assistantTelegramID *int64) error {
	subscriptions, err := c.subStorage.ListExpiringSubscriptionsByAssistant(ctx, assistantTelegramID, 0) // 0 = сегодня
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

	return c.sendExpiringMessages(ctx, chatID, subscriptions)
}

// ExecuteExp3 показывает истекающие через 3 дня подписки с кнопками
// assistantTelegramID nil = показать все (для админов)
func (c *ExpirationCommand) ExecuteExp3(ctx context.Context, chatID int64, assistantTelegramID *int64) error {
	subscriptions, err := c.subStorage.ListExpiringSubscriptionsByAssistant(ctx, assistantTelegramID, 3) // 3 = через 3 дня
	if err != nil {
		c.logger.Error("Failed to list subscriptions expiring in 3 days", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки истекающих подписок")
		_, _ = c.bot.Send(msg)
		return err
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, "✅ Нет подписок, истекающих через 3 дня")
		_, _ = c.bot.Send(msg)
		return nil
	}

	return c.sendExp3Messages(ctx, chatID, subscriptions)
}

// sendExp3Messages отправляет сводку и отдельные сообщения для подписок, истекающих через 3 дня
func (c *ExpirationCommand) sendExp3Messages(ctx context.Context, chatID int64, subscriptions []*subs.Subscription) error {
	// Сводное сообщение
	summaryText := fmt.Sprintf("🔔 *У вас %d подписок, истекающих через 3 дня*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions))
	summaryMsg := tgbotapi.NewMessage(chatID, summaryText)
	summaryMsg.ParseMode = "Markdown"
	_, _ = c.bot.Send(summaryMsg)

	// Отдельные сообщения для каждой подписки через notification service
	for _, sub := range subscriptions {
		if err := c.notificationService.SendExpiringSubscriptionMessage(ctx, chatID, sub, 3); err != nil {
			c.logger.Error("Failed to send expiring subscription message", "error", err, "sub_id", sub.ID)
		}
	}

	return nil
}

// HandleCallback обрабатывает callback кнопок exp_*
func (c *ExpirationCommand) HandleCallback(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery) error {
	chatID := callbackQuery.Message.Chat.ID
	messageID := callbackQuery.Message.MessageID
	callbackData := callbackQuery.Data

	// Парсим callback data
	parts := strings.Split(callbackData, ":")
	if len(parts) < 2 {
		return c.answerCallback(callbackQuery.ID, "Неверный формат")
	}

	action := parts[0]

	switch action {
	case "exp_dis":
		// exp_dis:subID
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		return c.handleDisable(ctx, callbackQuery, chatID, messageID, subID)
	case "exp_link":
		// exp_link:subID
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		return c.handleCreatePayment(ctx, callbackQuery, chatID, messageID, subID)
	case "exp_paid":
		// exp_paid:subID
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		return c.handleCheckPayment(ctx, callbackQuery, chatID, messageID, subID)
	case "exp_tariff":
		// exp_tariff:subID - показать список тарифов
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		return c.handleShowTariffs(ctx, callbackQuery, chatID, messageID, subID)
	case "exp_set_tariff":
		// exp_set_tariff:subID:tariffID - установить тариф
		if len(parts) != 3 {
			return c.answerCallback(callbackQuery.ID, "Неверный формат")
		}
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		tariffID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID тарифа")
		}
		return c.handleSetTariff(ctx, callbackQuery, chatID, messageID, subID, tariffID)
	case "exp_server":
		// exp_server:subID - показать сервер после активации
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		return c.handleShowServer(ctx, callbackQuery, subID)
	case "exp_tariff_back":
		// exp_tariff_back:subID - вернуться к основным кнопкам
		subID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
		}
		return c.handleTariffBack(ctx, callbackQuery, chatID, messageID, subID)
	default:
		// Старые callbacks для совместимости
		if strings.HasPrefix(callbackData, "exp_chk:") || strings.HasPrefix(callbackData, "exp_pay:") {
			subID, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return c.answerCallback(callbackQuery.ID, "Неверный ID подписки")
			}
			if action == "exp_chk" {
				return c.handleCheckPayment(ctx, callbackQuery, chatID, messageID, subID)
			}
			return c.handleCreatePayment(ctx, callbackQuery, chatID, messageID, subID)
		}
		return c.answerCallback(callbackQuery.ID, "Неизвестная команда")
	}
}

// sendOverdueMessages отправляет сводку и отдельные сообщения для каждой просроченной подписки
func (c *ExpirationCommand) sendOverdueMessages(ctx context.Context, chatID int64, subscriptions []*subs.Subscription) error {
	// Сводное сообщение
	summaryText := fmt.Sprintf("⚠️ *У вас %d просроченных подписок*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions))
	summaryMsg := tgbotapi.NewMessage(chatID, summaryText)
	summaryMsg.ParseMode = "Markdown"
	_, _ = c.bot.Send(summaryMsg)

	// Отдельные сообщения для каждой подписки через notification service
	for _, sub := range subscriptions {
		if err := c.notificationService.SendOverdueSubscriptionMessage(ctx, chatID, sub); err != nil {
			c.logger.Error("Failed to send overdue subscription message", "error", err, "sub_id", sub.ID)
		}
	}

	return nil
}

// sendExpiringMessages отправляет сводку и отдельные сообщения для каждой истекающей подписки
func (c *ExpirationCommand) sendExpiringMessages(ctx context.Context, chatID int64, subscriptions []*subs.Subscription) error {
	// Сводное сообщение
	summaryText := fmt.Sprintf("🔔 *У вас %d подписок, истекающих сегодня*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions))
	summaryMsg := tgbotapi.NewMessage(chatID, summaryText)
	summaryMsg.ParseMode = "Markdown"
	_, _ = c.bot.Send(summaryMsg)

	// Отдельные сообщения для каждой подписки через notification service
	// Передаем 0 потому что команда /expiring показывает подписки истекающие сегодня
	for _, sub := range subscriptions {
		if err := c.notificationService.SendExpiringSubscriptionMessage(ctx, chatID, sub, 0); err != nil {
			c.logger.Error("Failed to send expiring subscription message", "error", err, "sub_id", sub.ID)
		}
	}

	return nil
}

// handleDisable - кнопка "Отключить"
func (c *ExpirationCommand) handleDisable(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// Проверяем актуальность сообщения
	if active, err := c.checkMessageActive(ctx, chatID, messageID); !active {
		if err != nil {
			c.logger.Error("Failed to check message active", "error", err)
		}
		return c.markMessageOutdated(chatID, messageID, callbackQuery.ID)
	}

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

	// 3. Счетчик пользователей на сервере теперь считается динамически (не нужен декремент)

	c.logger.Info("Subscription disabled", "sub_id", subID)

	// 4. Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "✅ Подписка отключена"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// 5. Обновить это сообщение с новыми кнопками для продления
	return c.updateToDisabledMessage(ctx, chatID, messageID, sub)
}

// updateToDisabledMessage обновляет сообщение после отключения подписки
func (c *ExpirationCommand) updateToDisabledMessage(ctx context.Context, chatID int64, messageID int, sub *subs.Subscription) error {
	tariff, _ := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})

	var server *servers.Server
	if sub.ServerID != nil {
		server, _ = c.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
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
		clientToken, err := c.clientTokenStorage.GetOrCreateClientToken(ctx, *sub.ClientWhatsApp, createdByTgID)
		if err != nil {
			c.logger.Error("Failed to get client token", "error", err)
		} else {
			clientLink = fmt.Sprintf("%s/c/%s", c.webDomain, clientToken.Token)
		}
	}

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Здравствуйте! Ваша подписка VPN истекла. Для продолжения работы необходимо оплатить подписку.")
		if clientLink != "" {
			text = fmt.Sprintf(
				"⏸ *Подписка отключена*\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 Тариф: %s (%.0f ₽)%s\n\n"+
					"🔗 [Ссылка для оплаты](%s)",
				whatsapp, whatsappLink, tariffName, price, passwordLine, clientLink)
		} else {
			text = fmt.Sprintf(
				"⏸ *Подписка отключена*\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 Тариф: %s (%.0f ₽)%s",
				whatsapp, whatsappLink, tariffName, price, passwordLine)
		}
	} else {
		text = fmt.Sprintf(
			"⏸ *Подписка отключена*\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s (%.0f ₽)%s",
			whatsapp, tariffName, price, passwordLine)
	}

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.DisableWebPagePreview = true
	_, err := c.bot.Send(editMsg)

	// Деактивируем все другие сообщения для этой подписки
	c.deactivateOtherMessages(ctx, sub.ID, chatID, messageID)

	return err
}

// handleCreatePayment - кнопка "Получить ссылку" (теперь возвращает ссылку на веб-продление)
func (c *ExpirationCommand) handleCreatePayment(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// Проверяем актуальность сообщения
	if active, err := c.checkMessageActive(ctx, chatID, messageID); !active {
		if err != nil {
			c.logger.Error("Failed to check message active", "error", err)
		}
		return c.markMessageOutdated(chatID, messageID, callbackQuery.ID)
	}

	// 1. Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// 2. Проверить selected_tariff из сообщения
	subMsg, _ := c.messageStorage.GetSubscriptionMessageByChatAndMessageID(ctx, chatID, messageID)

	tariffID := sub.TariffID
	if subMsg != nil && subMsg.SelectedTariffID != nil {
		tariffID = *subMsg.SelectedTariffID
	}

	// 3. Получить тариф для определения цены
	tariff, err := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil || tariff == nil {
		c.logger.Error("Failed to get tariff", "error", err, "tariff_id", tariffID)
		return c.answerCallback(callbackQuery.ID, "Тариф не найден")
	}

	// 4. Получить WhatsApp клиента и Telegram ID ассистента для создания client token
	whatsapp := ""
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}
	if whatsapp == "" {
		c.logger.Error("Subscription has no client WhatsApp", "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "У клиента не указан WhatsApp")
	}

	createdByTgID := int64(0)
	if sub.CreatedByTelegramID != nil {
		createdByTgID = *sub.CreatedByTelegramID
	}

	clientToken, err := c.clientTokenStorage.GetOrCreateClientToken(ctx, whatsapp, createdByTgID)
	if err != nil {
		c.logger.Error("Failed to get/create client token", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Ошибка создания ссылки")
	}

	// 5. Сформировать личную ссылку клиента
	renewalURL := fmt.Sprintf("%s/c/%s", c.webDomain, clientToken.Token)

	// 6. Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "Ссылка создана"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// 7. Редактировать сообщение со ссылкой
	whatsappDisplay := whatsapp
	if whatsappDisplay == "" {
		whatsappDisplay = "Не указан"
	}

	// Формируем текст со ссылкой как кликабельный alias "link"
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, messages.WhatsAppMsgExpired)
		text = fmt.Sprintf(
			"🔗 *Ссылка на продление*\n\n"+
				"📱 Клиент: [%s](%s)\n"+
				"📅 Тариф: %s\n"+
				"💰 Сумма: %.0f ₽\n\n"+
				"🌐 [link](%s)",
			whatsappDisplay, whatsappLink, tariff.Name, tariff.Price, renewalURL)
	} else {
		text = fmt.Sprintf(
			"🔗 *Ссылка на продление*\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s\n"+
				"💰 Сумма: %.0f ₽\n\n"+
				"🌐 [link](%s)",
			whatsappDisplay, tariff.Name, tariff.Price, renewalURL)
	}

	// Кнопки: Сменить тариф, Новый
	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Сменить тариф", fmt.Sprintf("exp_tariff:%d", sub.ID)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔗 Новый", fmt.Sprintf("exp_link:%d", sub.ID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &keyboard
	editMsg.DisableWebPagePreview = true
	_, err = c.bot.Send(editMsg)

	return err
}

// handleCheckPayment - кнопка "Оплачено/Проверить" (проверка оплаты и продление)
func (c *ExpirationCommand) handleCheckPayment(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// Проверяем актуальность сообщения
	if active, err := c.checkMessageActive(ctx, chatID, messageID); !active {
		if err != nil {
			c.logger.Error("Failed to check message active", "error", err)
		}
		return c.markMessageOutdated(chatID, messageID, callbackQuery.ID)
	}

	// 1. Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// 2. Проверить selected_tariff из сообщения
	subMsg, _ := c.messageStorage.GetSubscriptionMessageByChatAndMessageID(ctx, chatID, messageID)

	tariffID := sub.TariffID
	if subMsg != nil && subMsg.SelectedTariffID != nil {
		tariffID = *subMsg.SelectedTariffID
		// Обновляем тариф подписки
		if err := c.subStorage.UpdateSubscriptionTariff(ctx, subID, tariffID); err != nil {
			c.logger.Error("Failed to update subscription tariff", "error", err, "sub_id", subID, "tariff_id", tariffID)
		}
	}

	// 3. Получить тариф для продления
	tariff, err := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil || tariff == nil {
		c.logger.Error("Failed to get tariff", "error", err, "tariff_id", tariffID)
		return c.answerCallback(callbackQuery.ID, "Тариф не найден")
	}

	// 4. Проверить/создать платёж в зависимости от режима
	if c.paymentService.IsManualPayment() {
		// Mock режим: создаём approved платёж если не было ссылки
		if subMsg == nil || subMsg.PaymentID == nil {
			paymentEntity := payment.Payment{
				UserID: sub.UserID,
				Amount: tariff.Price,
				Status: payment.StatusPending,
			}
			_, err := c.paymentService.CreatePayment(ctx, paymentEntity)
			if err != nil {
				c.logger.Error("Failed to create payment", "error", err, "sub_id", subID)
				return c.answerCallback(callbackQuery.ID, "Ошибка создания платежа")
			}
		}
	} else {
		// Real режим: требуем ссылку и проверяем YooKassa
		if subMsg == nil || subMsg.PaymentID == nil {
			alertConfig := tgbotapi.NewCallbackWithAlert(callbackQuery.ID, "Сначала создайте ссылку на оплату")
			_, _ = c.bot.Request(alertConfig)
			return nil
		}
		paymentObj, err := c.paymentService.CheckPaymentStatus(ctx, *subMsg.PaymentID)
		if err != nil {
			c.logger.Error("Failed to check payment status", "error", err, "payment_id", *subMsg.PaymentID)
			return c.answerCallback(callbackQuery.ID, "Ошибка проверки платежа")
		}
		if paymentObj.Status != payment.StatusApproved {
			alertConfig := tgbotapi.NewCallbackWithAlert(callbackQuery.ID, "⏳ Платёж ещё не оплачен")
			_, _ = c.bot.Request(alertConfig)
			return nil
		}
	}

	// 5. Продлить подписку
	if err := c.subStorage.ExtendSubscription(ctx, subID, tariff.DurationDays); err != nil {
		c.logger.Error("Failed to extend subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Ошибка продления")
	}

	// 6. Установить статус active (если был expired/disabled)
	activeStatus := subs.StatusActive
	_, err = c.subStorage.UpdateSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}}, subs.UpdateParams{
		Status: &activeStatus,
	})
	if err != nil {
		c.logger.Error("Failed to update subscription status", "error", err, "sub_id", subID)
	}

	// 7. Счетчик пользователей на сервере теперь считается динамически (не нужен инкремент)

	c.logger.Info("Subscription extended", "sub_id", subID, "days", tariff.DurationDays)

	// 8. Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "✅ Подписка продлена"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// 9. Обновить сообщение
	wasDisabled := sub.Status == subs.StatusDisabled
	return c.updateToRenewedMessage(ctx, chatID, messageID, sub, tariff, wasDisabled)
}

// updateToRenewedMessage обновляет сообщение после продления подписки
func (c *ExpirationCommand) updateToRenewedMessage(ctx context.Context, chatID int64, messageID int, sub *subs.Subscription, tariff *tariffs.Tariff, wasDisabled bool) error {
	var server *servers.Server
	if sub.ServerID != nil {
		server, _ = c.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
	}

	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	// Формируем строку пароля только если был disabled и есть сервер
	passwordLine := ""
	if wasDisabled && server != nil && server.UIPassword != "" {
		passwordLine = fmt.Sprintf("\n🔐 Пароль: `%s`", server.UIPassword)
	}

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, "Ваша подписка VPN продлена!")
		text = fmt.Sprintf(
			"✅ *Подписка продлена!*\n\n"+
				"📱 Клиент: [%s](%s)\n"+
				"📅 Тариф: %s\n"+
				"⏱ Продлено на: %d дней%s",
			whatsapp, whatsappLink, tariff.Name, tariff.DurationDays, passwordLine)
	} else {
		text = fmt.Sprintf(
			"✅ *Подписка продлена!*\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s\n"+
				"⏱ Продлено на: %d дней%s",
			whatsapp, tariff.Name, tariff.DurationDays, passwordLine)
	}

	// Кнопка для перехода на сервер - только если подписка была отключена
	var rows [][]tgbotapi.InlineKeyboardButton
	if wasDisabled && server != nil && server.UIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🌐 Сервер", server.UIURL),
		))
	}

	var keyboard *tgbotapi.InlineKeyboardMarkup
	if len(rows) > 0 {
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		keyboard = &kb
	}

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = keyboard
	editMsg.DisableWebPagePreview = true
	_, err := c.bot.Send(editMsg)

	// Деактивируем все сообщения для этой подписки
	c.deactivateAllMessages(ctx, sub.ID)

	return err
}

// handleShowTariffs - кнопка "Сменить тариф"
func (c *ExpirationCommand) handleShowTariffs(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// Проверяем актуальность сообщения
	if active, err := c.checkMessageActive(ctx, chatID, messageID); !active {
		if err != nil {
			c.logger.Error("Failed to check message active", "error", err)
		}
		return c.markMessageOutdated(chatID, messageID, callbackQuery.ID)
	}

	// Получить активные тарифы
	tariffsList, err := c.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		c.logger.Error("Failed to get active tariffs", "error", err)
		return c.answerCallback(callbackQuery.ID, "Ошибка загрузки тарифов")
	}

	if len(tariffsList) == 0 {
		return c.answerCallback(callbackQuery.ID, "Нет активных тарифов")
	}

	// Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, "Выберите тариф"); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// Получить подписку для отображения информации
	sub, _ := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})

	whatsapp := "Не указан"
	if sub != nil && sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	text := fmt.Sprintf("📋 *Выберите тариф для продления*\n\n📱 Клиент: `%s`", whatsapp)

	// Создаем кнопки с тарифами
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, t := range tariffsList {
		buttonText := fmt.Sprintf("%s - %.0f ₽ (%d дн.)", t.Name, t.Price, t.DurationDays)
		callbackData := fmt.Sprintf("exp_set_tariff:%d:%d", subID, t.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText, callbackData),
		))
	}

	// Кнопка отмены
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", fmt.Sprintf("exp_tariff_back:%d", subID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &keyboard
	editMsg.DisableWebPagePreview = true
	_, err = c.bot.Send(editMsg)
	return err
}

// handleSetTariff - установка нового тарифа
func (c *ExpirationCommand) handleSetTariff(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID, tariffID int64) error {
	// Проверяем актуальность сообщения
	if active, err := c.checkMessageActive(ctx, chatID, messageID); !active {
		if err != nil {
			c.logger.Error("Failed to check message active", "error", err)
		}
		return c.markMessageOutdated(chatID, messageID, callbackQuery.ID)
	}

	// Получить тариф
	tariff, err := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil || tariff == nil {
		c.logger.Error("Failed to get tariff", "error", err, "tariff_id", tariffID)
		return c.answerCallback(callbackQuery.ID, "Тариф не найден")
	}

	// Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// Сохранить selected_tariff в сообщении
	subMsg, _ := c.messageStorage.GetSubscriptionMessageByChatAndMessageID(ctx, chatID, messageID)
	if subMsg != nil {
		if err := c.messageStorage.UpdateSelectedTariff(ctx, subMsg.ID, &tariffID); err != nil {
			c.logger.Error("Failed to update selected tariff", "error", err, "msg_id", subMsg.ID)
		}
	}

	// Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, fmt.Sprintf("Выбран тариф: %s", tariff.Name)); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// Обновить сообщение с новым тарифом
	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	msgType := submessages.TypeExpiring
	if subMsg != nil {
		msgType = subMsg.Type
	}

	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, messages.WhatsAppMsgToday)
		if msgType == submessages.TypeOverdue {
			text = fmt.Sprintf(
				"⏸ *Подписка отключена*\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 *Новый тариф: %s (%.0f ₽)*",
				whatsapp, whatsappLink, tariff.Name, tariff.Price)
		} else {
			text = fmt.Sprintf(
				"🔔 *Подписка истекает сегодня*\n\n"+
					"📱 Клиент: [%s](%s)\n"+
					"📅 *Новый тариф: %s (%.0f ₽)*",
				whatsapp, whatsappLink, tariff.Name, tariff.Price)
		}
	} else {
		if msgType == submessages.TypeOverdue {
			text = fmt.Sprintf(
				"⏸ *Подписка отключена*\n\n"+
					"📱 Клиент: `%s`\n"+
					"📅 *Новый тариф: %s (%.0f ₽)*",
				whatsapp, tariff.Name, tariff.Price)
		} else {
			text = fmt.Sprintf(
				"🔔 *Подписка истекает сегодня*\n\n"+
					"📱 Клиент: `%s`\n"+
					"📅 *Новый тариф: %s (%.0f ₽)*",
				whatsapp, tariff.Name, tariff.Price)
		}
	}

	// Кнопки: Сменить тариф, Ссылка/Оплачено
	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Сменить тариф", fmt.Sprintf("exp_tariff:%d", sub.ID)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔗 Ссылка", fmt.Sprintf("exp_link:%d", sub.ID)),
		tgbotapi.NewInlineKeyboardButtonData(c.paidButtonText(), fmt.Sprintf("exp_paid:%d", sub.ID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &keyboard
	editMsg.DisableWebPagePreview = true
	_, err = c.bot.Send(editMsg)
	return err
}

// handleShowServer - показать сервер
func (c *ExpirationCommand) handleShowServer(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, subID int64) error {
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil || sub.ServerID == nil {
		return c.answerCallback(callbackQuery.ID, "Сервер не найден")
	}

	server, err := c.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
	if err != nil || server == nil {
		return c.answerCallback(callbackQuery.ID, "Сервер не найден")
	}

	return c.answerCallback(callbackQuery.ID, "Откройте ссылку на сервер")
}

// handleTariffBack - вернуться из выбора тарифа к основным кнопкам
func (c *ExpirationCommand) handleTariffBack(ctx context.Context, callbackQuery *tgbotapi.CallbackQuery, chatID int64, messageID int, subID int64) error {
	// Проверяем актуальность сообщения
	if active, err := c.checkMessageActive(ctx, chatID, messageID); !active {
		if err != nil {
			c.logger.Error("Failed to check message active", "error", err)
		}
		return c.markMessageOutdated(chatID, messageID, callbackQuery.ID)
	}

	// Получить подписку
	sub, err := c.subStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil || sub == nil {
		c.logger.Error("Failed to get subscription", "error", err, "sub_id", subID)
		return c.answerCallback(callbackQuery.ID, "Подписка не найдена")
	}

	// Получить сообщение для определения типа
	subMsg, _ := c.messageStorage.GetSubscriptionMessageByChatAndMessageID(ctx, chatID, messageID)

	// Получить тариф (используем selected_tariff если есть)
	tariffID := sub.TariffID
	if subMsg != nil && subMsg.SelectedTariffID != nil {
		tariffID = *subMsg.SelectedTariffID
	}

	tariff, _ := c.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})

	// Ответить на callback
	if err := c.answerCallback(callbackQuery.ID, ""); err != nil {
		c.logger.Error("Failed to answer callback", "error", err)
	}

	// Определяем тип сообщения
	msgType := submessages.TypeExpiring
	if subMsg != nil {
		msgType = subMsg.Type
	}

	// Возвращаем соответствующее сообщение
	if msgType == submessages.TypeOverdue {
		return c.updateToDisabledMessage(ctx, chatID, messageID, sub)
	}
	return c.updateToExpiringMessage(ctx, chatID, messageID, sub, tariff)
}

// updateToExpiringMessage обновляет сообщение обратно к формату истекающей подписки
func (c *ExpirationCommand) updateToExpiringMessage(ctx context.Context, chatID int64, messageID int, sub *subs.Subscription, tariff *tariffs.Tariff) error {
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

	// Формируем текст со ссылкой на WhatsApp в номере клиента
	var text string
	if sub.ClientWhatsApp != nil && *sub.ClientWhatsApp != "" {
		whatsappLink := generateWhatsAppLink(*sub.ClientWhatsApp, messages.WhatsAppMsgToday)
		text = fmt.Sprintf(
			"🔔 *Подписка истекает сегодня*\n\n"+
				"📱 Клиент: [%s](%s)\n"+
				"📅 Тариф: %s (%.0f ₽)",
			whatsapp, whatsappLink, tariffName, price)
	} else {
		text = fmt.Sprintf(
			"🔔 *Подписка истекает сегодня*\n\n"+
				"📱 Клиент: `%s`\n"+
				"📅 Тариф: %s (%.0f ₽)",
			whatsapp, tariffName, price)
	}

	// Кнопки: Сменить тариф, Ссылка/Оплачено, Отказ
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Сменить тариф", fmt.Sprintf("exp_tariff:%d", sub.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔗 Ссылка", fmt.Sprintf("exp_link:%d", sub.ID)),
			tgbotapi.NewInlineKeyboardButtonData(c.paidButtonText(), fmt.Sprintf("exp_paid:%d", sub.ID)),
		),
	)

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &keyboard
	editMsg.DisableWebPagePreview = true
	_, err := c.bot.Send(editMsg)
	return err
}

// checkMessageActive проверяет, активно ли сообщение
func (c *ExpirationCommand) checkMessageActive(ctx context.Context, chatID int64, messageID int) (bool, error) {
	subMsg, err := c.messageStorage.GetSubscriptionMessageByChatAndMessageID(ctx, chatID, messageID)
	if err != nil {
		return true, err // При ошибке считаем активным чтобы не блокировать
	}
	if subMsg == nil {
		return true, nil // Если нет записи - считаем активным (старые сообщения)
	}
	return subMsg.IsActive, nil
}

// markMessageOutdated помечает сообщение как устаревшее
func (c *ExpirationCommand) markMessageOutdated(chatID int64, messageID int, callbackID string) error {
	// Ответить на callback
	_ = c.answerCallback(callbackID, "Это сообщение устарело")

	// Обновить сообщение
	text := "⚠️ *Это сообщение устарело*\n\nПодписка уже была обработана через другое сообщение."
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	_, _ = c.bot.Send(editMsg)
	return nil
}

// deactivateOtherMessages деактивирует все сообщения для подписки кроме указанного
func (c *ExpirationCommand) deactivateOtherMessages(ctx context.Context, subscriptionID int64, exceptChatID int64, exceptMessageID int) {
	messages, err := c.messageStorage.ListActiveSubscriptionMessages(ctx, subscriptionID)
	if err != nil {
		c.logger.Error("Failed to list active messages", "error", err, "sub_id", subscriptionID)
		return
	}

	for _, msg := range messages {
		if msg.ChatID == exceptChatID && msg.MessageID == exceptMessageID {
			continue
		}

		// Деактивировать в БД
		if err := c.messageStorage.DeactivateSubscriptionMessage(ctx, msg.ID); err != nil {
			c.logger.Error("Failed to deactivate message", "error", err, "msg_id", msg.ID)
			continue
		}

		// Обновить сообщение в Telegram
		text := "⚠️ *Это сообщение устарело*\n\nПодписка уже была обработана через другое сообщение."
		editMsg := tgbotapi.NewEditMessageText(msg.ChatID, msg.MessageID, text)
		editMsg.ParseMode = "Markdown"
		_, _ = c.bot.Send(editMsg)
	}
}

// deactivateAllMessages деактивирует все сообщения для подписки
func (c *ExpirationCommand) deactivateAllMessages(ctx context.Context, subscriptionID int64) {
	if err := c.messageStorage.DeactivateAllSubscriptionMessages(ctx, subscriptionID); err != nil {
		c.logger.Error("Failed to deactivate all messages", "error", err, "sub_id", subscriptionID)
	}
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
