package environment

import (
	"context"
	"log/slog"
	"time"

	"kurut-bot/internal/config"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/subs/createsubs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram"
	"kurut-bot/internal/telegram/flows/buysub"
	"kurut-bot/internal/telegram/flows/createtariff"
	"kurut-bot/internal/telegram/states"

	"github.com/pkg/errors"
)

type Services struct {
	TelegramRouter      *telegram.Router
	CreateTariffHandler *createtariff.Handler
}

func newServices(ctx context.Context, clients *Clients, cfg *config.Config, logger *slog.Logger) (*Services, error) {
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
	createSubService := createsubs.NewService(storageImpl, clients.MarzbanClient, time.Now, cfg.MarzbanClient.APIURL)

	// Создаем StateManager
	stateManager := states.NewManager()

	// Создаем AdminChecker
	adminChecker := telegram.NewAdminChecker(&cfg.Telegram)

	// Создаем Mock-сервисы для buySubHandler (временно)
	// mockSubscriptionService := buysub.NewMockSubscriptionService()
	mockPaymentService := buysub.NewMockPaymentService()

	// Создаем buySubHandler - наш клиент уже реализует botApi интерфейс
	buySubHandler := buysub.NewHandler(
		clients.TelegramBot,
		stateManager,
		tariffService,
		createSubService,
		mockPaymentService,
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

	// Создаем роутер
	s.TelegramRouter = telegram.NewRouter(
		clients.TelegramBot.GetBotAPI(),
		stateManager,
		userService,
		adminChecker,
		buySubHandler,
		createTariffHandler,
	)

	return &s, nil
}
