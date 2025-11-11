package telegram

import (
	"context"
	"strings"

	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/flows/buysub"
	"kurut-bot/internal/telegram/flows/createsubforclient"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/flows/disabletariff"
	"kurut-bot/internal/telegram/flows/enabletariff"
	"kurut-bot/internal/telegram/flows/renewsub"
	"kurut-bot/internal/telegram/flows/starttrial"
	"kurut-bot/internal/telegram/flows/wgserver"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Router struct {
	bot          *tgbotapi.BotAPI
	stateManager stateManager
	userService  userService
	adminChecker adminChecker
	l10n         localizer

	// Handler для флоу покупки подписки
	buySubHandler             *buysub.Handler
	createSubForClientHandler *createsubforclient.Handler
	createTariffHandler       *createtariff.Handler
	disableTariffHandler      *disabletariff.Handler
	enableTariffHandler       *enabletariff.Handler
	startTrialHandler         *starttrial.Handler
	renewSubHandler           *renewsub.Handler
	wgServerHandler           *wgserver.Handler
	mySubsCommand             *cmds.MySubsCommand
	statsCommand              *cmds.StatsCommand
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

type localizer interface {
	Get(lang, key string, params map[string]interface{}) string
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
		_ = r.sendError(telegramID, "ru")
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
		case callbackData == "start_trial":
			return r.handleStartTrial(update, user)
		case callbackData == "view_tariffs":
			chatID := extractChatID(update)
			// Получаем MessageID из welcome flow для бесшовного редактирования
			welcomeData, _ := r.stateManager.GetWelcomeData(chatID)
			var messageID *int
			if welcomeData != nil {
				messageID = &welcomeData.MessageID
			}
			// Отвечаем на callback query
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = r.bot.Request(callbackConfig)
			return r.buySubHandler.Start(user.ID, chatID, user.Language, messageID)
		case callbackData == "my_subscriptions":
			return r.mySubsCommand.Execute(ctx, user, extractChatID(update))
		case strings.HasPrefix(callbackData, "my_subs_page:"):
			return r.mySubsCommand.HandleCallback(ctx, user, extractChatID(update), update.CallbackQuery.Message.MessageID, callbackData)
		case callbackData == "my_subs_noop":
			// Ignore noop callback (page indicator button)
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			_, _ = r.bot.Request(callback)
			return nil
		case callbackData == "lang_ru":
			return r.handleLanguageSelection(ctx, update, user, "ru")
		case callbackData == "lang_ky":
			return r.handleLanguageSelection(ctx, update, user, "ky")
		}
	}

	// Проверяем состояние флоу покупки подписки
	if strings.HasPrefix(string(state), "ubs_") {
		return r.buySubHandler.Handle(update, state)
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

	// Проверяем состояние флоу продления подписки
	if strings.HasPrefix(string(state), "urs_") {
		return r.renewSubHandler.Handle(update, state)
	}

	// Проверяем состояние флоу управления WG серверами
	if strings.HasPrefix(string(state), "wgserver_") {
		return r.wgServerHandler.Handle(ctx, update, string(state))
	}

	// Если нет активного состояния - обрабатываем как обычное сообщение
	return r.sendHelp(extractChatID(update), user.Language)
}

func (r *Router) handleCommandWithUser(update *tgbotapi.Update, user *users.User) error {
	if update.Message == nil || !update.Message.IsCommand() {
		return r.sendHelp(extractChatID(update), user.Language)
	}

	switch update.Message.Command() {
	case "start":
		return r.sendWelcome(update.Message.Chat.ID, user)
	case "language":
		return r.sendLanguageSelection(update.Message.Chat.ID, user, nil)
	case "buy":
		return r.buySubHandler.Start(
			user.ID,
			update.Message.Chat.ID,
			user.Language,
			nil, // Команда /buy не имеет MessageID для редактирования
		)
	case "create_sub":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для создания подписок клиентам"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		return r.createSubForClientHandler.Start(
			user.ID,
			update.Message.Chat.ID,
		)
	case "create_tariff":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для создания тарифов"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		return r.createTariffHandler.Start(
			update.Message.Chat.ID,
		)
	case "disable_tariff":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для архивации тарифов"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		return r.disableTariffHandler.Start(
			update.Message.Chat.ID,
		)
	case "enable_tariff":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для восстановления тарифов"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		return r.enableTariffHandler.Start(
			update.Message.Chat.ID,
		)
	case "wg_servers":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для управления серверами"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		ctx := context.Background()
		return r.wgServerHandler.ListServers(ctx, update.Message.Chat.ID)
	case "add_wg_server":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для добавления серверов"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		return r.wgServerHandler.StartAddServer(update.Message.Chat.ID)
	case "archive_wg_server":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для архивирования серверов"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		return r.wgServerHandler.StartArchiveServer(update.Message.Chat.ID)
	case "my_subs":
		ctx := context.Background()
		return r.mySubsCommand.Execute(ctx, user, update.Message.Chat.ID)
	case "renew":
		return r.renewSubHandler.Start(user.ID, update.Message.Chat.ID, user.Language)
	case "stats":
		if !r.adminChecker.IsAdmin(user.TelegramID) {
			_, _ = r.bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для просмотра статистики"))
			return r.sendHelp(update.Message.Chat.ID, user.Language)
		}
		ctx := context.Background()
		return r.statsCommand.Execute(ctx, update.Message.Chat.ID)
	default:
		return r.sendHelp(update.Message.Chat.ID, user.Language)
	}
}

