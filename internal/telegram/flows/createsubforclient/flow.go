package createsubforclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot                 botApi
	stateManager        stateManager
	tariffService       tariffService
	subscriptionService subscriptionService
	subscriptionStorage subscriptionStorage
	paymentService      paymentService
	orderService        orderService
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	ss subscriptionService,
	storage subscriptionStorage,
	ps paymentService,
	os orderService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		subscriptionService: ss,
		subscriptionStorage: storage,
		paymentService:      ps,
		orderService:        os,
		logger:              logger,
	}
}

// Start начинает flow создания подписки для клиента
func (h *Handler) Start(userID, assistantTelegramID, chatID int64) error {
	// Инициализируем данные флоу
	flowData := &flows.CreateSubForClientFlowData{
		AdminUserID:         userID,
		AssistantTelegramID: assistantTelegramID,
	}
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitClientName, flowData)

	msg := tgbotapi.NewMessage(chatID, "📱 Введите номер WhatsApp клиента (например: +996555123456):")
	_, err := h.bot.Send(msg)
	return err
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminCreateSubWaitClientName:
		return h.handleWhatsAppInput(ctx, update)
	case states.AdminCreateSubWaitReferrer:
		return h.handleReferrerInput(ctx, update)
	case states.AdminCreateSubWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.AdminCreateSubWaitPayment:
		return h.handlePaymentConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown state: %s", state)
	}
}

// handleWhatsAppInput обрабатывает ввод номера WhatsApp
func (h *Handler) handleWhatsAppInput(ctx context.Context, update *tgbotapi.Update) error {
	if update.Message == nil || update.Message.Text == "" {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, введите номер WhatsApp текстом")
	}

	chatID := update.Message.Chat.ID
	whatsapp := strings.TrimSpace(update.Message.Text)

	// Сначала нормализуем, потом валидируем
	whatsapp = NormalizePhone(whatsapp)

	if !IsValidPhoneNumber(whatsapp) {
		return h.sendError(chatID, "❌ Неверный формат номера. Введите номер в формате +996555123456")
	}

	// Получаем данные флоу
	flowData, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Сохраняем WhatsApp номер
	flowData.ClientWhatsApp = whatsapp

	// Проверяем право на trial
	hasUsedTrial, err := h.subscriptionStorage.HasUsedTrialByPhone(ctx, whatsapp)
	if err != nil {
		h.logger.Warn("Failed to check trial status", "error", err, "whatsapp", whatsapp)
		flowData.IsTrialEligible = false
	} else {
		flowData.IsTrialEligible = !hasUsedTrial
		if flowData.IsTrialEligible {
			h.logger.Info("Client eligible for trial", "whatsapp", whatsapp)
		}
	}

	// Переводим в состояние ввода реферала
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitReferrer, flowData)

	// Показываем вопрос о реферале
	return h.showReferrerQuestion(chatID)
}

// showReferrerQuestion показывает вопрос о реферале
func (h *Handler) showReferrerQuestion(chatID int64) error {
	text := "👥 Есть номер того, кто пригласил клиента?\n\n" +
		"Бонус +10 дней при первой оплате!"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, есть", "ref_yes"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🤝 Партнёрка", "ref_partnership"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет", "ref_no"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("◀️ Отменить", "cancel"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard

	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	flowData, _ := h.stateManager.GetCreateSubForClientData(chatID)
	if flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.AdminCreateSubWaitReferrer, flowData)
	}

	return nil
}

