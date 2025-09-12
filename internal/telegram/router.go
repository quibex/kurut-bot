package telegram

import (
	"context"
	"strings"

	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram/flows/buysub"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Router struct {
	bot          *tgbotapi.BotAPI
	stateManager stateManager
	userService  userService
	adminChecker adminChecker

	// Handler для флоу покупки подписки
	buySubHandler       *buysub.Handler
	createTariffHandler *createtariff.Handler
}

type stateManager interface {
	GetState(tgUserID int64) states.State
	Clear(tgUserID int64)
}

type userService interface {
	GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (*users.User, error)
}

type adminChecker interface {
	IsAdmin(telegramID int64) bool
}

func (r *Router) Route(update *tgbotapi.Update) error {
	ctx := context.Background()

	// Получаем telegram_id
	telegramID := extractUserID(update)
	if telegramID == 0 {
		return nil // Некорректный update
	}

	// Получаем или создаем пользователя для получения внутреннего ID
	user, err := r.userService.GetOrCreateUserByTelegramID(
		ctx,
		telegramID,
	)
	if err != nil {
		_ = r.sendError(telegramID)
		return err
	}

	// ПРИОРИТЕТ: Проверяем команды первыми (отменяют любой флоу)
	if update.Message != nil && update.Message.IsCommand() {
		// Очищаем состояние при любой команде
		r.stateManager.Clear(telegramID)
		return r.handleCommandWithUser(update, user)
	}

	// Используем внутренний ID для состояния
	state := r.stateManager.GetState(telegramID)

	// Проверяем глобальную отмену
	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return r.handleGlobalCancelWithInternalID(update)
	}

	// Проверяем состояние флоу покупки подписки
	if strings.HasPrefix(string(state), "ubs_") {
		return r.buySubHandler.Handle(update, state)
	}

	// Проверяем состояние флоу создания тарифа
	if strings.HasPrefix(string(state), "act_") {
		return r.createTariffHandler.Handle(update, state)
	}

	// Если нет активного состояния - обрабатываем как обычное сообщение
	return r.sendHelp(extractChatID(update))
}

func (r *Router) handleCommandWithUser(update *tgbotapi.Update, user *users.User) error {
	if update.Message == nil || !update.Message.IsCommand() {
		return r.sendHelp(extractChatID(update))
	}

	switch update.Message.Command() {
	case "start":
		return r.sendWelcome(update.Message.Chat.ID)
	case "buy":
		return r.buySubHandler.Start(
			user.ID,
			update.Message.Chat.ID,
		)
	case "create_tariff":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для создания тарифов"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		return r.createTariffHandler.Start(
			update.Message.Chat.ID,
		)
	default:
		return r.sendHelp(update.Message.Chat.ID)
	}
}

func (r *Router) sendWelcome(chatID int64) error {
	text := "🎉 Добро пожаловать в Kurut VPN!\n\n" +
		"🌍 Быстрый и надежный VPN\n" +
		"🔒 Полная анонимность\n" +
		"📱 Поддержка всех устройств\n\n" +
		"Используйте команду /buy для покупки подписки"
	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды для администратора:\n" +
			"/create_tariff — Создать тариф"
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := r.bot.Send(msg)
	return err
}

func (r *Router) sendHelp(chatID int64) error {
	if chatID == 0 {
		return nil // Не можем отправить сообщение
	}
	text := "Доступные команды:\n\n" +
		"/start — Начать работу\n" +
		"/buy — Купить подписку VPN"
	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды для администратора:\n" +
			"/create_tariff — Создать тариф"
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := r.bot.Send(msg)
	return err
}

func (r *Router) sendError(chatID int64) error {
	msg := tgbotapi.NewMessage(chatID, "❌ Ошибка. Пожалуйста, попробуйте позже.")
	_, err := r.bot.Send(msg)
	return err
}

func extractUserID(update *tgbotapi.Update) int64 {
	if update.Message != nil {
		return update.Message.From.ID
	}
	if update.CallbackQuery != nil {
		return update.CallbackQuery.From.ID
	}
	return 0
}

func extractChatID(update *tgbotapi.Update) int64 {
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	if update.CallbackQuery != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}

// handleGlobalCancelWithInternalID обрабатывает глобальную отмену из любого состояния
func (r *Router) handleGlobalCancelWithInternalID(update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Очищаем любое состояние (используем внутренний ID)
	r.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Возвращаемся в главное меню")
	_, err := r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем главное меню
	return r.sendWelcome(chatID)
}

// NewRouter создает новый роутер с зависимостями
func NewRouter(bot *tgbotapi.BotAPI, stateManager stateManager, userService userService, adminChecker adminChecker, buySubHandler *buysub.Handler, createTariffHandler *createtariff.Handler) *Router {
	return &Router{
		bot:                 bot,
		stateManager:        stateManager,
		userService:         userService,
		buySubHandler:       buySubHandler,
		adminChecker:        adminChecker,
		createTariffHandler: createTariffHandler,
	}
}

// SetupBotCommands устанавливает команды для меню бота
func (r *Router) SetupBotCommands() error {
	commands := []tgbotapi.BotCommand{
		{
			Command:     "start",
			Description: "Начать работу с ботом",
		},
		{
			Command:     "buy",
			Description: "Купить подписку VPN",
		},
		{
			Command:     "create_tariff",
			Description: "Создать тариф (только для админов)",
		},
	}

	setCommandsConfig := tgbotapi.NewSetMyCommands(commands...)
	_, err := r.bot.Request(setCommandsConfig)
	return err
}