func (r *Router) sendWelcome(chatID int64, user *users.User) error {
	// Если язык не установлен - показываем выбор языка
	if user.Language == "" {
		return r.sendLanguageSelection(chatID, user, nil)
	}

	text := r.l10n.Get(user.Language, "welcome.title", nil) + "\n\n" +
		r.l10n.Get(user.Language, "welcome.description", nil)

	// Создаем кнопки
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				r.l10n.Get(user.Language, "buttons.start_trial", nil),
				"start_trial",
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				r.l10n.Get(user.Language, "buttons.view_tariffs", nil),
				"view_tariffs",
			),
		),
	)

	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды для администратора:\n" +
			"/create_sub — Создать подписку для клиента\n" +
			"/create_tariff — Создать тариф\n" +
			"/disable_tariff — Архивировать тариф\n" +
			"/enable_tariff — Восстановить тариф из архива\n" +
			"/wg_servers — Список WireGuard серверов\n" +
			"/add_wg_server — Добавить WireGuard сервер\n" +
			"/archive_wg_server — Архивировать WireGuard сервер\n" +
			"/stats — Просмотр статистики"
	}

	// Добавляем "Выберите действие:" в самый конец
	text += "\n\n" + r.l10n.Get(user.Language, "welcome.choose_action", nil)

	// Проверяем есть ли сохраненное сообщение для редактирования
	welcomeData, _ := r.stateManager.GetWelcomeData(chatID)
	if welcomeData != nil {
		// Редактируем существующее сообщение
		editMsg := tgbotapi.NewEditMessageText(chatID, welcomeData.MessageID, text)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err := r.bot.Send(editMsg)
		return err
	}

	// Отправляем новое сообщение и сохраняем его ID
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	sentMsg, err := r.bot.Send(msg)
	if err != nil {
		return err
	}

	// Сохраняем MessageID для последующего редактирования
	r.stateManager.SetState(chatID, states.StateWelcome, &flows.WelcomeFlowData{
		MessageID: sentMsg.MessageID,
		Language:  user.Language,
	})

	return nil
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

func (r *Router) sendHelp(chatID int64, lang string) error {
	if chatID == 0 {
		return nil // Не можем отправить сообщение
	}
	text := r.l10n.Get(lang, "commands.help", nil)
	if r.adminChecker.IsAdmin(chatID) {
		text += "\n\nКоманды для администратора:\n" +
			"/create_sub — Создать подписку для клиента\n" +
			"/create_tariff — Создать тариф\n" +
			"/disable_tariff — Архивировать тариф\n" +
			"/enable_tariff — Восстановить тариф из архива\n" +
			"/wg_servers — Список WireGuard серверов\n" +
			"/add_wg_server — Добавить WireGuard сервер\n" +
			"/archive_wg_server — Архивировать WireGuard сервер\n" +
			"/stats — Просмотр статистики"
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := r.bot.Send(msg)
	return err
}

func (r *Router) sendError(chatID int64, lang string) error {
	msg := tgbotapi.NewMessage(chatID, r.l10n.Get(lang, "common.error", nil))
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
func (r *Router) handleGlobalCancelWithInternalID(update *tgbotapi.Update, user *users.User) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Очищаем любое состояние (используем внутренний ID)
	r.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, r.l10n.Get(user.Language, "common.cancel", nil))
	_, err := r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем список доступных команд
	return r.sendHelp(chatID, user.Language)
}

// sendLanguageSelection отправляет меню выбора языка
func (r *Router) sendLanguageSelection(chatID int64, user *users.User, messageID *int) error {
	// Если язык пустой - используем русский для отображения
	lang := user.Language
	if lang == "" {
		lang = "ru"
	}

	text := r.l10n.Get(lang, "welcome.choose_language", nil)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇷🇺 Русский", "lang_ru"),
			tgbotapi.NewInlineKeyboardButtonData("🇰🇬 Кыргызча", "lang_ky"),
		),
	)

	// Если есть MessageID - редактируем существующее сообщение
	if messageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *messageID, text)
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
		Language:  lang,
	})

	return nil
}