// handleReferrerInput обрабатывает ввод реферального номера
func (h *Handler) handleReferrerInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	flowData, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обрабатываем callback кнопки
	if update.CallbackQuery != nil {
		callbackData := update.CallbackQuery.Data

		switch callbackData {
		case "ref_yes":
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = h.bot.Request(callbackConfig)

			// Редактируем сообщение с просьбой ввести номер реферала
			if flowData.MessageID != nil {
				editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID,
					"📱 Введите номер WhatsApp того, кто пригласил (например: +996555123456):")
				_, _ = h.bot.Send(editMsg)
			} else {
				msg := tgbotapi.NewMessage(chatID, "📱 Введите номер WhatsApp того, кто пригласил (например: +996555123456):")
				_, _ = h.bot.Send(msg)
			}
			return nil

		case "ref_no":
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = h.bot.Request(callbackConfig)

			// Переходим к выбору тарифа без реферала
			h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)
			return h.showTariffs(chatID)

		case "ref_partnership":
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = h.bot.Request(callbackConfig)

			// Помечаем как партнёрку и запрашиваем номер
			flowData.IsPartnership = true
			h.stateManager.SetState(chatID, states.AdminCreateSubWaitReferrer, flowData)

			if flowData.MessageID != nil {
				editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID,
					"🤝 Введите номер WhatsApp партнёра (например: +996555123456):")
				_, _ = h.bot.Send(editMsg)
			} else {
				msg := tgbotapi.NewMessage(chatID, "🤝 Введите номер WhatsApp партнёра (например: +996555123456):")
				_, _ = h.bot.Send(msg)
			}
			return nil

		case "cancel":
			return h.handleCancel(ctx, update)

		case "ref_retry":
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = h.bot.Request(callbackConfig)

			// Показываем запрос на ввод номера снова
			if flowData.MessageID != nil {
				editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID,
					"📱 Введите номер WhatsApp того, кто пригласил (например: +996555123456):")
				_, _ = h.bot.Send(editMsg)
			}
			return nil

		case "ref_skip":
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = h.bot.Request(callbackConfig)

			// Пропускаем реферала и переходим к тарифам
			h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)
			return h.showTariffs(chatID)
		}

		return nil
	}

	// Обрабатываем текстовый ввод номера реферала
	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите номер WhatsApp реферала")
	}

	referrerWhatsApp := strings.TrimSpace(update.Message.Text)

	// Сначала нормализуем, потом валидируем
	referrerWhatsApp = NormalizePhone(referrerWhatsApp)

	if !IsValidPhoneNumber(referrerWhatsApp) {
		return h.sendReferrerError(chatID, flowData, "❌ Неверный формат номера. Введите номер в формате +996555123456")
	}

	// Проверяем что клиент не указал свой же номер
	if referrerWhatsApp == flowData.ClientWhatsApp {
		return h.sendReferrerError(chatID, flowData, "❌ Нельзя указать номер клиента как реферала")
	}

	// Для партнёрки: просто сохраняем номер без проверки подписки и без бонуса
	if flowData.IsPartnership {
		flowData.ReferrerWhatsApp = &referrerWhatsApp
		// ReferrerSubscriptionID остаётся nil - бонус не начисляется
		h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)
		return h.showTariffs(chatID)
	}

	// Обычный реферал: ищем активную подписку по номеру реферала
	referrerSub, err := h.subscriptionService.FindActiveSubscriptionByWhatsApp(ctx, referrerWhatsApp)
	if err != nil {
		h.logger.Error("Failed to find referrer subscription", "error", err, "whatsapp", referrerWhatsApp)
		return h.sendReferrerError(chatID, flowData, "❌ Ошибка поиска реферала. Попробуйте снова.")
	}

	if referrerSub == nil {
		return h.sendReferrerError(chatID, flowData, "❌ Клиент с таким номером не найден или у него нет активной подписки")
	}

	// Сохраняем данные реферала
	flowData.ReferrerWhatsApp = &referrerWhatsApp
	flowData.ReferrerSubscriptionID = &referrerSub.ID

	// Переходим к выбору тарифа
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)
	return h.showTariffs(chatID)
}

// sendReferrerError отправляет ошибку с возможностью повторить или пропустить
func (h *Handler) sendReferrerError(chatID int64, flowData *flows.CreateSubForClientFlowData, errorMsg string) error {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Ввести другой номер", "ref_retry"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить", "ref_skip"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("◀️ Отменить", "cancel"),
		),
	)

	if flowData.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, errorMsg)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, errorMsg)
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err == nil && flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.AdminCreateSubWaitReferrer, flowData)
	}
	return err
}

