package wgserver

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot          botApi
	stateManager StateManager
	storage      Storage
	logger       *slog.Logger
}

func NewHandler(bot botApi, stateManager StateManager, storage Storage, logger *slog.Logger) *Handler {
	return &Handler{
		bot:          bot,
		stateManager: stateManager,
		storage:      storage,
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
		if !server.Enabled {
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
	h.stateManager.SetState(chatID, StateAddName, &AddServerData{})

	msg := tgbotapi.NewMessage(chatID,
		"➕ *Добавление нового WireGuard сервера*\n\n"+
			"Шаг 1/4: Введите название сервера\n"+
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

	data := &AddServerData{Name: name}
	h.stateManager.SetState(chatID, StateAddEndpoint, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ Название сохранено: `"+name+"`\n\n"+
			"Шаг 2/4: Введите endpoint сервера\n"+
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
			"Шаг 3/4: Введите gRPC адрес сервера\n"+
			"Формат: `vpn.example.com:50051` или `1.2.3.4:50051`")
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
	h.stateManager.SetState(chatID, StateAddMaxPeers, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ gRPC адрес сохранен: `"+grpcAddr+"`\n\n"+
			"Шаг 4/4: Введите максимальное количество пиров\n"+
			"По умолчанию: 150\n"+
			"Введите число или отправьте `/skip` для значения по умолчанию")
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddMaxPeers(ctx context.Context, chatID int64, input string) error {
	input = strings.TrimSpace(input)

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	maxPeers := 150
	if input != "/skip" && input != "" {
		parsed, err := strconv.Atoi(input)
		if err != nil || parsed < 1 {
			msg := tgbotapi.NewMessage(chatID, "❌ Неверное число. Введите положительное число или `/skip`:")
			_, _ = h.bot.Send(msg)
			return nil
		}
		maxPeers = parsed
	}

	data.MaxPeers = maxPeers
	h.stateManager.SetState(chatID, StateAddTLS, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ Max пиров: `"+strconv.Itoa(maxPeers)+"`\n\n"+
			"Шаг 5/7: Использовать TLS для gRPC соединения?\n"+
			"Рекомендуется: `yes` (если бот и сервер через интернет)\n\n"+
			"Введите `yes` для включения TLS или `no` для отключения")
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddTLS(ctx context.Context, chatID int64, input string) error {
	input = strings.ToLower(strings.TrimSpace(input))

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	if input != "yes" && input != "no" {
		msg := tgbotapi.NewMessage(chatID, "❌ Введите `yes` или `no`:")
		msg.ParseMode = "Markdown"
		_, _ = h.bot.Send(msg)
		return nil
	}

	data.TLSEnabled = (input == "yes")

	if !data.TLSEnabled {
		return h.createServer(ctx, chatID, data)
	}

	h.stateManager.SetState(chatID, StateAddCertPath, data)

	msg := tgbotapi.NewMessage(chatID,
		"✅ TLS включен\n\n"+
			"Шаг 6/7: Введите путь к TLS сертификату (CA cert)\n"+
			"Например: `/etc/kurut-bot/certs/ca.crt`\n"+
			"Или `/skip` если сертификат в системном хранилище")
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddCertPath(ctx context.Context, chatID int64, input string) error {
	input = strings.TrimSpace(input)

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	if input != "/skip" && input != "" {
		data.TLSCertPath = &input
	}

	h.stateManager.SetState(chatID, StateAddServerName, data)

	msg := tgbotapi.NewMessage(chatID,
		"Шаг 7/7: Введите server name для TLS проверки\n"+
			"Обычно это доменное имя, например: `vpn.example.com`\n"+
			"Или `/skip` для использования имени из gRPC адреса")
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) HandleAddServerName(ctx context.Context, chatID int64, input string) error {
	input = strings.TrimSpace(input)

	_, dataInterface := h.stateManager.GetState(chatID)
	data, ok := dataInterface.(*AddServerData)
	if !ok {
		return h.handleError(chatID, "Ошибка состояния")
	}

	if input != "/skip" && input != "" {
		data.TLSServerName = &input
	}

	return h.createServer(ctx, chatID, data)
}

func (h *Handler) createServer(ctx context.Context, chatID int64, data *AddServerData) error {
	server := storage.WGServer{
		Name:          data.Name,
		Endpoint:      data.Endpoint,
		GRPCAddress:   data.GRPCAddress,
		Interface:     "wg0",
		DNSServers:    "1.1.1.1",
		MaxPeers:      data.MaxPeers,
		Enabled:       true,
		TLSEnabled:    data.TLSEnabled,
		TLSCertPath:   data.TLSCertPath,
		TLSServerName: data.TLSServerName,
	}

	created, err := h.storage.CreateWGServer(ctx, server)
	if err != nil {
		h.logger.Error("Failed to create WireGuard server", "error", err)
		return h.handleError(chatID, "Ошибка создания сервера")
	}

	h.stateManager.ClearState(chatID)

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
	h.stateManager.ClearState(chatID)
	msg := tgbotapi.NewMessage(chatID, "❌ "+errorMsg)
	_, err := h.bot.Send(msg)
	return err
}

