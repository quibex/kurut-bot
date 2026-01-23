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
	"kurut-bot/internal/telegram/flows/migrateclient"
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
	addServerHandler          *addserver.Handler
	migrateClientHandler      *migrateclient.Handler
	mySubsCommand             *cmds.MySubsCommand
	statsCommand              *cmds.StatsCommand
	expirationCommand         *cmds.ExpirationCommand
	tariffsCommand            *cmds.TariffsCommand
	serversCommand            *cmds.ServersCommand
	topReferrersCommand       *cmds.TopReferrersCommand
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
	IsAllowedUser(telegramID int64) bool
}

func (r *Router) Route(update *tgbotapi.Update) error {
	ctx := context.Background()

	// Получаем telegram_id
	telegramID := extractUserID(update)
	if telegramID == 0 {
		return nil // Некорректный update
	}

	// Проверяем доступ к боту
	if !r.adminChecker.IsAllowedUser(telegramID) {
		return r.sendAccessDenied(extractChatID(update))
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

	// Устанавливаем команды при первом взаимодействии
	if r.adminChecker.IsAdmin(telegramID) {
		r.setupAdminCommands(telegramID)
	} else {
		r.setupAssistantCommands(telegramID)
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
		case callbackData == "stats_refresh":
			if !r.adminChecker.IsAdmin(user.TelegramID) {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Нет прав")
				_, _ = r.bot.Request(callback)
				return nil
			}
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ Обновлено")
			_, _ = r.bot.Request(callback)
			chatID := update.CallbackQuery.Message.Chat.ID
			messageID := update.CallbackQuery.Message.MessageID
			return r.statsCommand.Refresh(ctx, chatID, messageID)
		case callbackData == "top_ref_refresh":
			if !r.adminChecker.IsAdmin(user.TelegramID) {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Нет прав")
				_, _ = r.bot.Request(callback)
				return nil
			}
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ Обновлено")
			_, _ = r.bot.Request(callback)
			chatID := update.CallbackQuery.Message.Chat.ID
			messageID := update.CallbackQuery.Message.MessageID
			return r.topReferrersCommand.Refresh(ctx, chatID, messageID)
		case strings.HasPrefix(callbackData, "exp_"):
			// Expiration callbacks (exp_dis, exp_link, exp_paid, exp_tariff, etc.)
			// Доступны для всех пользователей с доступом к боту (ассистентов и админов)
			return r.expirationCommand.HandleCallback(ctx, update.CallbackQuery)
		case strings.HasPrefix(callbackData, "pay_"):
			// Payment callbacks (pay_check, pay_refresh, pay_cancel) - работают независимо от состояния
			return r.createSubForClientHandler.HandlePaymentCallback(update)
		case strings.HasPrefix(callbackData, "migpay_"):
			// Migrate payment callbacks (migpay_check, migpay_refresh, migpay_cancel) - работают независимо от состояния
			return r.migrateClientHandler.HandleMigratePaymentCallback(update)
		case strings.HasPrefix(callbackData, "trf_"):
			// Tariff callbacks
			if !r.adminChecker.IsAdmin(user.TelegramID) {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Нет прав")
				_, _ = r.bot.Request(callback)
				return nil
			}
			// Специальная обработка для создания тарифа
			if callbackData == "trf_create" {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
				_, _ = r.bot.Request(callback)
				return r.createTariffHandler.Start(extractChatID(update))
			}
			return r.tariffsCommand.HandleCallback(ctx, update.CallbackQuery)
		case strings.HasPrefix(callbackData, "srv_"):
			// Server callbacks
			if !r.adminChecker.IsAdmin(user.TelegramID) {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Нет прав")
				_, _ = r.bot.Request(callback)
				return nil
			}
			// Специальная обработка для добавления сервера
			if callbackData == "srv_add" {
				callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
				_, _ = r.bot.Request(callback)
				return r.addServerHandler.Start(extractChatID(update))
			}
			return r.serversCommand.HandleCallback(ctx, update.CallbackQuery)
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

	// Проверяем состояние флоу добавления сервера
	if strings.HasPrefix(string(state), "asv_") {
		return r.addServerHandler.Handle(update, state)
	}

	// Проверяем состояние флоу миграции клиента
	if strings.HasPrefix(string(state), "amc_") {
		return r.migrateClientHandler.Handle(update, state)
	}

	// Если нет активного состояния - обрабатываем как обычное сообщение
	return r.sendHelp(extractChatID(update))
}

func (r *Router) handleCommandWithUser(update *tgbotapi.Update, user *users.User) error {
	if update.Message == nil || !update.Message.IsCommand() {
		return r.sendHelp(extractChatID(update))
	}

	ctx := context.Background()
	chatID := update.Message.Chat.ID

	switch update.Message.Command() {
	case "start":
		return r.sendWelcome(chatID, user)
	case "create_sub":
		// Любой пользователь может создавать подписки для клиентов (ассистенты)
		return r.createSubForClientHandler.Start(user.ID, user.TelegramID, chatID)
	case "tariffs":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(chatID, "❌ У вас нет прав для управления тарифами"))
			return r.sendHelp(chatID)
		}
		return r.tariffsCommand.Execute(ctx, chatID)
	case "servers":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(chatID, "❌ У вас нет прав для управления серверами"))
			return r.sendHelp(chatID)
		}
		return r.serversCommand.Execute(ctx, chatID)
	case "my_subs":
		return r.mySubsCommand.Execute(ctx, user.TelegramID, chatID)
	case "stats":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(chatID, "❌ У вас нет прав для просмотра статистики"))
			return r.sendHelp(chatID)
		}
		return r.statsCommand.Execute(ctx, chatID)
	case "top_referrers":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(chatID, "❌ У вас нет прав для просмотра топа рефералов"))
			return r.sendHelp(chatID)
		}
		return r.topReferrersCommand.Execute(ctx, chatID)
	case "overdue":
		// Все ассистенты видят все просроченные подписки
		return r.expirationCommand.ExecuteOverdue(ctx, chatID, nil)
	case "expiring":
		// Все ассистенты видят все истекающие подписки
		return r.expirationCommand.ExecuteExpiring(ctx, chatID, nil)
	case "exp3":
		// Все ассистенты видят все подписки истекающие через 3 дня
		return r.expirationCommand.ExecuteExp3(ctx, chatID, nil)
	case "migrate_client":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(chatID, "❌ У вас нет прав для миграции клиентов"))
			return r.sendHelp(chatID)
		}
		return r.migrateClientHandler.Start(user.ID, user.TelegramID, chatID)
	default:
		return r.sendHelp(chatID)
	}
}

