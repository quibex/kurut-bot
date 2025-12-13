package telegram

import (
	"context"
	"strings"

	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/flows/addserver"
	"kurut-bot/internal/telegram/flows/createsubforclient"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/flows/disabletariff"
	"kurut-bot/internal/telegram/flows/enabletariff"
	"kurut-bot/internal/telegram/messages"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Router struct {
	bot          *tgbotapi.BotAPI
	stateManager stateManager
	userService  userService
	adminChecker adminChecker

	// Handlers
	createSubForClientHandler *createsubforclient.Handler
	createTariffHandler       *createtariff.Handler
	disableTariffHandler      *disabletariff.Handler
	enableTariffHandler       *enabletariff.Handler
	addServerHandler          *addserver.Handler
	mySubsCommand             *cmds.MySubsCommand
	statsCommand              *cmds.StatsCommand
	expirationCommand         *cmds.ExpirationCommand

	// Workers for manual run
	expirationRunner expirationRunner
}

type stateManager interface {
	GetState(tgUserID int64) states.State
	SetState(chatID int64, state states.State, data any)
	Clear(tgUserID int64)
	GetWelcomeData(chatID int64) (*flows.WelcomeFlowData, error)
}

type userService interface {
	GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (*users.User, error)
	SetLanguage(ctx context.Context, telegramID int64, language string) error
}

type adminChecker interface {
	IsAdmin(telegramID int64) bool
}

type expirationRunner interface {
	RunNow(ctx context.Context) error
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
		callbackData := update.CallbackQuery.Data
		switch {
		case callbackData == "cancel" || callbackData == "main_menu":
			return r.handleGlobalCancelWithInternalID(update, user)
		case callbackData == "my_subscriptions":
			return r.mySubsCommand.Execute(ctx, user.TelegramID, extractChatID(update))
		case strings.HasPrefix(callbackData, "exp_"):
			// Expiration callbacks (exp_dis, exp_pay, exp_chk)
			if !r.adminChecker.IsAdmin(user.TelegramID) {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Нет прав")
				_, _ = r.bot.Request(callback)
				return nil
			}
			return r.expirationCommand.HandleCallback(ctx, update.CallbackQuery)
		case strings.HasPrefix(callbackData, "pay_"):
			// Payment callbacks (pay_check, pay_refresh, pay_cancel) - работают независимо от состояния
			return r.createSubForClientHandler.HandlePaymentCallback(update)
		}
	}

	// Проверяем состояние флоу создания подписки для клиента
	if strings.HasPrefix(string(state), "acs_") {
		return r.createSubForClientHandler.Handle(update, state)
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

	// Проверяем состояние флоу добавления сервера
	if strings.HasPrefix(string(state), "asv_") {
		return r.addServerHandler.Handle(update, state)
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
		return r.sendWelcome(update.Message.Chat.ID, user)
	case "create_sub":
		// Любой пользователь может создавать подписки для клиентов (ассистенты)
		return r.createSubForClientHandler.Start(
			user.ID,
			user.TelegramID,
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
	case "add_server":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для добавления серверов"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		return r.addServerHandler.Start(
			update.Message.Chat.ID,
		)
	case "my_subs":
		ctx := context.Background()
		return r.mySubsCommand.Execute(ctx, user.TelegramID, update.Message.Chat.ID)
	case "stats":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для просмотра статистики"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		ctx := context.Background()
		return r.statsCommand.Execute(ctx, update.Message.Chat.ID)
	case "run_expiration":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		return r.runExpirationWorker(update.Message.Chat.ID)
	case "overdue":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		ctx := context.Background()
		return r.expirationCommand.ExecuteOverdue(ctx, update.Message.Chat.ID)
	case "expiring":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав"))
			return r.sendHelp(update.Message.Chat.ID)
		}
		ctx := context.Background()
		return r.expirationCommand.ExecuteExpiring(ctx, update.Message.Chat.ID)
	default:
		return r.sendHelp(update.Message.Chat.ID)
	}
}

func (r *Router) sendWelcome(chatID int64, user *users.User) error {
	text := "👋 Добро пожаловать!\n\nЭтот бот помогает ассистентам управлять подписками клиентов."

	// Создаем кнопки для ассистентов
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"📋 Мои подписки",
				"my_subscriptions",
			),
		),
	)

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\n🔧 Команды администратора:\n" +
			"/create_tariff — Создать тариф\n" +
			"/disable_tariff — Архивировать тариф\n" +
			"/enable_tariff — Восстановить тариф\n" +
			"/add_server — Добавить сервер\n" +
			"/stats — Просмотр статистики"
	}

	text += "\n\n📱 Команды ассистента:\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/my_subs — Список подписок"

	// Проверяем есть ли сохраненное сообщение для редактирования
	welcomeData, _ := r.stateManager.GetWelcomeData(chatID)
	if welcomeData != nil {
		// Редактируем существующее сообщение
		editMsg := tgbotapi.NewEditMessageText(chatID, welcomeData.MessageID, text)
		editMsg.ReplyMarkup = &keyboard
		_, err := r.bot.Send(editMsg)
		return err
	}

	// Отправляем новое сообщение и сохраняем его ID
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	sentMsg, err := r.bot.Send(msg)
	if err != nil {
		return err
	}

	// Сохраняем MessageID для последующего редактирования
	r.stateManager.SetState(chatID, states.StateWelcome, &flows.WelcomeFlowData{
		MessageID: sentMsg.MessageID,
	})

	return nil
}