// handleLanguageSelection обрабатывает выбор языка
func (r *Router) handleLanguageSelection(ctx context.Context, update *tgbotapi.Update, user *users.User, language string) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Обновляем язык пользователя
	err := r.userService.SetLanguage(ctx, user.TelegramID, language)
	if err != nil {
		return err
	}

	// Обновляем локальную копию
	user.Language = language

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, r.l10n.Get(language, "welcome.language_set", nil))
	_, err = r.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Показываем главное меню
	return r.sendWelcome(chatID, user)
}

// NewRouter создает новый роутер с зависимостями
func NewRouter(bot *tgbotapi.BotAPI, stateManager stateManager, userService userService, adminChecker adminChecker, buySubHandler *buysub.Handler, createSubForClientHandler *createsubforclient.Handler, createTariffHandler *createtariff.Handler, disableTariffHandler *disabletariff.Handler, enableTariffHandler *enabletariff.Handler, startTrialHandler *starttrial.Handler, renewSubHandler *renewsub.Handler, wgServerHandler *wgserver.Handler, mySubsCommand *cmds.MySubsCommand, statsCommand *cmds.StatsCommand, l10n localizer) *Router {
	return &Router{
		bot:                       bot,
		stateManager:              stateManager,
		userService:               userService,
		buySubHandler:             buySubHandler,
		adminChecker:              adminChecker,
		createSubForClientHandler: createSubForClientHandler,
		createTariffHandler:       createTariffHandler,
		disableTariffHandler:      disableTariffHandler,
		enableTariffHandler:       enableTariffHandler,
		startTrialHandler:         startTrialHandler,
		renewSubHandler:           renewSubHandler,
		wgServerHandler:           wgServerHandler,
		mySubsCommand:             mySubsCommand,
		statsCommand:              statsCommand,
		l10n:                      l10n,
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
			Command:     "renew",
			Description: "Продлить подписку",
		},
		{
			Command:     "my_subs",
			Description: "Мои активные подписки",
		},
		{
			Command:     "language",
			Description: "Изменить язык",
		},
	}

	setCommandsConfig := tgbotapi.NewSetMyCommands(commands...)
	_, err := r.bot.Request(setCommandsConfig)
	if err != nil {
		return err
	}

	// Устанавливаем описание бота (отображается до нажатия START)
	return r.SetupBotDescription()
}

// SetupBotDescription устанавливает описание бота
func (r *Router) SetupBotDescription() error {
	// Русское описание
	descriptionRu := `🇰🇬 Кыргызская разработка - поддержите своих!

‼️ 7 дней подписки бесплатно ‼️

🚀 Высокая скорость
💎 Стабильность подключения
💬 Отзывчивая поддержка
📱 Для телефонов и компьютеров
💳 Оплата картами РФ и СБП`

	// Кыргызское описание
	descriptionKy := `🇰🇬 Кыргыз иштеп чыгаруусу - өзүбүздүкүн колдойлу!

‼️ 7 күн акысыз жазылуу ‼️

🚀 Жогорку ылдамдык
💎 Туруктуу байланыш
💬 Тез жооп берүүчү колдоо
📱 Телефондор жана компьютерлер үчүн
💳 РФ карталары жана СБП менен төлөө`

	// Устанавливаем описание для русского языка
	paramsRu := tgbotapi.Params{
		"description":   descriptionRu,
		"language_code": "ru",
	}
	_, err := r.bot.MakeRequest("setMyDescription", paramsRu)
	if err != nil {
		return err
	}

	// Устанавливаем описание для кыргызского языка
	paramsKy := tgbotapi.Params{
		"description":   descriptionKy,
		"language_code": "ky",
	}
	_, err = r.bot.MakeRequest("setMyDescription", paramsKy)
	if err != nil {
		return err
	}

	// Устанавливаем дефолтное описание (для всех остальных языков)
	paramsDefault := tgbotapi.Params{
		"description": descriptionRu,
	}
	_, err = r.bot.MakeRequest("setMyDescription", paramsDefault)
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
			Command:     "language",
			Description: "Изменить язык",
		},
		{
			Command:     "create_sub",
			Description: "Создать подписку для клиента",
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
			Command:     "stats",
			Description: "Просмотр статистики",
		},
		{
			Command:     "wg_servers",
			Description: "Список WireGuard серверов",
		},
		{
			Command:     "add_wg_server",
			Description: "Добавить WireGuard сервер",
		},
		{
			Command:     "archive_wg_server",
			Description: "Архивировать WireGuard сервер",
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
