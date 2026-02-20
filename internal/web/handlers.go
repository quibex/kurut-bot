package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/webtokens"
)

//go:embed templates/*
var templatesFS embed.FS

// Handlers provides HTTP handlers for web payments
type Handlers struct {
	tariffService       TariffService
	paymentService      PaymentService
	subscriptionCreator SubscriptionCreator
	subscriptionStore   SubscriptionStorage
	messageStorage      SubscriptionMessageStorage
	clientTokenStorage  ClientTokenStorage
	orderStorage        OrderStorage
	serverStorage       ServerStorage
	telegramBot         TelegramBot
	webDomain           string
	tgChannelURL        string
	tgSupportURL        string
	waSupportURL        string
	logger              *slog.Logger
}

// NewHandlers creates new web handlers
func NewHandlers(
	tariffService TariffService,
	paymentService PaymentService,
	subscriptionCreator SubscriptionCreator,
	purchaseStorage PurchaseTokenStorage,
	renewalStorage RenewalTokenStorage,
	subscriptionStore SubscriptionStorage,
	messageStorage SubscriptionMessageStorage,
	clientTokenStorage ClientTokenStorage,
	orderStorage OrderStorage,
	serverStorage ServerStorage,
	telegramBot TelegramBot,
	webDomain string,
	tgChannelURL string,
	tgSupportURL string,
	waSupportURL string,
	logger *slog.Logger,
) *Handlers {
	return &Handlers{
		tariffService:       tariffService,
		paymentService:      paymentService,
		subscriptionCreator: subscriptionCreator,
		subscriptionStore:   subscriptionStore,
		messageStorage:      messageStorage,
		clientTokenStorage:  clientTokenStorage,
		orderStorage:        orderStorage,
		serverStorage:       serverStorage,
		telegramBot:         telegramBot,
		webDomain:           webDomain,
		tgChannelURL:        tgChannelURL,
		tgSupportURL:        tgSupportURL,
		waSupportURL:        waSupportURL,
		logger:              logger,
	}
}

// StaticHandler serves a static file from templates
func (h *Handlers) StaticHandler(filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := templatesFS.ReadFile("templates/" + filename)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Determine content type
		if strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") {
			w.Header().Set("Content-Type", "image/jpeg")
		} else if strings.HasSuffix(filename, ".png") {
			w.Header().Set("Content-Type", "image/png")
		} else if strings.HasSuffix(filename, ".svg") {
			w.Header().Set("Content-Type", "image/svg+xml")
		}

		w.Write(data) //nolint:errcheck // static file response
	}
}

// SubscriptionView for template rendering
type SubscriptionView struct {
	ID          int64
	TariffName  string
	StatusText  string
	StatusClass string
}

