package environment

import (
	"context"
	"log/slog"
	"time"

	"kurut-bot/internal/config"
	"kurut-bot/internal/infra/yookassa"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs/createsubs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram"
	"kurut-bot/internal/telegram/cmds"
	"kurut-bot/internal/telegram/flows/addserver"
	"kurut-bot/internal/telegram/flows/createsubforclient"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/flows/disabletariff"
	"kurut-bot/internal/telegram/flows/enabletariff"
	"kurut-bot/internal/telegram/states"
	"kurut-bot/internal/workers"
	"kurut-bot/internal/workers/expiration"
	"kurut-bot/internal/workers/notification"

	"github.com/pkg/errors"
)

type Services struct {
	TelegramRouter      *telegram.Router
	CreateTariffHandler *createtariff.Handler
	WorkerManager       *workers.Manager
}

func newServices(_ context.Context, clients *Clients, cfg *config.Config, logger *slog.Logger, _ *telegram.ConfigStore) (*Services, error) {
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

	// Создаем AdminChecker
	adminChecker := telegram.NewAdminChecker(&cfg.Telegram)

	// Создаем YooKassa client
	yookassaClient, err := yookassa.NewClient(cfg.YooKassa.ShopID, cfg.YooKassa.SecretKey, cfg.YooKassa.ReturnURL, logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create yookassa client")
	}

	// Создаем Payment service
	paymentService := payment.NewService(storageImpl, yookassaClient, cfg.YooKassa.ReturnURL, cfg.YooKassa.MockPayment, logger)

	// Создаем Orders service
	orderService := orders.NewService(storageImpl)

	// Создаем createSubForClientHandler
	createSubForClientHandler := createsubforclient.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		createSubService,
		paymentService,
		orderService,
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

	// Создаем addServerHandler
	addServerHandler := addserver.NewHandler(
		clients.TelegramBot,
		stateManager,
		serverService,
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

	// Создаем expirationCommand
	expirationCommand := cmds.NewExpirationCommand(
		clients.TelegramBot.GetBotAPI(),
		storageImpl,
		storageImpl, // serverStorage
		tariffService,
		paymentService,
		logger,
	)

	// Создаем воркеры (до роутера, чтобы передать в роутер)
	expirationWorker := expiration.NewWorker(
		storageImpl,
		storageImpl, // serverStorage
		clients.TelegramBot,
		tariffService,
		logger,
	)

	notificationWorker := notification.NewWorker(
		storageImpl,
		clients.TelegramBot,
		tariffService,
		logger,
	)

	// Создаем роутер
	s.TelegramRouter = telegram.NewRouter(
		clients.TelegramBot.GetBotAPI(),
		stateManager,
		userService,
		adminChecker,
		createSubForClientHandler,
		createTariffHandler,
		disableTariffHandler,
		enableTariffHandler,
		addServerHandler,
		mySubsCommand,
		statsCommand,
		expirationCommand,
		expirationWorker,
	)

	// Создаем менеджер воркеров
	s.WorkerManager = workers.NewManager(
		logger,
		expirationWorker,
		notificationWorker,
	)

	return &s, nil
}
