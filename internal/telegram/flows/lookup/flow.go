package lookup

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot                botApi
	stateManager       stateManager
	lookupStorage      lookupStorage
	clientTokenStorage clientTokenStorage
	serverService      serverService
	subStorage         subscriptionStorage
	webDomain          string
	logger             *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ls lookupStorage,
	cts clientTokenStorage,
	ss serverService,
	subSt subscriptionStorage,
	webDomain string,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                bot,
		stateManager:       sm,
		lookupStorage:      ls,
		clientTokenStorage: cts,
		serverService:      ss,
		subStorage:         subSt,
		webDomain:          webDomain,
		logger:             logger,
	}
}

// Start performs the initial search and shows the first subscription
func (h *Handler) Start(ctx context.Context, chatID int64, phoneSuffix string) error {
	digits := extractDigits(phoneSuffix)
	if len(digits) < 3 || len(digits) > 6 {
		msg := tgbotapi.NewMessage(chatID, "Введите от 3 до 6 последних цифр номера.\nПример: /find 2706")
		_, _ = h.bot.Send(msg)
		return nil
	}

	results, err := h.lookupStorage.SearchSubscriptionsByPhoneSuffix(ctx, digits)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка поиска")
		_, _ = h.bot.Send(msg)
		return fmt.Errorf("search subscriptions by phone suffix: %w", err)
	}

	if len(results) == 0 {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Подписки с номером *...%s* не найдены", digits))
		msg.ParseMode = "Markdown"
		_, _ = h.bot.Send(msg)
		return nil
	}

	// Convert storage results to flow results
	flowResults := make([]flows.LookupSubResult, len(results))
	for i, r := range results {
		flowResults[i] = flows.LookupSubResult{
			ID:             r.ID,
			ClientWhatsApp: r.ClientWhatsApp,
			TariffName:     r.TariffName,
			Status:         r.Status,
			ExpiresAt:      r.ExpiresAt,
			LastRenewedAt:  r.LastRenewedAt,
			CreatedAt:      r.CreatedAt,
			ServerID:       r.ServerID,
			ServerName:     r.ServerName,
		}
	}

	// Build client links for unique WhatsApp numbers
	clientLinks := make(map[string]string)
	for _, r := range results {
		if r.ClientWhatsApp == nil || *r.ClientWhatsApp == "" {
			continue
		}
		wa := *r.ClientWhatsApp
		if _, ok := clientLinks[wa]; ok {
			continue
		}
		token, err := h.clientTokenStorage.GetOrCreateClientToken(ctx, wa, chatID, nil)
		if err != nil {
			h.logger.Error("Failed to get client token", "error", err, "whatsapp", wa)
			continue
		}
		clientLinks[wa] = fmt.Sprintf("%s/c/%s", h.webDomain, token.Token)
	}

	flowData := &flows.LookupFlowData{
		Results:     flowResults,
		CurrentIdx:  0,
		Suffix:      digits,
		ClientLinks: clientLinks,
	}

	text, keyboard := h.formatSubscriptionView(flowData)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	msg.DisableWebPagePreview = true

	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	flowData.MessageID = &sentMsg.MessageID
	h.stateManager.SetState(chatID, states.LookupViewSub, flowData)

	return nil
}

// HandleCallback handles all lkp_* callbacks
func (h *Handler) HandleCallback(ctx context.Context, callback *tgbotapi.CallbackQuery) error {
	chatID := callback.Message.Chat.ID
	callbackData := callback.Data

	// Acknowledge callback
	cb := tgbotapi.NewCallback(callback.ID, "")
	_, _ = h.bot.Request(cb)

	flowData, err := h.stateManager.GetLookupData(chatID)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "Данные устарели. Выполните /find заново.")
		_, _ = h.bot.Send(msg)
		return nil
	}

	switch {
	case callbackData == "lkp_next":
		return h.handleNext(chatID, flowData)
	case callbackData == "lkp_prev":
		return h.handlePrev(chatID, flowData)
	case callbackData == "lkp_transfer":
		return h.handleTransfer(ctx, chatID, flowData)
	case strings.HasPrefix(callbackData, "lkp_srv:"):
		return h.handleServerSelection(ctx, chatID, callbackData, flowData)
	case callbackData == "lkp_back":
		return h.handleBack(chatID, flowData)
	case callbackData == "lkp_close":
		return h.handleClose(chatID, flowData)
	}

	return nil
}