func (r *Router) sendHelp(chatID int64) error {
	if chatID == 0 {
		return nil // Не можем отправить сообщение
	}
	text := "📱 Доступные команды:\n\n" +
		"/start — Главное меню\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/my_subs — Список подписок"

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\n🔧 Команды администратора:\n" +
			"/create_tariff — Создать тариф\n" +
			"/disable_tariff — Архивировать тариф\n" +
			"/enable_tariff — Восстановить тариф\n" +
			"/add_server — Добавить сервер\n" +
			"/stats — Просмотр статистики"
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := r.bot.Send(msg)
	return err
}

func (r *Router) sendError(chatID int64) error {
	msg := tgbotapi.NewMessage(chatID, messages.Error)
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
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}

// runExpirationWorker запускает воркер истечения подписок вручную
func (r *Router) runExpirationWorker(chatID int64) error {
	if r.expirationRunner == nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Воркер не настроен")
		_, _ = r.bot.Send(msg)
		return nil
	}

	msg := tgbotapi.NewMessage(chatID, "⏳ Запускаю проверку подписок...")
	_, _ = r.bot.Send(msg)

	ctx := context.Background()
	err := r.expirationRunner.RunNow(ctx)
	if err != nil {
		errMsg := tgbotapi.NewMessage(chatID, "❌ Ошибка: "+err.Error())
		_, _ = r.bot.Send(errMsg)
		return err
	}

	successMsg := tgbotapi.NewMessage(chatID, "✅ Проверка подписок завершена")
	_, _ = r.bot.Send(successMsg)
	return nil
}

// handleGlobalCancelWithInternalID обрабатывает глобальную отмену из любого состояния
func (r *Router) handleGlobalCancelWithInternalID(update *tgbotapi.Update, user *users.User) error {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID

	// Очищаем любое состояние (используем внутренний ID)
	r.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.Cancel)
	_, err := r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем список доступных команд
	return r.sendHelp(chatID)
}

// NewRouter создает новый роутер с зависимостями
func NewRouter(
	bot *tgbotapi.BotAPI,
	stateManager stateManager,
	userService userService,
	adminChecker adminChecker,
	createSubForClientHandler *createsubforclient.Handler,
	createTariffHandler *createtariff.Handler,
	disableTariffHandler *disabletariff.Handler,
	enableTariffHandler *enabletariff.Handler,
	addServerHandler *addserver.Handler,
	mySubsCommand *cmds.MySubsCommand,
	statsCommand *cmds.StatsCommand,
	expirationCommand *cmds.ExpirationCommand,
	expirationRunner expirationRunner,
) *Router {
	return &Router{
		bot:                       bot,
		stateManager:              stateManager,
		userService:               userService,
		adminChecker:              adminChecker,
		createSubForClientHandler: createSubForClientHandler,
		createTariffHandler:       createTariffHandler,
		disableTariffHandler:      disableTariffHandler,
		enableTariffHandler:       enableTariffHandler,
		addServerHandler:          addServerHandler,
		mySubsCommand:             mySubsCommand,
		statsCommand:              statsCommand,
		expirationCommand:         expirationCommand,
		expirationRunner:          expirationRunner,
	}
}

// SetupBotCommands устанавливает команды для меню бота
func (r *Router) SetupBotCommands() error {
	// Команды для всех пользователей (ассистентов)
	commands := []tgbotapi.BotCommand{
		{
			Command:     "start",
			Description: "Главное меню",
		},
		{
			Command:     "create_sub",
			Description: "Создать подписку для клиента",
		},
		{
			Command:     "my_subs",
			Description: "Список подписок",
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
			Description: "Главное меню",
		},
		{
			Command:     "create_sub",
			Description: "Создать подписку для клиента",
		},
		{
			Command:     "my_subs",
			Description: "Список подписок",
		},
		{
			Command:     "overdue",
			Description: "Просроченные подписки",
		},
		{
			Command:     "expiring",
			Description: "Истекающие завтра",
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
		{
			Command:     "add_server",
			Description: "Добавить сервер",
		},
		{
			Command:     "stats",
			Description: "Просмотр статистики",
		},
		{
			Command:     "run_expiration",
			Description: "Запустить проверку подписок",
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