// ClientPageHandler handles GET /c/{token} - shows client page with subscriptions
func (h *Handlers) ClientPageHandler() http.HandlerFunc {
	tmpl := template.Must(template.ParseFS(templatesFS, "templates/client.html"))

	return func(w http.ResponseWriter, r *http.Request) {
		// Handle both GET and POST
		if r.Method == http.MethodPost {
			h.handleClientSubmit(w, r)
			return
		}

		// Extract token from URL path: /c/{token}
		token := strings.TrimPrefix(r.URL.Path, "/c/")
		if token == "" {
			http.Error(w, "Недействительная ссылка", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Get client token
		clientToken, err := h.clientTokenStorage.GetClientTokenByToken(ctx, token)
		if err != nil {
			h.logger.Error("Failed to get client token", "error", err, "token", token)
			http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
			return
		}
		if clientToken == nil {
			http.Error(w, "Ссылка не найдена", http.StatusNotFound)
			return
		}

		// Get client's subscriptions by WhatsApp
		subscriptions, err := h.subscriptionStore.ListSubscriptions(ctx, subs.ListCriteria{
			ClientWhatsApp: &clientToken.WhatsApp,
		})
		if err != nil {
			h.logger.Error("Failed to get subscriptions", "error", err)
			http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
			return
		}

		// Get active tariffs
		activeTariffs, err := h.tariffService.GetActiveTariffs(ctx)
		if err != nil {
			h.logger.Error("Failed to get tariffs", "error", err)
			http.Error(w, "Ошибка загрузки тарифов", http.StatusInternalServerError)
			return
		}

		// Показываем пробный тариф только если у клиента нет подписок
		if len(subscriptions) == 0 {
			trialTariff, err := h.tariffService.GetTrialTariff(ctx)
			if err != nil {
				h.logger.Error("Failed to get trial tariff", "error", err)
			}
			if trialTariff != nil {
				activeTariffs = append([]*tariffs.Tariff{trialTariff}, activeTariffs...)
			}
		}

		sort.Slice(activeTariffs, func(i, j int) bool {
			return activeTariffs[i].DurationDays < activeTariffs[j].DurationDays
		})

		// Выбираем дефолтный тариф: 2 месяца (60 дней), иначе первый платный
		defaultTariffIndex := 0
		for i, t := range activeTariffs {
			if t.DurationDays >= 60 {
				defaultTariffIndex = i
				break
			}
			if t.Price > 0 && defaultTariffIndex == 0 {
				defaultTariffIndex = i
			}
		}

		// Check if servers are available
		hasServers := false
		if server, err := h.serverStorage.GetAvailableServer(ctx); err == nil && server != nil {
			hasServers = true
		}

		// Convert subscriptions to view models
		var subViews []SubscriptionView
		for _, sub := range subscriptions {
			view := SubscriptionView{
				ID:         sub.ID,
				TariffName: "Подписка",
			}

			// Status
			if sub.ExpiresAt != nil && isToday(*sub.ExpiresAt) {
				view.StatusText = "Истекает сегодня"
				view.StatusClass = "status-expired"
			} else if sub.ExpiresAt != nil {
				switch sub.Status {
				case subs.StatusActive:
					dateStr := formatRussianDate(*sub.ExpiresAt)
					remaining := formatRemainingTime(*sub.ExpiresAt)
					view.StatusText = fmt.Sprintf("Активна до %s (%s)", dateStr, remaining)
					view.StatusClass = "status-active"
				case subs.StatusExpired:
					dateStr := formatRussianDate(*sub.ExpiresAt)
					view.StatusText = "Истекла " + dateStr
					view.StatusClass = "status-expired"
				case subs.StatusDisabled:
					dateStr := formatRussianDate(*sub.ExpiresAt)
					view.StatusText = fmt.Sprintf("Отключена (истекла %s)", dateStr)
					view.StatusClass = "status-disabled"
				default:
					view.StatusText = string(sub.Status)
				}
			} else if sub.Status == subs.StatusDisabled {
				view.StatusText = "Отключена"
				view.StatusClass = "status-disabled"
			}

			subViews = append(subViews, view)
		}

		data := map[string]any{
			"Token":              token,
			"WhatsApp":           clientToken.WhatsApp,
			"Subscriptions":      subViews,
			"Tariffs":            activeTariffs,
			"DefaultTariffIndex": defaultTariffIndex,
			"HasServers":         hasServers,
			"Error":         r.URL.Query().Get("error"),
			"PaymentResult": r.URL.Query().Get("payment_result"),
			"PaymentType":   r.URL.Query().Get("type"),
			"TgChannelURL":  h.tgChannelURL,
			"TgSupportURL":  h.tgSupportURL,
			"WaSupportURL":  h.waSupportURL,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			h.logger.Error("Failed to render template", "error", err)
			http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
		}
	}
}

// handleClientSubmit handles POST /c/{token} - process payment
func (h *Handlers) handleClientSubmit(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/c/")
	if token == "" {
		http.Error(w, "Недействительная ссылка", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get client token
	clientToken, err := h.clientTokenStorage.GetClientTokenByToken(ctx, token)
	if err != nil || clientToken == nil {
		http.Error(w, "Ссылка не найдена", http.StatusNotFound)
		return
	}

	// Parse form
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/c/"+token+"?error=Ошибка+формы", http.StatusSeeOther)
		return
	}

	subscriptionID := r.FormValue("subscription_id") // "new" or numeric ID
	tariffIDStr := r.FormValue("tariff_id")

	tariffID, err := strconv.ParseInt(tariffIDStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/c/"+token+"?error=Выберите+тариф", http.StatusSeeOther)
		return
	}

	// Get tariff
	tariff, err := h.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil || tariff == nil {
		http.Redirect(w, r, "/c/"+token+"?error=Тариф+не+найден", http.StatusSeeOther)
		return
	}

	// For new subscriptions, check if servers are available
	if subscriptionID == "new" {
		server, err := h.serverStorage.GetAvailableServer(ctx)
		if err != nil || server == nil {
			h.logger.Error("No available servers", "error", err)
			http.Redirect(w, r, "/c/"+token+"?error=Нет+доступных+серверов", http.StatusSeeOther)
			return
		}
	}

	// Пробный тариф (price=0) — создаём подписку без оплаты
	if tariff.Price == 0 {
		h.handleTrialSubscription(w, r, token, clientToken, tariff)
		return
	}

	// Cancel old pending orders and their payments for this client
	h.logger.Info("Looking for old pending orders to cancel", "whatsapp", clientToken.WhatsApp)
	oldPaymentIDs, err := h.orderStorage.CancelPendingOrdersByWhatsApp(ctx, clientToken.WhatsApp)
	if err != nil {
		h.logger.Error("Failed to cancel old pending orders", "error", err)
		// Don't fail - continue with new payment
	} else {
		h.logger.Info("Found old pending orders to cancel", "count", len(oldPaymentIDs), "payment_ids", oldPaymentIDs)
		// Cancel old payments in YooKassa and DB
		for _, paymentID := range oldPaymentIDs {
			h.logger.Info("Cancelling old payment", "payment_id", paymentID)
			if err := h.paymentService.CancelPayment(ctx, paymentID); err != nil {
				h.logger.Error("Failed to cancel old payment", "error", err, "payment_id", paymentID)
			}
		}
	}

	// Determine payment type for return URL
	paymentType := "new"
	if subscriptionID != "new" {
		// For renewals, check if subscription is active or disabled
		subID, _ := strconv.ParseInt(subscriptionID, 10, 64)
		if sub, err := h.subscriptionStore.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}}); err == nil && sub != nil {
			if sub.Status == subs.StatusActive {
				paymentType = "renew_active"
			} else {
				paymentType = "renew_disabled"
			}
		}
	}

	// Build return URL with client token and payment type
	returnURL := fmt.Sprintf("%s/c/%s?payment_result=success&type=%s", h.webDomain, token, paymentType)

	// Create payment with dynamic return URL
	paymentEntity := payment.Payment{
		UserID: clientToken.CreatedByTelegramID,
		Amount: tariff.Price,
	}

	createdPayment, err := h.paymentService.CreatePaymentWithReturnURL(ctx, paymentEntity, returnURL)
	if err != nil {
		h.logger.Error("Failed to create payment", "error", err)
		http.Redirect(w, r, "/c/"+token+"?error=Ошибка+создания+платежа", http.StatusSeeOther)
		return
	}

	if subscriptionID == "new" {
		// New subscription - create pending order
		// If clientToken has partner_whatsapp, use it for partnership tracking
		var referrerWhatsApp *string
		var referralType *string
		if clientToken.PartnerWhatsApp != nil {
			referrerWhatsApp = clientToken.PartnerWhatsApp
			partnershipType := "partnership"
			referralType = &partnershipType
		}

		order := PendingOrder{
			ClientWhatsApp:      clientToken.WhatsApp,
			TariffID:            tariffID,
			ServerID:            clientToken.ServerID,
			ServerName:          clientToken.ServerName,
			ReferrerWhatsApp:    referrerWhatsApp,
			ReferralType:        referralType,
			PaymentID:           createdPayment.ID,
			CreatedByTelegramID: clientToken.CreatedByTelegramID,
		}

		if _, err := h.orderStorage.CreatePendingOrderFromWeb(ctx, order); err != nil {
			h.logger.Error("Failed to create pending order", "error", err)
			http.Redirect(w, r, "/c/"+token+"?error=Ошибка+сервера", http.StatusSeeOther)
			return
		}
	} else {
		// Renewal - create subscription message
		subID, err := strconv.ParseInt(subscriptionID, 10, 64)
		if err != nil {
			http.Redirect(w, r, "/c/"+token+"?error=Неверная+подписка", http.StatusSeeOther)
			return
		}

		// Cancel old active messages with payments for this subscription,
		// but skip messages whose payments are already approved in YooKassa
		h.logger.Info("Looking for old subscription messages to cancel", "subscription_id", subID)
		activeMessages, err := h.messageStorage.ListActiveSubscriptionMessages(ctx, subID)
		if err != nil {
			h.logger.Error("Failed to list active subscription messages", "error", err)
		} else {
			for _, msg := range activeMessages {
				if msg.PaymentID == nil {
					continue
				}
				h.logger.Info("Checking old renewal payment before cancel", "msg_id", msg.ID, "payment_id", *msg.PaymentID)
				if err := h.paymentService.CancelPayment(ctx, *msg.PaymentID); err != nil {
					if errors.Is(err, payment.ErrPaymentAlreadyApproved) {
						// Payment was already paid — don't deactivate the message,
						// the worker will process it and extend the subscription
						h.logger.Info("Old renewal payment already approved, keeping message for worker",
							"msg_id", msg.ID, "payment_id", *msg.PaymentID)
						continue
					}
					h.logger.Error("Failed to cancel old renewal payment", "error", err, "payment_id", *msg.PaymentID)
				}
				// Payment was cancelled or failed — deactivate the message
				if err := h.messageStorage.DeactivateSubscriptionMessage(ctx, msg.ID); err != nil {
					h.logger.Error("Failed to deactivate subscription message", "error", err, "msg_id", msg.ID)
				}
			}
		}

		if err := h.messageStorage.CreateSubscriptionMessageWithPayment(ctx, subID, tariffID, createdPayment.ID); err != nil {
			h.logger.Error("Failed to create subscription message", "error", err)
			http.Redirect(w, r, "/c/"+token+"?error=Ошибка+сервера", http.StatusSeeOther)
			return
		}
	}

	// Redirect to payment URL
	if createdPayment.PaymentURL != nil && *createdPayment.PaymentURL != "" {
		http.Redirect(w, r, *createdPayment.PaymentURL, http.StatusSeeOther)
		return
	}

	// Manual payment mode
	http.Redirect(w, r, "/payment/success", http.StatusSeeOther)
}