func (h *Handler) handleNext(chatID int64, flowData *flows.LookupFlowData) error {
	if flowData.CurrentIdx < len(flowData.Results)-1 {
		flowData.CurrentIdx++
	}
	h.stateManager.SetState(chatID, states.LookupViewSub, flowData)
	return h.editSubscriptionView(chatID, flowData)
}

func (h *Handler) handlePrev(chatID int64, flowData *flows.LookupFlowData) error {
	if flowData.CurrentIdx > 0 {
		flowData.CurrentIdx--
	}
	h.stateManager.SetState(chatID, states.LookupViewSub, flowData)
	return h.editSubscriptionView(chatID, flowData)
}

func (h *Handler) handleTransfer(ctx context.Context, chatID int64, flowData *flows.LookupFlowData) error {
	current := flowData.Results[flowData.CurrentIdx]
	if current.Status != "active" {
		return h.editSubscriptionView(chatID, flowData)
	}

	h.stateManager.SetState(chatID, states.LookupSelectServer, flowData)
	return h.editServerSelectionView(ctx, chatID, flowData)
}

func (h *Handler) handleServerSelection(ctx context.Context, chatID int64, callbackData string, flowData *flows.LookupFlowData) error {
	parts := strings.SplitN(callbackData, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	serverID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil
	}

	current := flowData.Results[flowData.CurrentIdx]

	// Update subscription's server in the database
	if err := h.subStorage.UpdateSubscriptionServer(ctx, current.ID, serverID); err != nil {
		h.logger.Error("Failed to update subscription server", "error", err, "sub_id", current.ID, "server_id", serverID)
		return h.editErrorMessage(chatID, flowData, "❌ Ошибка переноса подписки")
	}

	// Find the server name for display
	archivedFalse := false
	serversList, _ := h.serverService.ListServers(ctx, servers.ListCriteria{Archived: &archivedFalse})
	var serverName string
	for _, s := range serversList {
		if s.ID == serverID {
			serverName = s.Name
			break
		}
	}

	// Update flow data with new server info
	flowData.Results[flowData.CurrentIdx].ServerID = &serverID
	flowData.Results[flowData.CurrentIdx].ServerName = &serverName

	// Go back to subscription view
	h.stateManager.SetState(chatID, states.LookupViewSub, flowData)

	// Show success and then the updated subscription
	text, keyboard := h.formatSubscriptionView(flowData)
	successText := fmt.Sprintf("✅ Подписка #%d перенесена на сервер %s\n\n%s", current.ID, serverName, text)

	return h.editMessage(chatID, flowData, successText, keyboard)
}

func (h *Handler) handleBack(chatID int64, flowData *flows.LookupFlowData) error {
	h.stateManager.SetState(chatID, states.LookupViewSub, flowData)
	return h.editSubscriptionView(chatID, flowData)
}

func (h *Handler) handleClose(chatID int64, flowData *flows.LookupFlowData) error {
	h.stateManager.Clear(chatID)

	if flowData.MessageID != nil {
		text := fmt.Sprintf("🔍 Поиск по номеру ...%s завершён", flowData.Suffix)
		editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, text)
		_, _ = h.bot.Send(editMsg)
	}

	return nil
}

// formatSubscriptionView formats a single subscription with navigation buttons
func (h *Handler) formatSubscriptionView(flowData *flows.LookupFlowData) (string, tgbotapi.InlineKeyboardMarkup) {
	current := flowData.Results[flowData.CurrentIdx]
	total := len(flowData.Results)
	idx := flowData.CurrentIdx

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔍 *Подписки по номеру ...%s* (%d/%d)\n\n", flowData.Suffix, idx+1, total))

	b.WriteString(fmt.Sprintf("━━━ *#%d* ━━━\n", current.ID))

	if current.ClientWhatsApp != nil {
		b.WriteString(fmt.Sprintf("📱 %s\n", *current.ClientWhatsApp))
	}

	b.WriteString(fmt.Sprintf("📋 Тариф: %s\n", current.TariffName))
	b.WriteString(fmt.Sprintf("📌 Статус: %s\n", formatStatus(current.Status)))

	if current.ServerName != nil {
		b.WriteString(fmt.Sprintf("🖥 Сервер: %s\n", *current.ServerName))
	}

	if current.ExpiresAt != nil {
		b.WriteString(fmt.Sprintf("⏳ Истекает: %s\n", formatDate(*current.ExpiresAt)))
	}

	if current.LastRenewedAt != nil {
		b.WriteString(fmt.Sprintf("🔄 Продлён: %s\n", formatDate(*current.LastRenewedAt)))
	}

	b.WriteString(fmt.Sprintf("📅 Создан: %s\n", formatDate(current.CreatedAt)))

	if current.ClientWhatsApp != nil {
		if link, ok := flowData.ClientLinks[*current.ClientWhatsApp]; ok {
			b.WriteString(fmt.Sprintf("🔗 [Личный кабинет](%s)\n", link))
		}
	}

	// Build keyboard
	var rows [][]tgbotapi.InlineKeyboardButton

	// Navigation row
	if total > 1 {
		var navButtons []tgbotapi.InlineKeyboardButton
		if idx > 0 {
			navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("◀️ Пред.", "lkp_prev"))
		}
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d/%d", idx+1, total), "lkp_noop"))
		if idx < total-1 {
			navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("След. ▶️", "lkp_next"))
		}
		rows = append(rows, navButtons)
	}

	// Transfer button for active subscriptions
	if current.Status == "active" {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🔄 Перенести на другой сервер", "lkp_transfer"),
		})
	}

	// Close button
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("✖️ Закрыть", "lkp_close"),
	})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return b.String(), keyboard
}

