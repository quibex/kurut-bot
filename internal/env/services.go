package environment

import (
	"context"
	"log/slog"
	"time"

	"kurut-bot/internal/config"
	"kurut-bot/internal/infra/wireguard"
	"kurut-bot/internal/infra/yookassa"
	"kurut-bot/internal/localization"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/subs/createsubs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram"
	wgService "kurut-bot/internal/wireguard"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows/buysub"
	"kurut-bot/internal/telegram/flows/createsubforclient"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/flows/disabletariff"
	"kurut-bot/internal/telegram/flows/enabletariff"
	"kurut-bot/internal/telegram/flows/renewsub"
	"kurut-bot/internal/telegram/flows/starttrial"
	"kurut-bot/internal/telegram/flows/wgserver"
	"kurut-bot/internal/telegram/states"
	"kurut-bot/internal/workers"
	"kurut-bot/internal/workers/expiration"
	"kurut-bot/internal/workers/healthcheck"
	"kurut-bot/internal/workers/notification"
	retrysubscription "kurut-bot/internal/workers/retry-subscription"

	"github.com/pkg/errors"
)

type Services struct {
	TelegramRouter      *telegram.Router
	CreateTariffHandler *createtariff.Handler
	WorkerManager       *workers.Manager
}

func newServices(_ context.Context, clients *Clients, cfg *config.Config, logger *slog.Logger, configStore *telegram.ConfigStore) (*Services, error) {
	var s Services

	// Инициализируем telegram сервисы
	if clients.TelegramBot == nil {
		return nil, errors.New("telegram bot не инициализирован")
	}
	// Создаем реальный storage
	storageImpl := storage.New(clients.SQLiteDB.DB)

	// Создаем localization service
	l10nService, err := localization.NewService()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create localization service")
	}

	// Создаем WireGuard сервисы
	wgBalancer := wireguard.NewBalancer(storageImpl, logger)
	wgTLSAdapter := wgserver.NewTLSConfigAdapter(&cfg.WireGuard)
	wireguardService := wgService.NewService(storageImpl, wgBalancer, wgTLSAdapter, logger)

	// Создаем реальные сервисы
	userService := users.NewService(storageImpl)
	tariffService := tariffs.NewService(storageImpl)
	subsService := subs.NewService(storageImpl, wireguardService)
	createSubService := createsubs.NewService(storageImpl, wireguardService, time.Now)

	// Создаем StateManager
	stateManager := states.NewManager()

	// Создаем AdminChecker
	adminChecker := telegram.NewAdminChecker(&cfg.Telegram)

	// Создаем YooKassa client
	yookassaClient, err := yookassa.NewClient(cfg.YooKassa.ShopID, cfg.YooKassa.SecretKey, cfg.YooKassa.ReturnURL, logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create yookassa client")
	}

	// Создаем Payment service
	paymentService := payment.NewService(storageImpl, yookassaClient, cfg.YooKassa.ReturnURL, cfg.YooKassa.MockPayment, logger)

	// Создаем buySubHandler - наш клиент уже реализует botApi интерфейс
	buySubHandler := buysub.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		createSubService,
		paymentService,
		storageImpl,
		l10nService,
		configStore,
		cfg.WireGuard.WebAppBaseURL,
		logger,
	)

	// Создаем createSubForClientHandler
	createSubForClientHandler := createsubforclient.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		createSubService,
		paymentService,
		configStore,
		cfg.WireGuard.WebAppBaseURL,
		logger,
	)

	// Создаем createTariffHandler
	createTariffHandler := createtariff.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		logger,
	)
	s.CreateTariffHandler = createTariffHandler

	// Создаем disableTariffHandler
	disableTariffHandler := disabletariff.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		logger,
	)

	// Создаем enableTariffHandler
	enableTariffHandler := enabletariff.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		logger,
	)

	// Создаем startTrialHandler
	startTrialHandler := starttrial.NewHandler(
		clients.TelegramBot,
		tariffService,
		createSubService,
		userService,
		l10nService,
		configStore,
		cfg.WireGuard.WebAppBaseURL,
		logger,
	)

	// Создаем mySubsCommand
	mySubsCommand := cmds.NewMySubsCommand(
		clients.TelegramBot.GetBotAPI(),
		subsService,
		tariffService,
		l10nService,
	)

	// Создаем statsCommand
	statsCommand := cmds.NewStatsCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl,
	)

	// Создаем renewSubHandler
	renewSubHandler := renewsub.NewHandler(
		clients.TelegramBot,
		stateManager,
		subsService,
		tariffService,
		paymentService,
		l10nService,
		logger,
	)

	// Создаем wgServerHandler
	wgServerAdapter := wgserver.NewStateManagerAdapter(stateManager)
	wgTLSConfigAdapter := wgserver.NewTLSConfigAdapter(&cfg.WireGuard)
	wgServerHandler := wgserver.NewHandler(
		clients.TelegramBot,
		wgServerAdapter,
		storageImpl,
		wgTLSConfigAdapter,
		logger,
	)

	// Создаем роутер
	s.TelegramRouter = telegram.NewRouter(
		clients.TelegramBot.GetBotAPI(),
		stateManager,
		userService,
		adminChecker,
		buySubHandler,
		createSubForClientHandler,
		createTariffHandler,
		disableTariffHandler,
		enableTariffHandler,
		startTrialHandler,
		renewSubHandler,
		wgServerHandler,
		mySubsCommand,
		statsCommand,
		l10nService,
	)

	// Создаем воркеры
	retrySubWorker := retrysubscription.NewWorker(
		storageImpl,
		createSubService,
		clients.TelegramBot,
		l10nService,
		logger,
	)

	expirationWorker := expiration.NewWorker(
		storageImpl,
		wireguardService,
		logger,
	)

	notificationWorker := notification.NewWorker(
		storageImpl,
		clients.TelegramBot,
		tariffService,
		logger,
	)

	healthCheckWorker := healthcheck.NewWorker(
		storageImpl,
		clients.TelegramBot,
		cfg.Telegram.AdminTelegramIDs,
		logger,
	)

	// Создаем менеджер воркеров
	s.WorkerManager = workers.NewManager(
		logger,
		retrySubWorker,
		expirationWorker,
		notificationWorker,
		healthCheckWorker,
	)

	return &s, nil
}
