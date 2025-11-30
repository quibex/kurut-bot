package wgserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot          botApi
	stateManager StateManager
	storage      Storage
	tlsConfig    TLSConfig
	logger       *slog.Logger
}

func NewHandler(bot botApi, stateManager StateManager, storage Storage, tlsConfig TLSConfig, logger *slog.Logger) *Handler {
	return &Handler{
		bot:          bot,
		stateManager: stateManager,
		storage:      storage,
		tlsConfig:    tlsConfig,
		logger:       logger,
	}
}

func (h *Handler) ListServers(ctx context.Context, chatID int64) error {
	servers, err := h.storage.ListWGServers(ctx)
	if err != nil {
		h.logger.Error("Failed to list WireGuard servers", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки списка серверов")
		_, _ = h.bot.Send(msg)
		return err
	}

	if len(servers) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📋 Список WireGuard серверов\n\nСерверов пока нет.\n\nИспользуйте команду для добавления нового сервера.")
		_, err := h.bot.Send(msg)
		return err
	}

	var text strings.Builder
	text.WriteString("📋 Список WireGuard серверов\n\n")

	for _, server := range servers {
		status := "✅ Включен"
		if server.Archived {
			status = "📦 Архивирован"
		} else if !server.Enabled {
			status = "❌ Выключен"
		}

		tlsStatus := "❌"
		if server.TLSEnabled {
			tlsStatus = "✅"
		}

		healthStatus := "❌ Не настроен"
		if server.HealthEndpoint != "" {
			healthStatus = server.HealthEndpoint
		}

		text.WriteString(fmt.Sprintf(
			"🖥 %s (ID: %d)\n"+
				"├ Endpoint: %s\n"+
				"├ gRPC: %s\n"+
				"├ Health: %s\n"+
				"├ Пиров: %d/%d\n"+
				"├ TLS: %s\n"+
				"└ Статус: %s\n\n",
			server.Name,
			server.ID,
			server.Endpoint,
			server.GRPCAddress,
			healthStatus,
			server.CurrentPeers,
			server.MaxPeers,
			tlsStatus,
			status,
		))
	}

	msg := tgbotapi.NewMessage(chatID, text.String())
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) StartAddServer(chatID int64) error {
	h.stateManager.SetState(chatID, StateAddName, nil)

	msg := tgbotapi.NewMessage(chatID,
		"➕ Добавление нового WireGuard сервера\n\n"+
			"Шаг 1/4: Введите название сервера\n"+
			"Например: Server DE-1 или Main Server")
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddName(ctx context.Context, chatID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		msg := tgbotapi.NewMessage(chatID, "❌ Название не может быть пустым. Попробуйте еще раз:")
		_, _ = h.bot.Send(msg)
		return nil
	}

	_, dataInterface := h.stateManager.GetState(chatID)
	var data *AddServerData
	if dataInterface != nil {
		data, _ = dataInterface.(*AddServerData)
	}
	if data == nil {
		data = &AddServerData{}
	}
	data.Name = name
	h.stateManager.SetState(chatID, StateAddEndpoint, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ Название сохранено: "+name+"\n\n"+
			"Шаг 2/4: Введите endpoint сервера (для клиентов WireGuard)\n"+
			"Формат: vpn.example.com:51820 или 1.2.3.4:51820")
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddEndpoint(ctx context.Context, chatID int64, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		msg := tgbotapi.NewMessage(chatID, "❌ Endpoint не может быть пустым. Попробуйте еще раз:")
		_, _ = h.bot.Send(msg)
		return nil
	}

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	data.Endpoint = endpoint
	h.stateManager.SetState(chatID, StateAddGRPCAddr, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ Endpoint сохранен: "+endpoint+"\n\n"+
			"Шаг 3/4: Введите gRPC адрес сервера (для управления)\n"+
			"Формат: vpn.example.com:7443 или 1.2.3.4:7443")
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddGRPC(ctx context.Context, chatID int64, grpcAddr string) error {
	grpcAddr = strings.TrimSpace(grpcAddr)
	if grpcAddr == "" {
		msg := tgbotapi.NewMessage(chatID, "❌ gRPC адрес не может быть пустым. Попробуйте еще раз:")
		_, _ = h.bot.Send(msg)
		return nil
	}

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	data.GRPCAddress = grpcAddr
	h.stateManager.SetState(chatID, StateAddHealthEndpoint, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ gRPC адрес сохранен: "+grpcAddr+"\n\n"+
			"Шаг 4/4: Введите Health endpoint (для мониторинга)\n"+
			"Формат: 1.2.3.4:8080/health или vpn.example.com:8080/health\n\n"+
			"Или отправьте - чтобы пропустить (healthcheck будет отключен)")
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddHealthEndpoint(ctx context.Context, chatID int64, healthEndpoint string) error {
	healthEndpoint = strings.TrimSpace(healthEndpoint)

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	// Если пользователь ввёл "-", пропускаем health endpoint
	if healthEndpoint != "-" && healthEndpoint != "" {
		data.HealthEndpoint = healthEndpoint
	}

	return h.createServer(ctx, chatID, data, 150)
}

func (h *Handler) createServer(ctx context.Context, chatID int64, data *AddServerData, maxPeers int) error {
	var tlsServerName *string

	serverName := h.tlsConfig.GetServerName()
	if serverName != "" {
		tlsServerName = &serverName
	}
	server := storage.WGServer{
		Name:           data.Name,
		Endpoint:       data.Endpoint,
		GRPCAddress:    data.GRPCAddress,
		HealthEndpoint: data.HealthEndpoint,
		Interface:      "wg0",
		DNSServers:     "1.1.1.1",
		MaxPeers:       maxPeers,
		Enabled:        true,
		TLSEnabled:     true,
		TLSCertPath:    nil,
		TLSServerName:  tlsServerName,
	}

	created, err := h.storage.CreateWGServer(ctx, server)
	if err != nil {
		h.logger.Error("Failed to create WireGuard server", "error", err)
		return h.handleError(chatID, "Ошибка создания сервера")
	}

	h.stateManager.SetState(chatID, "", nil)

	healthStatus := "❌ Не настроен"
	if created.HealthEndpoint != "" {
		healthStatus = created.HealthEndpoint
	}

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf(
			"✅ Сервер успешно добавлен!\n\n"+
				"🖥 %s (ID: %d)\n"+
				"├ Endpoint: %s\n"+
				"├ gRPC: %s\n"+
				"├ Health: %s\n"+
				"├ Max пиров: %d\n"+
				"└ Статус: ✅ Включен",
			created.Name,
			created.ID,
			created.Endpoint,
			created.GRPCAddress,
			healthStatus,
			created.MaxPeers,
		))
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) handleError(chatID int64, errorMsg string) error {
	h.stateManager.SetState(chatID, "", nil)
	msg := tgbotapi.NewMessage(chatID, "❌ "+errorMsg)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) StartArchiveServer(ctx context.Context, chatID int64) error {
	servers, err := h.storage.ListWGServers(ctx)
	if err != nil {
		h.logger.Error("Failed to list WireGuard servers", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки списка серверов")
		_, _ = h.bot.Send(msg)
		return err
	}

	// Фильтруем только неархивированные серверы
	var activeServers []*storage.WGServer
	for _, s := range servers {
		if !s.Archived {
			activeServers = append(activeServers, s)
		}
	}

	if len(activeServers) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📦 Нет активных серверов для архивирования")
		_, _ = h.bot.Send(msg)
		return nil
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, server := range activeServers {
		btnText := fmt.Sprintf("%s (пиров: %d/%d)", server.Name, server.CurrentPeers, server.MaxPeers)
		callbackData := fmt.Sprintf("wg_archive:%d", server.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnText, callbackData),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "wg_archive:cancel"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID,
		"📦 Архивирование WireGuard сервера\n\n"+
			"Выберите сервер для архивирования.\n"+
			"После архивирования сервер будет исключен из балансировки и healthcheck мониторинга.")
	msg.ReplyMarkup = keyboard
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) HandleArchiveCallback(ctx context.Context, chatID int64, callbackID string, data string) error {
	// data format: "wg_archive:123" or "wg_archive:cancel"
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	if parts[1] == "cancel" {
		msg := tgbotapi.NewMessage(chatID, "❌ Архивирование отменено")
		_, _ = h.bot.Send(msg)
		return nil
	}

	var serverID int64
	if _, err := fmt.Sscanf(parts[1], "%d", &serverID); err != nil {
		return nil
	}

	server, err := h.storage.GetWGServer(ctx, serverID)
	if err != nil {
		h.logger.Error("Failed to get WireGuard server", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка получения сервера")
		_, _ = h.bot.Send(msg)
		return err
	}
	if server == nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Сервер не найден")
		_, _ = h.bot.Send(msg)
		return nil
	}

	if server.Archived {
		msg := tgbotapi.NewMessage(chatID, "❌ Сервер уже архивирован")
		_, _ = h.bot.Send(msg)
		return nil
	}

	archived, err := h.storage.ArchiveWGServer(ctx, serverID)
	if err != nil {
		h.logger.Error("Failed to archive WireGuard server", "error", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка архивирования сервера")
		_, _ = h.bot.Send(msg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf(
			"✅ Сервер успешно архивирован!\n\n"+
				"🖥 %s (ID: %d)\n"+
				"├ Endpoint: %s\n"+
				"├ gRPC: %s\n"+
				"├ Пиров: %d/%d\n"+
				"└ Статус: 📦 Архивирован\n\n"+
				"Сервер больше не будет использоваться для новых подключений и не будет проверяться healthcheck.",
			archived.Name,
			archived.ID,
			archived.Endpoint,
			archived.GRPCAddress,
			archived.CurrentPeers,
			archived.MaxPeers,
		))
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) Handle(ctx context.Context, update *tgbotapi.Update, state string) error {
	chatID := extractChatID(update)

	if update.Message == nil || update.Message.Text == "" {
		return nil
	}

	text := update.Message.Text

	switch state {
	case StateAddName:
		return h.HandleAddName(ctx, chatID, text)
	case StateAddEndpoint:
		return h.HandleAddEndpoint(ctx, chatID, text)
	case StateAddGRPCAddr:
		return h.HandleAddGRPC(ctx, chatID, text)
	case StateAddHealthEndpoint:
		return h.HandleAddHealthEndpoint(ctx, chatID, text)
	default:
		h.stateManager.SetState(chatID, "", nil)
		return nil
	}
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