func (r *Router) sendWelcome(chatID int64, user *users.User) error {
	text := "Добро пожаловать!\n\nЭтот бот помогает ассистентам управлять подписками клиентов."

	// Создаем кнопки для ассистентов
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"Мои подписки",
				"my_subscriptions",
			),
		),
	)

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды администратора:\n" +
			"/tariffs — Управление тарифами\n" +
			"/servers — Управление серверами\n" +
			"/stats — Просмотр статистики\n" +
			"/top_referrers — Топ рефералов за неделю\n" +
			"/overdue — Просроченные подписки\n" +
			"/expiring — Истекающие подписки\n" +
			"/exp3 — Истекающие через 3 дня"
	}

	text += "\n\nКоманды ассистента:\n" +
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
	text := "Доступные команды:\n\n" +
		"/start — Главное меню\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/my_subs — Список подписок"

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды администратора:\n" +
			"/tariffs — Управление тарифами\n" +
			"/servers — Управление серверами\n" +
			"/stats — Просмотр статистики\n" +
			"/top_referrers — Топ рефералов за неделю\n" +
			"/overdue — Просроченные подписки\n" +
			"/expiring — Истекающие подписки\n" +
			"/exp3 — Истекающие через 3 дня"
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

func (r *Router) sendAccessDenied(chatID int64) error {
	if chatID == 0 {
		return nil
	}

	text := "В небе и на земле достойный лишь Я.."
	msg := tgbotapi.NewMessage(chatID, text)
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

// handleGlobalCancelWithInternalID обрабатывает глобальную отмену из любого состояния
func (r *Router) handleGlobalCancelWithInternalID(update *tgbotapi.Update, user *users.User) error {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID
	messageID := update.CallbackQuery.Message.MessageID

	// Очищаем любое состояние (используем внутренний ID)
	r.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.Cancel)
	_, err := r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Редактируем существующее сообщение вместо отправки нового
	return r.editToHelp(chatID, messageID)
}