// NormalizePhone очищает номер телефона, оставляя только цифры
func NormalizePhone(phone string) string {
	var result strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// IsValidPhoneNumber проверяет что нормализованный номер телефона валиден
func IsValidPhoneNumber(normalizedPhone string) bool {
	match, _ := regexp.MatchString(`^[0-9]{10,15}$`, normalizedPhone)
	return match
}

func (h *Handler) showTariffs(chatID int64) error {
	ctx := context.Background()

	// Получаем данные флоу
	flowData, _ := h.stateManager.GetCreateSubForClientData(chatID)

	// Удаляем предыдущее сообщение (реферальный вопрос)
	if flowData != nil && flowData.MessageID != nil {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, *flowData.MessageID)
		_, _ = h.bot.Request(deleteMsg)
		flowData.MessageID = nil
	}

	// Получаем платные тарифы
	tariffsList, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения тарифов: %w", err)
	}

	// Если клиент имеет право на trial - добавляем trial тариф в начало списка
	if flowData != nil && flowData.IsTrialEligible {
		trialTariff, err := h.tariffService.GetTrialTariff(ctx)
		if err != nil {
			h.logger.Error("Failed to get trial tariff", "error", err)
		} else if trialTariff != nil {
			h.logger.Info("Adding trial tariff to list", "whatsapp", flowData.ClientWhatsApp)
			// Добавляем trial в начало списка
			tariffsList = append([]*tariffs.Tariff{trialTariff}, tariffsList...)
		}
	}

	if len(tariffsList) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, "❌ К сожалению, активных тарифов сейчас нет")
		_, err = h.bot.Send(msg)
		return err
	}

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffsList)

	msg := tgbotapi.NewMessage(chatID, "📅 Выберите тариф:")
	msg.ReplyMarkup = keyboard

	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	if flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)
	}

	return nil
}

// handleTariffSelection обработка выбора тарифа
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		return h.sendError(chatID, "Пожалуйста, выберите тариф из меню")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	// Проверяем на отмену
	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Парсим данные тарифа
	tariffData, err := h.parseTariffFromCallback(update.CallbackQuery.Data)
	if err != nil {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	// Получаем существующие данные флоу
	flowData, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные о тарифе
	flowData.TariffID = tariffData.ID
	flowData.TariffName = tariffData.Name
	flowData.Price = tariffData.Price
	flowData.TotalAmount = tariffData.Price

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём заказ...")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Если тариф бесплатный - сразу создаем подписку без оплаты
	if tariffData.Price == 0 {
		return h.createFreeSubscription(ctx, chatID, flowData)
	}

	// Переводим в состояние ожидания оплаты
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitPayment, flowData)

	// Сразу создаём платёж и показываем ссылку на оплату
	return h.createPaymentAndShow(ctx, chatID, flowData)
}

// handlePaymentConfirmation обработка подтверждения оплаты
func (h *Handler) handlePaymentConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Получаем данные флоу
	data, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обрабатываем разные типы callback
	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "refresh_payment_link":
		return h.handleRefreshPaymentLink(ctx, update, data)
	case callbackData == "cancel_purchase" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
}

// handleRefreshPaymentLink обрабатывает обновление ссылки на оплату
func (h *Handler) handleRefreshPaymentLink(ctx context.Context, update *tgbotapi.Update, data *flows.CreateSubForClientFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём новую ссылку...")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Создаем новый платеж и показываем новую ссылку
	return h.createPaymentAndShow(ctx, chatID, data)
}