// editSubscriptionView edits the message with the current subscription view
func (h *Handler) editSubscriptionView(chatID int64, flowData *flows.LookupFlowData) error {
	text, keyboard := h.formatSubscriptionView(flowData)
	return h.editMessage(chatID, flowData, text, keyboard)
}

// editServerSelectionView edits the message with server selection
func (h *Handler) editServerSelectionView(ctx context.Context, chatID int64, flowData *flows.LookupFlowData) error {
	current := flowData.Results[flowData.CurrentIdx]

	archivedFalse := false
	serversList, err := h.serverService.ListServers(ctx, servers.ListCriteria{
		Archived: &archivedFalse,
	})
	if err != nil {
		h.logger.Error("Failed to list servers", "error", err)
		return h.editErrorMessage(chatID, flowData, "❌ Ошибка загрузки серверов")
	}

	if len(serversList) == 0 {
		return h.editErrorMessage(chatID, flowData, "❌ Нет доступных серверов")
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔄 *Перенос подписки #%d*\n\n", current.ID))
	if current.ClientWhatsApp != nil {
		b.WriteString(fmt.Sprintf("📱 Клиент: %s\n", *current.ClientWhatsApp))
	}
	if current.ServerName != nil {
		b.WriteString(fmt.Sprintf("🖥 Текущий сервер: %s\n", *current.ServerName))
	}
	b.WriteString("\nВыберите новый сервер:")

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range serversList {
		text := fmt.Sprintf("🖥 %s", s.Name)
		// Skip current server
		if current.ServerID != nil && s.ID == *current.ServerID {
			text += " (текущий)"
		}
		callbackData := fmt.Sprintf("lkp_srv:%d", s.ID)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(text, callbackData),
		})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", "lkp_back"),
	})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return h.editMessage(chatID, flowData, b.String(), keyboard)
}

// editMessage edits the stored message with new text and keyboard
func (h *Handler) editMessage(chatID int64, flowData *flows.LookupFlowData, text string, keyboard tgbotapi.InlineKeyboardMarkup) error {
	if flowData.MessageID == nil {
		// Fallback: send new message
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.DisableWebPagePreview = true
		msg.ReplyMarkup = keyboard
		sentMsg, err := h.bot.Send(msg)
		if err != nil {
			return err
		}
		flowData.MessageID = &sentMsg.MessageID
		return nil
	}

	editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.DisableWebPagePreview = true
	editMsg.ReplyMarkup = &keyboard
	_, err := h.bot.Send(editMsg)
	if err != nil {
		// Fallback: send new message if edit fails
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.DisableWebPagePreview = true
		msg.ReplyMarkup = keyboard
		sentMsg, sendErr := h.bot.Send(msg)
		if sendErr != nil {
			return sendErr
		}
		flowData.MessageID = &sentMsg.MessageID
	}
	return nil
}

// editErrorMessage edits the message with an error and a back button
func (h *Handler) editErrorMessage(chatID int64, flowData *flows.LookupFlowData, errorText string) error {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", "lkp_back"),
		},
	)
	return h.editMessage(chatID, flowData, errorText, keyboard)
}

func formatStatus(status string) string {
	switch status {
	case "active":
		return "✅ Активна"
	case "expired":
		return "⚠️ Просрочена"
	case "disabled":
		return "🚫 Отключена"
	case "pending":
		return "⏳ Ожидание"
	default:
		return status
	}
}

func formatDate(t time.Time) string {
	return t.Format("02.01.2006")
}

func extractDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
