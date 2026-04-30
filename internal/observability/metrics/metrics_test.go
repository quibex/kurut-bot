package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHandlerTotalIncrements(t *testing.T) {
	HandlerTotal.WithLabelValues("test_op", "ok").Inc()
	got := testutil.ToFloat64(HandlerTotal.WithLabelValues("test_op", "ok"))
	if got < 1 {
		t.Fatalf("expected >=1 increment, got %v", got)
	}
}

func TestPaymentCreateTotalLabels(t *testing.T) {
	for _, status := range []string{"ok", "fail_upstream", "fail_circuit_open", "fail_rate_limited", "fail_validation", "fail_other"} {
		PaymentCreateTotal.WithLabelValues(status).Inc()
		got := testutil.ToFloat64(PaymentCreateTotal.WithLabelValues(status))
		if got < 1 {
			t.Fatalf("status %s: expected >=1, got %v", status, got)
		}
	}
}

func TestOpsConstantsExist(t *testing.T) {
	allOps := []string{
		OpCmdMySubs, OpCmdStats, OpCmdTariffs, OpCmdServers, OpCmdPartners,
		OpCmdNewClient, OpTariffCreateFlow, OpServerAddFlow, OpMigrateClientFlow,
		OpLookupCallback, OpCallbackPay, OpCallbackOther, OpOther,
	}
	for _, op := range allOps {
		if op == "" || strings.Contains(op, " ") {
			t.Fatalf("invalid op constant: %q", op)
		}
	}
}