// createPaymentAndShow создает платеж и сразу показывает ссылку на оплату
func (h *Handler) createPaymentAndShow(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData) error {
	// Создаем платеж
	paymentEntity := payment.Payment{
		UserID: data.AdminUserID,
		Amount: data.TotalAmount,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		h.logger.Error("Failed to create payment",
			"error", err,
			"user_id", data.AdminUserID,
			"amount", data.TotalAmount)
		return h.sendError(chatID, "Ошибка создания платежа. Попробуйте позже или обратитесь к администратору.")
	}

	// Mock mode: платёж уже approved, сразу создаём подписку
	if paymentObj.PaymentURL == nil && paymentObj.Status == payment.StatusApproved {
		return h.createSubscriptionWithPayment(ctx, chatID, data, paymentObj.ID)
	}

	// Проверяем что ссылка на оплату была создана
	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "❌ Ошибка генерации ссылки на оплату")
	}

	// Определяем тип реферала
	var referralType *string
	if data.ReferrerWhatsApp != nil {
		if data.IsPartnership {
			rt := "partnership"
			referralType = &rt
		} else {
			rt := "referral"
			referralType = &rt
		}
	}

	// Создаем pending order для хранения контекста заказа
	pendingOrder := orders.PendingOrder{
		PaymentID:              paymentObj.ID,
		AdminUserID:            data.AdminUserID,
		AssistantTelegramID:    data.AssistantTelegramID,
		ChatID:                 chatID,
		ClientWhatsApp:         data.ClientWhatsApp,
		TariffID:               data.TariffID,
		TariffName:             data.TariffName,
		TotalAmount:            data.TotalAmount,
		ReferrerWhatsApp:       data.ReferrerWhatsApp,
		ReferrerSubscriptionID: data.ReferrerSubscriptionID,
		ReferralType:           referralType,
	}

	createdOrder, err := h.orderService.CreatePendingOrder(ctx, pendingOrder)
	if err != nil {
		h.logger.Error("Failed to create pending order", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания заказа")
	}

	// Показываем сообщение с ссылкой на оплату
	paymentMsg := fmt.Sprintf(
		"💳 Заказ создан!\n\n"+
			"📱 Клиент: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Ссылка на оплату: [link](%s)\n\n",
		data.ClientWhatsApp, data.TariffName, data.TotalAmount, *paymentObj.PaymentURL)

	// Создаем кнопки с orderID для независимой работы каждого заказа
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", fmt.Sprintf("pay_check:%d", createdOrder.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("🔗 Обновить ссылку", fmt.Sprintf("pay_refresh:%d", createdOrder.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", createdOrder.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(refreshButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем существующее сообщение, если MessageID есть
	var messageID int
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		if err != nil {
			return err
		}
		messageID = *data.MessageID
	} else {
		// Отправляем новое сообщение
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		sentMsg, err := h.bot.Send(msg)
		if err != nil {
			return err
		}
		messageID = sentMsg.MessageID
	}

	// Сохраняем MessageID в pending order для последующего редактирования
	if err := h.orderService.UpdateMessageID(ctx, createdOrder.ID, messageID); err != nil {
		h.logger.Error("Failed to update message ID", "error", err, "orderID", createdOrder.ID)
	}

	// ВАЖНО: очищаем состояние, чтобы админ мог начать новый флоу
	// Теперь кнопки работают независимо через orderID
	h.stateManager.Clear(chatID)

	return nil
}

// handleCancel обрабатывает отмену любого действия и возвращает в главное меню
func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем главное меню
	return h.sendMainMenu(chatID)
}

