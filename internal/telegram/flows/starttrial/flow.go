package starttrial

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram/messages"
)

type Handler struct {
	bot                 botApi
	storage             localStorage
	tariffService       tariffService
	subscriptionService subscriptionService
	userService         userService
	configStore         configStore
	webAppBaseURL       string
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	storage localStorage,
	ts tariffService,
	ss subscriptionService,
	us userService,
	configStore configStore,
	webAppBaseURL string,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		storage:             storage,
		tariffService:       ts,
		subscriptionService: ss,
		userService:         us,
		configStore:         configStore,
		webAppBaseURL:       webAppBaseURL,
		logger:              logger,
	}
}

func (h *Handler) Start(ctx context.Context, user *users.User, chatID int64) error {
	if user.UsedTrial {
		msg := tgbotapi.NewMessage(chatID, messages.TrialAlreadyUsed)
		_, err := h.bot.Send(msg)
		return err
	}

	trialTariff, err := h.tariffService.GetTrialTariff(ctx)
	if err != nil {
		return h.sendError(chatID, messages.TrialErrorGettingTariffs)
	}

	if trialTariff == nil {
		return h.sendError(chatID, messages.TrialUnavailable)
	}

	servers, err := h.storage.ListEnabledWGServers(ctx)
	if err != nil {
		h.logger.Error("Failed to check WireGuard servers", "error", err)
		return h.sendError(chatID, messages.SubscriptionErrorServerCheck)
	}

	if len(servers) == 0 {
		h.logger.Warn("No WireGuard servers available for trial subscription")
		return h.sendError(chatID, messages.SubscriptionNoServersAvailable)
	}

	hasCapacity := false
	for _, server := range servers {
		if server.CurrentPeers < server.MaxPeers {
			hasCapacity = true
			break
		}
	}

	if !hasCapacity {
		h.logger.Warn("All WireGuard servers at capacity for trial")
		return h.sendError(chatID, messages.SubscriptionServersAtCapacity)
	}

	subReq := &subs.CreateSubscriptionRequest{
		UserID:    user.ID,
		TariffID:  trialTariff.ID,
		PaymentID: nil,
	}

	subscription, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create trial subscription", "error", err)
		return h.sendError(chatID, messages.TrialErrorCreating)
	}

	err = h.userService.MarkTrialAsUsed(ctx, user.ID)
	if err != nil {
		h.logger.Error("Failed to mark trial as used", "error", err)
	}

	return h.sendConnectionInstructions(chatID, subscription, trialTariff.Name, trialTariff.DurationDays)
}

func (h *Handler) sendConnectionInstructions(chatID int64, subscription *subs.Subscription, tariffName string, durationDays int) error {
	wgData, err := subscription.GetWireGuardData()

	if err != nil || wgData == nil || wgData.ConfigFile == "" {
		messageText := messages.FormatSubscriptionSuccessTrial(tariffName, durationDays)
		messageText += "\n\n" + messages.SubscriptionLinkNotReady

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(messages.ButtonViewTariffs, "view_tariffs"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMainMenu, "main_menu"),
			),
		)

		msg := tgbotapi.NewMessage(chatID, messageText)
		msg.ReplyMarkup = keyboard
		msg.DisableWebPagePreview = true
		_, err = h.bot.Send(msg)
		return err
	}

	successText := messages.FormatSubscriptionSuccessTrial(tariffName, durationDays)

	msg := tgbotapi.NewMessage(chatID, successText)
	msg.DisableWebPagePreview = true
	_, _ = h.bot.Send(msg)

	instructionsText := messages.SubscriptionInstructions + "\n\n" + messages.SubscriptionTrialNote

	qrBytes, err := base64.StdEncoding.DecodeString(wgData.QRCodeBase64)
	if err != nil {
		h.logger.Error("Failed to decode QR code", "error", err)
	} else {
		qrPhoto := tgbotapi.FileBytes{
			Name:  "wireguard_qr.png",
			Bytes: qrBytes,
		}

		photoMsg := tgbotapi.NewPhoto(chatID, qrPhoto)
		photoMsg.Caption = instructionsText
		_, err = h.bot.Send(photoMsg)
		if err != nil {
			h.logger.Error("Failed to send QR code photo", "error", err)
		}
	}

	configBytes := []byte(wgData.ConfigFile)
	configFile := tgbotapi.FileBytes{
		Name:  "wireguard.conf",
		Bytes: configBytes,
	}

	configID := h.configStore.Store(wgData.ConfigFile, wgData.QRCodeBase64)
	wgLink := fmt.Sprintf("%s/wg/connect?id=%s", h.webAppBaseURL, configID)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 "+messages.ButtonOpenVPNPage, wgLink),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(messages.ButtonViewTariffs, "view_tariffs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMainMenu, "main_menu"),
		),
	)

	docMsg := tgbotapi.NewDocument(chatID, configFile)
	docMsg.Caption = messages.SubscriptionConfigFile
	docMsg.ReplyMarkup = keyboard
	_, err = h.bot.Send(docMsg)
	if err != nil {
		h.logger.Error("Failed to send config file", "error", err)
	}

	return nil
}

func (h *Handler) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := h.bot.Send(msg)
	return err
}
