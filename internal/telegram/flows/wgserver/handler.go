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
		msg := tgbotapi.NewMessage(chatID, "📋 *Список WireGuard серверов*\n\nСерверов пока нет.\n\nИспользуйте команду для добавления нового сервера.")
		msg.ParseMode = "Markdown"
		_, err := h.bot.Send(msg)
		return err
	}

	var text strings.Builder
	text.WriteString("📋 *Список WireGuard серверов*\n\n")

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

		text.WriteString(fmt.Sprintf(
			"🖥 *%s* (ID: %d)\n"+
				"├ Endpoint: `%s`\n"+
				"├ gRPC: `%s`\n"+
				"├ Пиров: %d/%d\n"+
				"├ TLS: %s\n"+
				"└ Статус: %s\n\n",
			server.Name,
			server.ID,
			server.Endpoint,
			server.GRPCAddress,
			server.CurrentPeers,
			server.MaxPeers,
			tlsStatus,
			status,
		))
	}

	msg := tgbotapi.NewMessage(chatID, text.String())
	msg.ParseMode = "Markdown"
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) StartAddServer(chatID int64) error {
	h.stateManager.SetState(chatID, StateAddName, nil)

	msg := tgbotapi.NewMessage(chatID,
		"➕ *Добавление нового WireGuard сервера*\n\n"+
			"Шаг 1/3: Введите название сервера\n"+
			"Например: `Server DE-1` или `Main Server`")
	msg.ParseMode = "Markdown"
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
		"✅ Название сохранено: `"+name+"`\n\n"+
			"Шаг 2/3: Введите endpoint сервера\n"+
			"Формат: `vpn.example.com:51820` или `1.2.3.4:51820`")
	msg.ParseMode = "Markdown"
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
		"✅ Endpoint сохранен: `"+endpoint+"`\n\n"+
			"Шаг 3/3: Введите gRPC адрес сервера\n"+
			"Формат: `vpn.example.com:7443` или `1.2.3.4:7443`")
	msg.ParseMode = "Markdown"
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
	
	return h.createServer(ctx, chatID, data, 150)
}

func (h *Handler) createServer(ctx context.Context, chatID int64, data *AddServerData, maxPeers int) error {
	var tlsServerName *string
	
	serverName := h.tlsConfig.GetServerName()
	if serverName != "" {
		tlsServerName = &serverName
	}
	server := storage.WGServer{
		Name:          data.Name,
		Endpoint:      data.Endpoint,
		GRPCAddress:   data.GRPCAddress,
		Interface:     "wg0",
		DNSServers:    "1.1.1.1",
		MaxPeers:      maxPeers,
		Enabled:       true,
		TLSEnabled:    true,
		TLSCertPath:   nil,
		TLSServerName: tlsServerName,
	}

	created, err := h.storage.CreateWGServer(ctx, server)
	if err != nil {
		h.logger.Error("Failed to create WireGuard server", "error", err)
		return h.handleError(chatID, "Ошибка создания сервера")
	}

	h.stateManager.SetState(chatID, "", nil)

	tlsStatus := "❌ Выключен"
	if created.TLSEnabled {
		tlsStatus = "✅ Включен"
		if created.TLSCertPath != nil {
			tlsStatus += fmt.Sprintf("\n   ├ Cert: `%s`", *created.TLSCertPath)
		}
		if created.TLSServerName != nil {
			tlsStatus += fmt.Sprintf("\n   └ Server: `%s`", *created.TLSServerName)
		}
	}

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf(
			"✅ *Сервер успешно добавлен!*\n\n"+
				"🖥 *%s* (ID: %d)\n"+
				"├ Endpoint: `%s`\n"+
				"├ gRPC: `%s`\n"+
				"├ Max пиров: %d\n"+
				"├ TLS: %s\n"+
				"└ Статус: ✅ Включен",
			created.Name,
			created.ID,
			created.Endpoint,
			created.GRPCAddress,
			created.MaxPeers,
			tlsStatus,
		))
	msg.ParseMode = "Markdown"
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) handleError(chatID int64, errorMsg string) error {
	h.stateManager.SetState(chatID, "", nil)
	msg := tgbotapi.NewMessage(chatID, "❌ "+errorMsg)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) StartArchiveServer(chatID int64) error {
	h.stateManager.SetState(chatID, StateArchiveServerID, nil)

	msg := tgbotapi.NewMessage(chatID,
		"📦 *Архивирование WireGuard сервера*\n\n"+
			"Введите ID сервера для архивирования.\n"+
			"После архивирования сервер будет исключен из балансировки и healthcheck мониторинга.\n\n"+
			"Используйте команду /wg_servers для просмотра списка серверов.")
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleArchiveServerID(ctx context.Context, chatID int64, text string) error {
	var serverID int64
	if _, err := fmt.Sscanf(text, "%d", &serverID); err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Некорректный ID сервера. Введите число:")
		_, _ = h.bot.Send(msg)
		return nil
	}

	server, err := h.storage.GetWGServer(ctx, serverID)
	if err != nil {
		h.logger.Error("Failed to get WireGuard server", "error", err)
		return h.handleError(chatID, "Ошибка получения сервера")
	}
	if server == nil {
		return h.handleError(chatID, "Сервер с таким ID не найден")
	}

	if server.Archived {
		return h.handleError(chatID, "Сервер уже архивирован")
	}

	archived, err := h.storage.ArchiveWGServer(ctx, serverID)
	if err != nil {
		h.logger.Error("Failed to archive WireGuard server", "error", err)
		return h.handleError(chatID, "Ошибка архивирования сервера")
	}

	h.stateManager.SetState(chatID, "", nil)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf(
			"✅ *Сервер успешно архивирован!*\n\n"+
				"🖥 *%s* (ID: %d)\n"+
				"├ Endpoint: `%s`\n"+
				"├ gRPC: `%s`\n"+
				"├ Пиров: %d/%d\n"+
				"└ Статус: 📦 *Архивирован*\n\n"+
				"Сервер больше не будет использоваться для новых подключений и не будет проверяться healthcheck.",
			archived.Name,
			archived.ID,
			archived.Endpoint,
			archived.GRPCAddress,
			archived.CurrentPeers,
			archived.MaxPeers,
		))
	msg.ParseMode = "Markdown"
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
	case StateArchiveServerID:
		return h.HandleArchiveServerID(ctx, chatID, text)
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