// handleTrialSubscription creates a trial subscription without payment
func (h *Handlers) handleTrialSubscription(w http.ResponseWriter, r *http.Request, token string, clientToken *webtokens.ClientToken, tariff *tariffs.Tariff) {
	ctx := r.Context()

	// Проверяем что у клиента нет подписок (защита от повторного создания)
	existingSubs, err := h.subscriptionStore.ListSubscriptions(ctx, subs.ListCriteria{
		ClientWhatsApp: &clientToken.WhatsApp,
	})
	if err != nil {
		h.logger.Error("Failed to check existing subscriptions", "error", err)
		http.Redirect(w, r, "/c/"+token+"?error=Ошибка+сервера", http.StatusSeeOther)
		return
	}
	if len(existingSubs) > 0 {
		http.Redirect(w, r, "/c/"+token+"?error=Пробный+период+доступен+только+для+новых+клиентов", http.StatusSeeOther)
		return
	}

	req := &subs.CreateSubscriptionRequest{
		UserID:              clientToken.CreatedByTelegramID,
		TariffID:            tariff.ID,
		ServerID:            clientToken.ServerID,
		ClientWhatsApp:      clientToken.WhatsApp,
		CreatedByTelegramID: clientToken.CreatedByTelegramID,
	}

	result, err := h.subscriptionCreator.CreateSubscription(ctx, req)
	if err != nil {
		h.logger.Error("Failed to create trial subscription", "error", err)
		http.Redirect(w, r, "/c/"+token+"?error=Ошибка+создания+подписки", http.StatusSeeOther)
		return
	}

	// Отправляем уведомление ассистенту о создании пробной подписки
	if err := h.sendTrialNotificationToAssistant(clientToken, result); err != nil {
		h.logger.Error("Failed to send trial notification to assistant", "error", err)
		// Не прерываем процесс, подписка уже создана
	}

	http.Redirect(w, r, "/c/"+token+"?payment_result=success&type=new", http.StatusSeeOther)
}

