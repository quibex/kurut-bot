// Package assistants implements the admin-panel flow for managing the
// assistant roster (list / add / remove). Assistants work only with clients —
// no money or stats — so this is the single place to grant/revoke that role at
// runtime instead of editing the TELEGRAM_ASSISTANT_IDS env var.
package assistants

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/assistants"
	"kurut-bot/internal/telegram/states"
)

type botAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type stateManager interface {
	SetState(chatID int64, state states.State, data any)
	Clear(chatID int64)
}

type assistantStore interface {
	CreateAssistant(ctx context.Context, a assistants.Assistant) (*assistants.Assistant, error)
	GetAssistantByTelegramID(ctx context.Context, telegramID int64) (*assistants.Assistant, error)
	ListAssistants(ctx context.Context) ([]*assistants.Assistant, error)
	DeleteAssistant(ctx context.Context, criteria assistants.DeleteCriteria) error
}

// roster mutates the in-memory role cache so access takes effect immediately.
type roster interface {
	IsAdmin(telegramID int64) bool
	AddAssistant(telegramID int64)
	RemoveAssistant(telegramID int64)
}

type Handler struct {
	bot          botAPI
	stateManager stateManager
	store        assistantStore
	roster       roster
	logger       *slog.Logger
}

func NewHandler(bot botAPI, sm stateManager, store assistantStore, r roster, logger *slog.Logger) *Handler {
	return &Handler{bot: bot, stateManager: sm, store: store, roster: r, logger: logger}
}

// ShowMenu sends a fresh assistants menu (used by the /assistants command).
func (h *Handler) ShowMenu(ctx context.Context, chatID int64) error {
	text, keyboard, err := h.renderMenu(ctx)
	if err != nil {
		return h.sendError(chatID, "Не удалось загрузить список ассистентов")
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	_, err = h.bot.Send(msg)
	return err
}

// HandleCallback dispatches the ast_* callbacks (admin-gated by the router).
func (h *Handler) HandleCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) error {
	data := cq.Data
	chatID := cq.Message.Chat.ID

	switch {
	case data == "ast_menu":
		h.ack(cq.ID, "")
		return h.editMenu(ctx, chatID, cq.Message.MessageID)
	case data == "ast_add":
		h.ack(cq.ID, "")
		return h.Start(chatID)
	case strings.HasPrefix(data, "ast_del:"):
		idStr := strings.TrimPrefix(data, "ast_del:")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			h.ack(cq.ID, "Некорректный id")
			return nil
		}
		if err := h.store.DeleteAssistant(ctx, assistants.DeleteCriteria{TelegramID: &id}); err != nil {
			h.logger.Error("delete assistant", "err", err)
			h.ack(cq.ID, "Ошибка удаления")
			return nil
		}
		h.roster.RemoveAssistant(id)
		h.ack(cq.ID, "Удалён")
		return h.editMenu(ctx, chatID, cq.Message.MessageID)
	}
	h.ack(cq.ID, "")
	return nil
}

// Start begins the add-assistant flow.
func (h *Handler) Start(chatID int64) error {
	h.stateManager.SetState(chatID, states.AdminAddAssistantWaitTarget, nil)
	text := "Кого добавить в ассистенты?\n\n" +
		"• <b>Перешли любое сообщение</b> от человека, или\n" +
		"• пришли его <b>числовой Telegram ID</b>.\n\n" +
		"Отмена — /start"
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "cancel"),
		),
	)
	_, err := h.bot.Send(msg)
	return err
}

// Handle consumes the forwarded message / numeric id and adds the assistant.
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()
	if state != states.AdminAddAssistantWaitTarget || update.Message == nil {
		return nil
	}
	chatID := update.Message.Chat.ID

	var telegramID int64
	var label string

	if fwd := update.Message.ForwardFrom; fwd != nil {
		telegramID = fwd.ID
		label = strings.TrimSpace(fwd.FirstName + " " + fwd.LastName)
		if fwd.UserName != "" {
			label = strings.TrimSpace(label + " @" + fwd.UserName)
		}
	} else {
		raw := strings.TrimSpace(update.Message.Text)
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return h.sendError(chatID, "Не понял. Перешли сообщение от человека или пришли его числовой Telegram ID.")
		}
		telegramID = id
	}

	h.stateManager.Clear(chatID)

	if h.roster.IsAdmin(telegramID) {
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, "Это админ — у него и так полный доступ."))
		return h.ShowMenu(ctx, chatID)
	}

	existing, err := h.store.GetAssistantByTelegramID(ctx, telegramID)
	if err != nil {
		h.logger.Error("get assistant", "err", err)
		return h.sendError(chatID, "Ошибка проверки. Попробуй ещё раз.")
	}

	if existing == nil {
		var addedBy int64
		if update.Message.From != nil {
			addedBy = update.Message.From.ID
		}
		_, err = h.store.CreateAssistant(ctx, assistants.Assistant{
			TelegramID: telegramID,
			Label:      label,
			AddedBy:    addedBy,
		})
		if err != nil {
			h.logger.Error("create assistant", "err", err)
			return h.sendError(chatID, "Не удалось добавить. Попробуй ещё раз.")
		}
	}
	h.roster.AddAssistant(telegramID)

	who := label
	if who == "" {
		who = strconv.FormatInt(telegramID, 10)
	}
	if existing != nil {
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("ℹ️ %s уже был ассистентом.", who)))
	} else {
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %s теперь ассистент (ID %d). Доступны клиентские команды.", who, telegramID)))
	}
	return h.ShowMenu(ctx, chatID)
}

func (h *Handler) renderMenu(ctx context.Context) (string, tgbotapi.InlineKeyboardMarkup, error) {
	list, err := h.store.ListAssistants(ctx)
	if err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}

	var b strings.Builder
	b.WriteString("🧑‍💼 <b>Ассистенты</b>\n\n")
	if len(list) == 0 {
		b.WriteString("Пока никого нет. Добавь первого кнопкой ниже.")
	} else {
		for _, a := range list {
			label := a.Label
			if label == "" {
				label = "—"
			}
			b.WriteString(fmt.Sprintf("• %s (<code>%d</code>)\n", label, a.TelegramID))
		}
	}
	b.WriteString("\n\nАссистенты работают только с клиентами — без денег и статистики.")

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(list)+2)
	for _, a := range list {
		label := a.Label
		if label == "" {
			label = strconv.FormatInt(a.TelegramID, 10)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➖ "+label, fmt.Sprintf("ast_del:%d", a.TelegramID)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить", "ast_add"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏠 Главное меню", "main_menu"),
	))

	return b.String(), tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

func (h *Handler) editMenu(ctx context.Context, chatID int64, messageID int) error {
	text, keyboard, err := h.renderMenu(ctx)
	if err != nil {
		return h.sendError(chatID, "Не удалось загрузить список ассистентов")
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "HTML"
	edit.ReplyMarkup = &keyboard
	_, err = h.bot.Send(edit)
	return err
}

func (h *Handler) ack(callbackID, text string) {
	_, _ = h.bot.Request(tgbotapi.NewCallback(callbackID, text))
}

func (h *Handler) sendError(chatID int64, text string) error {
	_, err := h.bot.Send(tgbotapi.NewMessage(chatID, "❌ "+text))
	return err
}
