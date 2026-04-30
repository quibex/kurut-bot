// Package metrics defines Prometheus counters/histograms for kurut-bot SLO monitoring.
//
// See: kurut-moning/docs/superpowers/specs/2026-04-29-slo-monitoring-design.md
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Op constants — closed enum used as the `op` label value on handler counters.
const (
	OpCmdMySubs         = "cmd_my_subs"
	OpCmdStats          = "cmd_stats"
	OpCmdTariffs        = "cmd_tariffs"
	OpCmdServers        = "cmd_servers"
	OpCmdPartners       = "cmd_partners"
	OpCmdNewClient      = "cmd_new_client"
	OpTariffCreateFlow  = "tariff_create_flow"
	OpServerAddFlow     = "server_add_flow"
	OpMigrateClientFlow = "migrate_client_flow"
	OpLookupCallback    = "lookup_callback"
	OpCallbackPay       = "callback_pay"
	OpCallbackOther     = "callback_other"
	OpOther             = "other"
)

// HandlerTotal counts every dispatched update.
var HandlerTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kurut_bot_handler_total",
	Help: "Bot handler invocations by op and status (status: ok|error)",
}, []string{"op", "status"})

// HandlerDuration observes how long handlers take, by op.
var HandlerDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "kurut_bot_handler_duration_seconds",
	Help:    "Bot handler duration in seconds",
	Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
}, []string{"op"})

// PaymentCreateTotal counts every payment-creation attempt by status.
//
// Statuses: ok | fail_upstream | fail_circuit_open | fail_rate_limited |
//
//	fail_validation | fail_other
var PaymentCreateTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kurut_bot_payment_create_total",
	Help: "Payment-creation attempts by status",
}, []string{"status"})

// YooKassaCallTotal counts every YooKassa API call by op and status.
//
// Ops:    create_payment | get_payment | cancel_payment
// Status: ok | fail | fail_timeout | fail_rate_limited
var YooKassaCallTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kurut_bot_yookassa_call_total",
	Help: "YooKassa API calls by op and status",
}, []string{"op", "status"})