// sendMainMenu отправляет главное меню
func (h *Handler) sendMainMenu(chatID int64) error {
	text := "📱 Доступные команды:\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/my_subs — Список подписок"

	msg := tgbotapi.NewMessage(chatID, text)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) createTariffsKeyboard(tariffList []*tariffs.Tariff) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffList {
		durationText := formatDuration(t.DurationDays)
		text := fmt.Sprintf("📅 %s - %.2f ₽ (%s)", t.Name, t.Price, durationText)
		callbackData := fmt.Sprintf("tariff:%d:%.2f:%s:%d", t.ID, t.Price, t.Name, t.DurationDays)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Добавляем кнопку отмены
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration форматирует длительность в удобный формат
func formatDuration(days int) string {
	if days >= 365 {
		years := days / 365
		if years == 1 {
			return "1 год"
		}
		return fmt.Sprintf("%d лет", years)
	}
	if days >= 30 {
		months := days / 30
		if months == 1 {
			return "1 месяц"
		}
		return fmt.Sprintf("%d мес", months)
	}
	if days == 1 {
		return "1 день"
	}
	return fmt.Sprintf("%d дней", days)
}

// handlePaymentCompleted обрабатывает нажатие кнопки "Оплатил"
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.CreateSubForClientFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Проверяем платеж...")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Проверяем что paymentID есть
	if data.PaymentID == nil {
		return h.sendError(chatID, "❌ Ошибка: платеж не найден")
	}

	// Проверяем статус платежа через API
	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, *data.PaymentID)
	if err != nil {
		return h.sendPaymentCheckError(chatID, data, "❌ Ошибка проверки платежа. Попробуйте еще раз.")
	}

	// Проверяем статус
	switch paymentObj.Status {
	case payment.StatusApproved:
		// Платеж успешен - создаем подписку
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		// Платеж еще обрабатывается - показываем всплывающее уведомление
		alertConfig := tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "⏳ Платеж еще обрабатывается.\nПожалуйста, подождите и попробуйте еще раз.")
		_, _ = h.bot.Request(alertConfig)
		return nil
	case payment.StatusRejected, payment.StatusCancelled:
		// Платеж отклонен или отменен
		return h.sendError(chatID, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendPaymentCheckError(chatID, data, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// sendPaymentCheckError отправляет сообщение об ошибке проверки с возможностью повторить
func (h *Handler) sendPaymentCheckError(chatID int64, data *flows.CreateSubForClientFlowData, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(retryButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, errorMsg)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, errorMsg)
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err == nil {
		data.MessageID = &sentMsg.MessageID
	}
	return err
}

// handleSuccessfulPayment обрабатывает успешный платеж и создает подписку
func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData, paymentID int64) error {
	// Определяем тип реферала
	var referralType *string
	if data.ReferrerWhatsApp != nil {
		if data.IsPartnership {
			rt := "partnership"
			referralType = &rt
		} else {
			rt := "referral"
			referralType = &rt
		}
	}

	// Создаем подписку после успешной оплаты
	subReq := &subs.CreateSubscriptionRequest{
		UserID:                 data.AdminUserID,
		TariffID:               data.TariffID,
		PaymentID:              &paymentID,
		ClientWhatsApp:         data.ClientWhatsApp,
		CreatedByTelegramID:    data.AssistantTelegramID,
		ReferrerSubscriptionID: data.ReferrerSubscriptionID,
		ReferrerWhatsApp:       data.ReferrerWhatsApp,
		ReferralType:           referralType,
	}

	result, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription after payment", "error", err, "paymentID", paymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем информацию о созданной подписке
	return h.sendSubscriptionCreated(chatID, result, data)
}

// sendSubscriptionCreated отправляет сообщение об успешном создании подписки
func (h *Handler) sendSubscriptionCreated(chatID int64, result *subs.CreateSubscriptionResult, data *flows.CreateSubForClientFlowData) error {
	// Формируем пароль если есть
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	// Формируем информацию о реферальном бонусе
	referralLine := ""
	if result.ReferralBonusApplied && result.ReferrerWhatsApp != nil {
		referralExpiresLine := ""
		if result.ReferrerNewExpiresAt != nil {
			referralExpiresLine = fmt.Sprintf("\nПодписка до: %s",
				result.ReferrerNewExpiresAt.Format("02.01.2006"))
		}
		referralLine = fmt.Sprintf("\n\n🎁 *+10 дней бонуса* пригласившему `%s`%s",
			*result.ReferrerWhatsApp,
			referralExpiresLine)
	}

	messageText := fmt.Sprintf(
		"✅ *Подписка создана успешно!*\n\n"+
			"📱 Клиент: `%s`\n"+
			"📅 Тариф: %s\n\n"+
			"🔑 User ID:\n`%s`\n"+
			"🔐 Пароль:%s%s",
		data.ClientWhatsApp,
		data.TariffName,
		result.GeneratedUserID,
		passwordLine,
		referralLine,
	)

	// Создаем кнопки
	whatsappLink := generateWhatsAppLink(data.ClientWhatsApp, "Ваша подписка VPN активирована! Сейчас отправлю инструкции по подключению.")

	var rows [][]tgbotapi.InlineKeyboardButton

	// Добавляем кнопку для открытия панели управления сервером
	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🖥 Открыть панель управления", *result.ServerUIURL),
		))
	}

	// Добавляем кнопку для написания клиенту
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("💬 Написать клиенту", whatsappLink),
	))

	// Добавляем кнопку для написания пригласившему
	if result.ReferralBonusApplied && result.ReferrerWhatsApp != nil {
		referrerExpiresStr := ""
		if result.ReferrerNewExpiresAt != nil {
			referrerExpiresStr = result.ReferrerNewExpiresAt.Format("02.01.2006")
		}
		referrerMessage := fmt.Sprintf("🎉 Сизден жаңы кардар келди!\n\nБул жумада: %d чакыруу\nСиздин жазылууңузга +10күн кошулду\nэми %s чейин болду",
			result.ReferrerWeeklyCount,
			referrerExpiresStr)
		referrerWhatsappLink := generateWhatsAppLink(*result.ReferrerWhatsApp, referrerMessage)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("💬 Написать пригласившему", referrerWhatsappLink),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		if err != nil {
			// Fallback: отправляем новое сообщение
			msg := tgbotapi.NewMessage(chatID, messageText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = keyboard
			_, err = h.bot.Send(msg)
		}
		h.stateManager.Clear(chatID)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)

	// Очищаем состояние флоу
	h.stateManager.Clear(chatID)

	return err
}

