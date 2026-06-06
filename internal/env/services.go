package environment

import (
	"context"
	"log/slog"
	"time"

	"kurut-bot/internal/config"
	"kurut-bot/internal/infra/yookassa"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs/createsubs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows/addserver"
	assistantsflow "kurut-bot/internal/telegram/flows/assistants"
	"kurut-bot/internal/telegram/flows/createtariff"
	lookupflow "kurut-bot/internal/telegram/flows/lookup"
	"kurut-bot/internal/telegram/flows/migrateclient"
	"kurut-bot/internal/telegram/states"
	"kurut-bot/internal/web"
	"kurut-bot/internal/workers"

	// "kurut-bot/internal/workers/disablereminder" // TODO: включить позже
	"kurut-bot/internal/workers/expiration"
	"kurut-bot/internal/workers/paymentautocheck"
	"kurut-bot/internal/workers/paymentreconcile"

	"github.com/pkg/errors"
)

type Services struct {
	TelegramRouter      *telegram.Router
	CreateTariffHandler *createtariff.Handler
	WorkerManager       *workers.Manager
	WebHandlers         *web.Handlers
}

func newServices(ctx context.Context, clients *Clients, cfg *config.Config, logger *slog.Logger, _ *telegram.ConfigStore) (*Services, error) {
	var s Services

	// Инициализируем telegram сервисы
	if clients.TelegramBot == nil {
		return nil, errors.New("telegram bot не инициализирован")
	}
	// Создаем реальный storage
	storageImpl := storage.New(clients.SQLiteDB.DB)

	// Создаем реальные сервисы
	userService := users.NewService(storageImpl)
	tariffService := tariffs.NewService(storageImpl)
	serverService := servers.NewService(storageImpl)
	createSubService := createsubs.NewService(storageImpl, time.Now)

	// Создаем StateManager
	stateManager := states.NewManager()

	// Создаем AdminChecker (ростер ассистентов: env ∪ панель-управляемая БД)
	adminChecker := telegram.NewAdminChecker(&cfg.Telegram, storageImpl)
	if err := adminChecker.ReloadAssistants(ctx); err != nil {
		logger.Warn("failed to load assistant roster from DB", "err", err)
	}

	// Создаем YooKassa client
	yookassaClient, err := yookassa.NewClient(cfg.YooKassa.ShopID, cfg.YooKassa.SecretKey, cfg.YooKassa.ReturnURL, logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create yookassa client")
	}

	// Создаем Payment service
	paymentService := payment.NewService(storageImpl, yookassaClient, cfg.YooKassa.ReturnURL, cfg.YooKassa.ManualPayment, logger)

	// Создаем createTariffHandler
	createTariffHandler := createtariff.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		logger,
	)
	s.CreateTariffHandler = createTariffHandler

	// Создаем addServerHandler
	addServerHandler := addserver.NewHandler(
		clients.TelegramBot,
		stateManager,
		serverService,
		logger,
	)

	// Создаем assistantsHandler (управление ассистентами из админ-панели)
	assistantsHandler := assistantsflow.NewHandler(
		clients.TelegramBot.GetBotAPI(),
		stateManager,
		storageImpl,
		adminChecker,
		logger,
	)

	// Создаем mySubsCommand
	mySubsCommand := cmds.NewMySubsCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl,
	)

	// Создаем statsCommand
	statsCommand := cmds.NewStatsCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl,
	)

	// Создаем expirationNotificationService
	expirationNotificationService := cmds.NewExpirationNotificationService(
		clients.TelegramBot.GetBotAPI(),
		tariffService,
		storageImpl, // serverStorage
		storageImpl, // clientTokenStorage
		cfg.Web.Domain,
		logger,
	)

	// Создаем expirationCommand
	expirationCommand := cmds.NewExpirationCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl,
		storageImpl, // serverStorage
		tariffService,
		paymentService,
		storageImpl, // messageStorage
		storageImpl, // clientTokenStorage
		cfg.Web.Domain,
		expirationNotificationService,
		logger,
	)

	// Создаем tariffsCommand
	tariffsCommand := cmds.NewTariffsCommand(
		clients.TelegramBot.GetBotAPI(),
		tariffService,
		storageImpl,
		logger,
	)

	// Создаем serversCommand
	serversCommand := cmds.NewServersCommand(
		clients.TelegramBot.GetBotAPI(),
		serverService,
		logger,
	)

	// Создаем partnershipCommand
	partnershipCommand := cmds.NewPartnershipCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl,
	)

	// Создаем lookupHandler
	lookupHandler := lookupflow.NewHandler(
		clients.TelegramBot,
		stateManager,
		storageImpl, // lookupStorage
		storageImpl, // clientTokenStorage
		serverService,
		storageImpl, // subscriptionStorage
		cfg.Web.Domain,
		logger,
	)

	// Создаем newClientCommand
	newClientCommand := cmds.NewNewClientCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl, // purchaseStorage
		cfg.Web.Domain,
		logger,
	)

	// Создаем migrateClientHandler
	migrateClientHandler := migrateclient.NewHandler(
		clients.TelegramBot,
		stateManager,
		serverService,
		storageImpl, // serverStorage
		storageImpl, // clientTokenStorage
		cfg.Web.Domain,
		logger,
	)

	// Создаем expiration worker
	expirationWorker := expiration.NewWorker(
		storageImpl,
		logger,
	)

	// Создаем payment autocheck worker
	paymentAutocheckWorker := paymentautocheck.NewWorker(
		storageImpl,      // orderStorage
		storageImpl,      // messageStorage
		storageImpl,      // purchaseStorage
		storageImpl,      // renewalStorage
		paymentService,   // paymentService
		createSubService, // subscriptionService
		storageImpl,      // subscriptionStorage
		tariffService,    // tariffService
		storageImpl,      // serverStorage
		clients.TelegramBot,
		cfg.YooKassa.ManualPayment,
		logger,
	)

	// Создаем payment reconcile worker: раз в сутки закрывает pending-платежи
	// старше 14 дней через сверку с YooKassa (race-safe, использует
	// paymentService.CancelPayment) и алертит на orphaned approved.
	paymentReconcileWorker := paymentreconcile.NewWorker(
		storageImpl,    // paymentStorage
		paymentService, // paymentService
		logger,
	)

	// TODO: включить позже
	// Создаем disable reminder worker
	// disableReminderWorker := disablereminder.NewWorker(
	// 	storageImpl,
	// 	clients.TelegramBot,
	// 	expirationNotificationService,
	// 	logger,
	// )

	// Создаем роутер
	s.TelegramRouter = telegram.NewRouter(
		clients.TelegramBot.GetBotAPI(),
		stateManager,
		userService,
		adminChecker,
		serverService,
		storageImpl, // clientTokenStorage
		logger,
		createTariffHandler,
		addServerHandler,
		migrateClientHandler,
		mySubsCommand,
		statsCommand,
		expirationCommand,
		tariffsCommand,
		serversCommand,
		partnershipCommand,
		lookupHandler,
		newClientCommand,
		assistantsHandler,
	)

	// Создаем менеджер воркеров
	s.WorkerManager = workers.NewManager(
		logger,
		expirationWorker,
		paymentAutocheckWorker,
		paymentReconcileWorker,
		// disableReminderWorker, // TODO: включить позже
	)

	// Создаем web handlers
	s.WebHandlers = web.NewHandlers(
		tariffService,
		paymentService,
		createSubService,                // subscriptionCreator (для пробных подписок)
		storageImpl,                     // purchaseStorage
		storageImpl,                     // renewalStorage
		storageImpl,                     // subscriptionStore
		storageImpl,                     // messageStorage
		storageImpl,                     // clientTokenStorage
		storageImpl,                     // orderStorage
		storageImpl,                     // serverStorage
		clients.TelegramBot.GetBotAPI(), // telegramBot
		cfg.Web.Domain,
		cfg.Web.TelegramChannelURL,
		cfg.Web.TelegramSupportURL,
		cfg.Web.WhatsAppSupportURL,
		cfg.Web.NewVPNBotURL,
		cfg.Web.NewVPNSiteURL,
		cfg.Web.NewVPNGrantURL,
		cfg.Web.NewVPNGrantSecret,
		logger,
	)

	return &s, nil
}