// sendTrialNotificationToAssistant sends notification to assistant about trial subscription creation
func (h *Handlers) sendTrialNotificationToAssistant(clientToken *webtokens.ClientToken, result *subs.CreateSubscriptionResult) error {
	// Get server info
	var err error
	if result.Subscription.ServerID != nil {
		_, err = h.serverStorage.GetServer(context.Background(), servers.GetCriteria{ID: result.Subscription.ServerID})
		if err != nil {
			h.logger.Error("Failed to get server for trial notification", "error", err, "server_id", result.Subscription.ServerID)
		}
	}

	serverPassword := ""
	if result.ServerUIPassword != nil {
		serverPassword = *result.ServerUIPassword
	}

	// Format WhatsApp link
	cleanNumber := strings.ReplaceAll(strings.ReplaceAll(clientToken.WhatsApp, "+", ""), " ", "")
	whatsappDisplay := fmt.Sprintf("[%s](https://wa.me/%s)", clientToken.WhatsApp, cleanNumber)

	text := fmt.Sprintf(
		"🎁 *Новый клиент подключил пробный тариф!*\n\n"+
			"📱 *WhatsApp:* %s\n"+
			"💰 *Тариф:* Пробный период\n"+
			"🆔 *User ID:* `%s`\n"+
			"🔑 *Пароль сервера:* `%s`\n\n"+
			"⚡ *Требуется создать ключ на сервере*",
		whatsappDisplay,
		result.GeneratedUserID,
		serverPassword,
	)

	// Build keyboard with server link
	var keyboard *tgbotapi.InlineKeyboardMarkup
	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("Сервер", *result.ServerUIURL),
			),
		)
		keyboard = &kb
	}

	msg := tgbotapi.NewMessage(clientToken.CreatedByTelegramID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	_, err = h.telegramBot.Send(msg)
	return err
}