// editToHelp редактирует сообщение на список доступных команд
func (r *Router) editToHelp(chatID int64, messageID int) error {
	text := "Доступные команды:\n\n" +
		"/start — Главное меню\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/my_subs — Список подписок"

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды администратора:\n" +
			"/tariffs — Управление тарифами\n" +
			"/servers — Управление серверами\n" +
			"/stats — Просмотр статистики\n" +
			"/top_referrers — Топ рефералов за неделю\n" +
			"/overdue — Просроченные подписки\n" +
			"/expiring — Истекающие подписки\n" +
			"/exp3 — Истекающие через 3 дня"
	}

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	_, err := r.bot.Send(editMsg)
	return err
}

// NewRouter создает новый роутер с зависимостями
func NewRouter(
	bot *tgbotapi.BotAPI,
	stateManager stateManager,
	userService userService,
	adminChecker adminChecker,
	createSubForClientHandler *createsubforclient.Handler,
	createTariffHandler *createtariff.Handler,
	addServerHandler *addserver.Handler,
	migrateClientHandler *migrateclient.Handler,
	mySubsCommand *cmds.MySubsCommand,
	statsCommand *cmds.StatsCommand,
	expirationCommand *cmds.ExpirationCommand,
	tariffsCommand *cmds.TariffsCommand,
	serversCommand *cmds.ServersCommand,
	topReferrersCommand *cmds.TopReferrersCommand,
) *Router {
	return &Router{
		bot:                       bot,
		stateManager:              stateManager,
		userService:               userService,
		adminChecker:              adminChecker,
		createSubForClientHandler: createSubForClientHandler,
		createTariffHandler:       createTariffHandler,
		addServerHandler:          addServerHandler,
		migrateClientHandler:      migrateClientHandler,
		mySubsCommand:             mySubsCommand,
		statsCommand:              statsCommand,
		expirationCommand:         expirationCommand,
		tariffsCommand:            tariffsCommand,
		serversCommand:            serversCommand,
		topReferrersCommand:       topReferrersCommand,
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
			Command:     "tariffs",
			Description: "Управление тарифами",
		},
		{
			Command:     "servers",
			Description: "Управление серверами",
		},
		{
			Command:     "stats",
			Description: "Просмотр статистики",
		},
		{
			Command:     "top_referrers",
			Description: "Топ рефералов за неделю",
		},
		{
			Command:     "overdue",
			Description: "Просроченные подписки",
		},
		{
			Command:     "expiring",
			Description: "Истекающие сегодня",
		},
		{
			Command:     "exp3",
			Description: "Истекающие через 3 дня",
		},
		{
			Command:     "migrate_client",
			Description: "Миграция существующего клиента",
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

// setupAssistantCommands устанавливает команды для ассистентов (без админских)
func (r *Router) setupAssistantCommands(chatID int64) {
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
			Description: "Мои просроченные подписки",
		},
		{
			Command:     "expiring",
			Description: "Мои истекающие подписки",
		},
		{
			Command:     "exp3",
			Description: "Истекающие через 3 дня",
		},
	}

	scope := tgbotapi.NewBotCommandScopeChat(chatID)
	setCommandsConfig := tgbotapi.SetMyCommandsConfig{
		Commands: commands,
		Scope:    &scope,
	}

	_, _ = r.bot.Request(setCommandsConfig)
}