// createFreeSubscription создает бесплатную подписку без оплаты
func (h *Handler) createFreeSubscription(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData) error {
	return h.createSubscriptionWithPayment(ctx, chatID, data, 0)
}

// createSubscriptionWithPayment создает подписку с привязкой к платежу
func (h *Handler) createSubscriptionWithPayment(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData, paymentID int64) error {
	var paymentIDPtr *int64
	if paymentID > 0 {
		paymentIDPtr = &paymentID
	}

	// Определяем тип реферала
	var referralType *string
	if data.ReferrerWhatsApp != nil {
		if data.IsPartnership {
			rt := "partnership"
			referralType = &rt
		} else {
			rt := "referral"
			referralType = &rt
		}
	}

	subReq := &subs.CreateSubscriptionRequest{
		UserID:                 data.AdminUserID,
		TariffID:               data.TariffID,
		PaymentID:              paymentIDPtr,
		ClientWhatsApp:         data.ClientWhatsApp,
		CreatedByTelegramID:    data.AssistantTelegramID,
		ReferrerSubscriptionID: data.ReferrerSubscriptionID,
		ReferrerWhatsApp:       data.ReferrerWhatsApp,
		ReferralType:           referralType,
	}

	result, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription", "error", err, "paymentID", paymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем информацию о созданной подписке
	return h.sendSubscriptionCreated(chatID, result, data)
}

// generateWhatsAppLink генерирует ссылку на WhatsApp с предзаполненным сообщением
func generateWhatsAppLink(phone string, message string) string {
	// Убираем + из начала номера для WhatsApp API
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")

	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}

// TariffCallbackData - структура для данных тарифа из callback
type TariffCallbackData struct {
	ID           int64
	Price        float64
	Name         string
	DurationDays int
}

// parseTariffFromCallback парсит данные тарифа из callback data
func (h *Handler) parseTariffFromCallback(callbackData string) (*TariffCallbackData, error) {
	if !strings.HasPrefix(callbackData, "tariff:") {
		return nil, fmt.Errorf("invalid callback format")
	}

	// Формат: tariff:id:price:name:days
	parts := strings.Split(callbackData, ":")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid tariff callback format")
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tariff ID: %w", err)
	}

	price, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tariff price: %w", err)
	}

	name := parts[3]

	days, err := strconv.Atoi(parts[4])
	if err != nil {
		return nil, fmt.Errorf("invalid tariff duration: %w", err)
	}

	return &TariffCallbackData{
		ID:           id,
		Price:        price,
		Name:         name,
		DurationDays: days,
	}, nil
}

func (h *Handler) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := h.bot.Send(msg)
	return err
}

func extractChatID(update *tgbotapi.Update) int64 {
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}

// HandlePaymentCallback обрабатывает callbacks от кнопок оплаты (pay_check, pay_refresh, pay_cancel)
// Эти callbacks работают независимо от состояния пользователя через orderID
func (h *Handler) HandlePaymentCallback(update *tgbotapi.Update) error {
	ctx := context.Background()

	if update.CallbackQuery == nil {
		return nil
	}

	callbackData := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Chat.ID

	// Парсим callback: pay_check:123 → action="check", orderID=123
	parts := strings.Split(callbackData, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, "❌ Неверный формат callback")
	}

	action := strings.TrimPrefix(parts[0], "pay_")
	orderID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "❌ Неверный ID заказа")
	}

	// Загружаем заказ из БД
	order, err := h.orderService.GetPendingOrderByID(ctx, orderID)
	if err != nil {
		h.logger.Error("Failed to get pending order", "error", err, "orderID", orderID)
		return h.sendError(chatID, "❌ Ошибка получения заказа")
	}
	if order == nil {
		return h.sendCallbackError(update, chatID, "❌ Заказ не найден или уже обработан")
	}

	switch action {
	case "check":
		return h.handlePaymentCheckFromOrder(ctx, update, order)
	case "refresh":
		return h.handlePaymentRefreshFromOrder(ctx, update, order)
	case "cancel":
		return h.handlePaymentCancelFromOrder(ctx, update, order)
	default:
		return h.sendError(chatID, "❌ Неизвестное действие")
	}
}

