package telegram

import (
	"context"
	"strings"

	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows/buysub"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/flows/disabletariff"
	"kurut-bot/internal/telegram/flows/enabletariff"
	"kurut-bot/internal/telegram/flows/starttrial"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Router struct {
	bot          *tgbotapi.BotAPI
	stateManager stateManager
	userService  userService
	adminChecker adminChecker

	// Handler для флоу покупки подписки
	buySubHandler        *buysub.Handler
	createTariffHandler  *createtariff.Handler
	disableTariffHandler *disabletariff.Handler
	enableTariffHandler  *enabletariff.Handler
	startTrialHandler    *starttrial.Handler
	mySubsCommand        *cmds.MySubsCommand
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

	// Устанавливаем команды для админов при первом взаимодействии
	if r.adminChecker.IsAdmin(telegramID) {
		r.setupAdminCommands(telegramID)
	}

	// ПРИОРИТЕТ: Проверяем команды первыми (отменяют любой флоу)
	if update.Message != nil && update.Message.IsCommand() {
		// Очищаем состояние при любой команде
		r.stateManager.Clear(telegramID)
		return r.handleCommandWithUser(update, user)
	}

	// Используем внутренний ID для состояния
	state := r.stateManager.GetState(telegramID)

	// Проверяем callback кнопки из главного меню
	if update.CallbackQuery != nil {
		switch update.CallbackQuery.Data {
		case "cancel", "main_menu":
			return r.handleGlobalCancelWithInternalID(update)
		case "start_trial":
			return r.handleStartTrial(update, user)
		case "view_tariffs":
			return r.buySubHandler.Start(user.ID, extractChatID(update))
		}
	}

	// Проверяем состояние флоу покупки подписки
	if strings.HasPrefix(string(state), "ubs_") {
		return r.buySubHandler.Handle(update, state)
	}

	// Проверяем состояние флоу создания тарифа
	if strings.HasPrefix(string(state), "act_") {
		return r.createTariffHandler.Handle(update, state)
	}

	// Проверяем состояние флоу архивации тарифа
	if strings.HasPrefix(string(state), "adt_") {
		return r.disableTariffHandler.Handle(update, state)
	}

	// Проверяем состояние флоу восстановления тарифа
	if strings.HasPrefix(string(state), "aet_") {
		return r.enableTariffHandler.Handle(update, state)
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
	case "disable_tariff":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для архивации тарифов"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		return r.disableTariffHandler.Start(
			update.Message.Chat.ID,
		)
	case "enable_tariff":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для восстановления тарифов"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		return r.enableTariffHandler.Start(
			update.Message.Chat.ID,
		)
	case "my_subs":
		ctx := context.Background()
		return r.mySubsCommand.Execute(ctx, user, update.Message.Chat.ID)
	default:
		return r.sendHelp(update.Message.Chat.ID)
	}
}

func (r *Router) sendWelcome(chatID int64) error {
	text := "🎉 Добро пожаловать в Kurut!\n\n" +
		"🌍 Быстрый и надежный доступ\n" +
		"🔒 Полная анонимность\n" +
		"📱 Поддержка всех устройств\n\n" +
		"Выберите действие:"

	// Создаем кнопки
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎁 Начать пробный период", "start_trial"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Посмотреть тарифы", "view_tariffs"),
		),
	)

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды для администратора:\n" +
			"/create_tariff — Создать тариф\n" +
			"/disable_tariff — Архивировать тариф\n" +
			"/enable_tariff — Восстановить тариф из архива"
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	_, err := r.bot.Send(msg)
	return err
}

func (r *Router) handleStartTrial(update *tgbotapi.Update, user *users.User) error {
	chatID := update.CallbackQuery.Message.Chat.ID
	ctx := context.Background()

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Активируем пробный период...")
	_, err := r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	return r.startTrialHandler.Start(ctx, user, chatID)
}

func (r *Router) sendHelp(chatID int64) error {
	if chatID == 0 {
		return nil // Не можем отправить сообщение
	}
	text := "Доступные команды:\n\n" +
		"/buy — Купить ключ доступа\n" +
		"/my_subs — Мои активные подписки"
	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды для администратора:\n" +
			"/create_tariff — Создать тариф\n" +
			"/disable_tariff — Архивировать тариф\n" +
			"/enable_tariff — Восстановить тариф из архива"
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
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, err := r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем список доступных команд
	return r.sendHelp(chatID)
}

// NewRouter создает новый роутер с зависимостями
func NewRouter(bot *tgbotapi.BotAPI, stateManager stateManager, userService userService, adminChecker adminChecker, buySubHandler *buysub.Handler, createTariffHandler *createtariff.Handler, disableTariffHandler *disabletariff.Handler, enableTariffHandler *enabletariff.Handler, startTrialHandler *starttrial.Handler, mySubsCommand *cmds.MySubsCommand) *Router {
	return &Router{
		bot:                  bot,
		stateManager:         stateManager,
		userService:          userService,
		buySubHandler:        buySubHandler,
		adminChecker:         adminChecker,
		createTariffHandler:  createTariffHandler,
		disableTariffHandler: disableTariffHandler,
		enableTariffHandler:  enableTariffHandler,
		startTrialHandler:    startTrialHandler,
		mySubsCommand:        mySubsCommand,
	}
}

// SetupBotCommands устанавливает команды для меню бота (для обычных пользователей)
func (r *Router) SetupBotCommands() error {
	// Устанавливаем только клиентские команды в панель по умолчанию
	commands := []tgbotapi.BotCommand{
		{
			Command:     "start",
			Description: "Начать работу с ботом",
		},
		{
			Command:     "buy",
			Description: "Купить ключ доступа",
		},
		{
			Command:     "my_subs",
			Description: "Мои активные подписки",
		},
	}

	setCommandsConfig := tgbotapi.NewSetMyCommands(commands...)
	_, err := r.bot.Request(setCommandsConfig)
	return err
}

// setupAdminCommands устанавливает расширенные команды для админов
func (r *Router) setupAdminCommands(chatID int64) {
	commands := []tgbotapi.BotCommand{
		{
			Command:     "start",
			Description: "Начать работу с ботом",
		},
		{
			Command:     "buy",
			Description: "Купить ключ доступа",
		},
		{
			Command:     "my_subs",
			Description: "Мои активные подписки",
		},
		{
			Command:     "create_tariff",
			Description: "Создать тариф",
		},
		{
			Command:     "disable_tariff",
			Description: "Архивировать тариф",
		},
		{
			Command:     "enable_tariff",
			Description: "Восстановить тариф",
		},
	}

	scope := tgbotapi.NewBotCommandScopeChat(chatID)
	setCommandsConfig := tgbotapi.SetMyCommandsConfig{
		Commands: commands,
		Scope:    &scope,
	}

	// Игнорируем ошибку, чтобы не блокировать основной поток
	_, _ = r.bot.Request(setCommandsConfig)
}
