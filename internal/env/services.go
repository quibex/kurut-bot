package environment

import (
	"context"
	"log/slog"
	"time"

	"kurut-bot/internal/config"
	"kurut-bot/internal/infra/yookassa"
	"kurut-bot/internal/localization"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/subs/createsubs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows/buysub"
	"kurut-bot/internal/telegram/flows/createsubforclient"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/flows/disabletariff"
	"kurut-bot/internal/telegram/flows/enabletariff"
	"kurut-bot/internal/telegram/flows/renewsub"
	"kurut-bot/internal/telegram/flows/starttrial"
	"kurut-bot/internal/telegram/states"
	"kurut-bot/internal/workers"
	"kurut-bot/internal/workers/expiration"
	"kurut-bot/internal/workers/notification"
	retrysubscription "kurut-bot/internal/workers/retry-subscription"

	"github.com/pkg/errors"
)

type Services struct {
	TelegramRouter      *telegram.Router
	CreateTariffHandler *createtariff.Handler
	WorkerManager       *workers.Manager
}

func newServices(_ context.Context, clients *Clients, cfg *config.Config, logger *slog.Logger) (*Services, error) {
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

	// Создаем реальные сервисы
	userService := users.NewService(storageImpl)
	tariffService := tariffs.NewService(storageImpl)
	subsService := subs.NewService(storageImpl)
	createSubService := createsubs.NewService(storageImpl, clients.MarzbanClient, time.Now, cfg.MarzbanClient.APIURL)

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
	paymentService := payment.NewService(storageImpl, yookassaClient, cfg.YooKassa.ReturnURL, logger)

	// Создаем buySubHandler - наш клиент уже реализует botApi интерфейс
	buySubHandler := buysub.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		createSubService,
		paymentService,
		l10nService,
		logger,
	)

	// Создаем createSubForClientHandler
	createSubForClientHandler := createsubforclient.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		createSubService,
		paymentService,
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
		logger,
	)

	notificationWorker := notification.NewWorker(
		storageImpl,
		clients.TelegramBot,
		tariffService,
		logger,
	)

	// Создаем менеджер воркеров
	s.WorkerManager = workers.NewManager(
		logger,
		retrySubWorker,
		expirationWorker,
		notificationWorker,
	)

	return &s, nil
}