// handlePaymentCheckFromOrder проверяет статус платежа и создает подписку если оплачено
func (h *Handler) handlePaymentCheckFromOrder(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Проверяем платеж...")
	_, _ = h.bot.Request(callbackConfig)

	// Проверяем статус платежа
	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, order.PaymentID)
	if err != nil {
		h.logger.Error("Failed to check payment status", "error", err, "paymentID", order.PaymentID)
		return h.sendPaymentCheckErrorForOrder(chatID, order, "❌ Ошибка проверки платежа. Попробуйте еще раз.")
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		// Платеж успешен - создаем подписку
		return h.handleSuccessfulPaymentFromOrder(ctx, chatID, order)
	case payment.StatusPending:
		// Платеж еще обрабатывается - показываем всплывающее уведомление
		alertConfig := tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "⏳ Платеж еще обрабатывается.\nПожалуйста, подождите и попробуйте еще раз.")
		_, _ = h.bot.Request(alertConfig)
		return nil
	case payment.StatusRejected, payment.StatusCancelled:
		// Платеж отклонен или отменен
		return h.sendPaymentCheckErrorForOrder(chatID, order, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendPaymentCheckErrorForOrder(chatID, order, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// handleSuccessfulPaymentFromOrder создает подписку после успешной оплаты
func (h *Handler) handleSuccessfulPaymentFromOrder(ctx context.Context, chatID int64, order *orders.PendingOrder) error {
	// Создаем подписку
	subReq := &subs.CreateSubscriptionRequest{
		UserID:                 order.AdminUserID,
		TariffID:               order.TariffID,
		PaymentID:              &order.PaymentID,
		ClientWhatsApp:         order.ClientWhatsApp,
		CreatedByTelegramID:    order.AssistantTelegramID,
		ReferrerSubscriptionID: order.ReferrerSubscriptionID,
		ReferrerWhatsApp:       order.ReferrerWhatsApp,
		ReferralType:           order.ReferralType,
	}

	result, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription after payment", "error", err, "paymentID", order.PaymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем сообщение об успехе
	if err := h.sendSubscriptionCreatedForOrder(chatID, result, order); err != nil {
		return err
	}

	// Удаляем pending order - он больше не нужен
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err, "orderID", order.ID)
	}

	return nil
}

// handlePaymentRefreshFromOrder создает новый платеж и обновляет сообщение
func (h *Handler) handlePaymentRefreshFromOrder(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём новую ссылку...")
	_, _ = h.bot.Request(callbackConfig)

	// Создаем новый платеж
	paymentEntity := payment.Payment{
		UserID: order.AdminUserID,
		Amount: order.TotalAmount,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		h.logger.Error("Failed to create payment for refresh",
			"error", err,
			"user_id", order.AdminUserID,
			"amount", order.TotalAmount)
		return h.sendError(chatID, "Ошибка создания платежа. Попробуйте позже или обратитесь к администратору.")
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "Ошибка генерации ссылки на оплату")
	}

	// Обновляем paymentID в заказе
	if err := h.orderService.UpdatePaymentID(ctx, order.ID, paymentObj.ID); err != nil {
		h.logger.Error("Failed to update payment ID", "error", err, "orderID", order.ID)
	}

	// Формируем обновленное сообщение
	paymentMsg := fmt.Sprintf(
		"💳 *Заказ создан!*\n\n"+
			"📱 Клиент: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Ссылка на оплату: [link](%s)\n\n"+
			"Отправьте эту ссылку клиенту.\n"+
			"После оплаты нажмите «Проверить оплату».",
		order.ClientWhatsApp, order.TariffName, order.TotalAmount, *paymentObj.PaymentURL)

	// Создаем кнопки
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", fmt.Sprintf("pay_check:%d", order.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("🔗 Обновить ссылку", fmt.Sprintf("pay_refresh:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", order.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(refreshButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем сообщение
	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, paymentMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Обновляем messageID в заказе
	if err := h.orderService.UpdateMessageID(ctx, order.ID, sentMsg.MessageID); err != nil {
		h.logger.Error("Failed to update message ID", "error", err, "orderID", order.ID)
	}

	return nil
}

