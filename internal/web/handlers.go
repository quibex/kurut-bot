package web

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
)

//go:embed templates/*
var templatesFS embed.FS

// Handlers provides HTTP handlers for web payments
type Handlers struct {
	tariffService      TariffService
	paymentService     PaymentService
	subscriptionStore  SubscriptionStorage
	messageStorage     SubscriptionMessageStorage
	clientTokenStorage ClientTokenStorage
	orderStorage       OrderStorage
	serverStorage      ServerStorage
	webDomain          string
	tgChannelURL       string
	tgSupportURL       string
	waSupportURL       string
	logger             *slog.Logger
}

// NewHandlers creates new web handlers
func NewHandlers(
	tariffService TariffService,
	paymentService PaymentService,
	purchaseStorage PurchaseTokenStorage,
	renewalStorage RenewalTokenStorage,
	subscriptionStore SubscriptionStorage,
	messageStorage SubscriptionMessageStorage,
	clientTokenStorage ClientTokenStorage,
	orderStorage OrderStorage,
	serverStorage ServerStorage,
	webDomain string,
	tgChannelURL string,
	tgSupportURL string,
	waSupportURL string,
	logger *slog.Logger,
) *Handlers {
	return &Handlers{
		tariffService:      tariffService,
		paymentService:     paymentService,
		subscriptionStore:  subscriptionStore,
		messageStorage:     messageStorage,
		clientTokenStorage: clientTokenStorage,
		orderStorage:       orderStorage,
		serverStorage:      serverStorage,
		webDomain:          webDomain,
		tgChannelURL:       tgChannelURL,
		tgSupportURL:       tgSupportURL,
		waSupportURL:       waSupportURL,
		logger:             logger,
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

		sort.Slice(activeTariffs, func(i, j int) bool {
			return activeTariffs[i].DurationDays < activeTariffs[j].DurationDays
		})

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
					view.StatusText = "Отключена"
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
			"Token":         token,
			"WhatsApp":      clientToken.WhatsApp,
			"Subscriptions": subViews,
			"Tariffs":       activeTariffs,
			"HasServers":    hasServers,
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
		// New subscription - create pending order (without referrer)
		order := PendingOrder{
			ClientWhatsApp:      clientToken.WhatsApp,
			TariffID:            tariffID,
			ReferrerWhatsApp:    nil,
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

		// Cancel old active messages with payments for this subscription
		h.logger.Info("Looking for old subscription messages to cancel", "subscription_id", subID)
		oldRenewalPaymentIDs, err := h.messageStorage.CancelActiveMessagesWithPayments(ctx, subID)
		if err != nil {
			h.logger.Error("Failed to cancel old subscription messages", "error", err)
		} else {
			h.logger.Info("Found old subscription messages to cancel", "count", len(oldRenewalPaymentIDs), "payment_ids", oldRenewalPaymentIDs)
			for _, paymentID := range oldRenewalPaymentIDs {
				h.logger.Info("Cancelling old renewal payment", "payment_id", paymentID)
				if err := h.paymentService.CancelPayment(ctx, paymentID); err != nil {
					h.logger.Error("Failed to cancel old renewal payment", "error", err, "payment_id", paymentID)
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