// handlePaymentCancelFromOrder отменяет заказ
func (h *Handler) handlePaymentCancelFromOrder(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, _ = h.bot.Request(callbackConfig)

	// Удаляем pending order
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err, "orderID", order.ID)
	}

	// Редактируем сообщение чтобы показать что заказ отменен
	if order.MessageID != nil {
		cancelledMsg := fmt.Sprintf(
			"❌ *Заказ отменен*\n\n"+
				"📱 Клиент: %s\n"+
				"📅 Тариф: %s",
			order.ClientWhatsApp, order.TariffName)

		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, cancelledMsg)
		editMsg.ParseMode = "Markdown"
		_, _ = h.bot.Send(editMsg)
	}

	return nil
}

// sendPaymentCheckErrorForOrder отправляет сообщение об ошибке проверки
func (h *Handler) sendPaymentCheckErrorForOrder(chatID int64, order *orders.PendingOrder, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", fmt.Sprintf("pay_check:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", order.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(retryButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, errorMsg)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, errorMsg)
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendSubscriptionCreatedForOrder отправляет сообщение об успешном создании подписки
func (h *Handler) sendSubscriptionCreatedForOrder(chatID int64, result *subs.CreateSubscriptionResult, order *orders.PendingOrder) error {
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	// Формируем информацию о реферальном бонусе
	referralLine := ""
	if result.ReferralBonusApplied && result.ReferrerWhatsApp != nil {
		referralExpiresLine := ""
		if result.ReferrerNewExpiresAt != nil {
			referralExpiresLine = fmt.Sprintf("\nПодписка до: %s",
				result.ReferrerNewExpiresAt.Format("02.01.2006"))
		}
		referralLine = fmt.Sprintf("\n\n🎁 *+10 дней бонуса* пригласившему `%s`%s",
			*result.ReferrerWhatsApp,
			referralExpiresLine)
	}

	messageText := fmt.Sprintf(
		"✅ *Подписка создана успешно!*\n\n"+
			"📱 Клиент: `%s`\n"+
			"📅 Тариф: %s\n\n"+
			"🔑 User ID:\n`%s`\n"+
			"🔐 Пароль:%s%s",
		order.ClientWhatsApp,
		order.TariffName,
		result.GeneratedUserID,
		passwordLine,
		referralLine,
	)

	whatsappLink := generateWhatsAppLink(order.ClientWhatsApp, "Ваша подписка VPN активирована! Сейчас отправлю инструкции по подключению.")

	var rows [][]tgbotapi.InlineKeyboardButton

	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🖥 Открыть панель управления", *result.ServerUIURL),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("💬 Написать клиенту", whatsappLink),
	))

	// Добавляем кнопку для написания пригласившему
	if result.ReferralBonusApplied && result.ReferrerWhatsApp != nil {
		referrerExpiresStr := ""
		if result.ReferrerNewExpiresAt != nil {
			referrerExpiresStr = result.ReferrerNewExpiresAt.Format("02.01.2006")
		}
		referrerMessage := fmt.Sprintf("🎉 Сизден жаңы кардар келди!\n\nБул жумада: %d чакыруу\nСиздин жазылууңузга +10күн кошулду\nэми %s чейин болду",
			result.ReferrerWeeklyCount,
			referrerExpiresStr)
		referrerWhatsappLink := generateWhatsAppLink(*result.ReferrerWhatsApp, referrerMessage)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("💬 Написать пригласившему", referrerWhatsappLink),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		if err != nil {
			// Fallback
			msg := tgbotapi.NewMessage(chatID, messageText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = keyboard
			_, err = h.bot.Send(msg)
		}
		return err
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendCallbackError отвечает на callback с ошибкой и отправляет сообщение
func (h *Handler) sendCallbackError(update *tgbotapi.Update, chatID int64, errorMsg string) error {
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, errorMsg)
	_, _ = h.bot.Request(callbackConfig)
	return nil
}
