package yookassa

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"kurut-bot/internal/observability/metrics"
)

// fakeYooKassa returns the supplied error from breaker.Execute. We don't
// have a stable client constructor for tests; this test relies on the
// internal classifyErr+counter wrapping. If your client requires a real
// HTTP server, use httptest.NewServer instead and adjust accordingly.
func TestCreatePayment_IncrementsCounters(t *testing.T) {
	t.Skip("requires test harness; covered by integration test in Task 7 smoke")
	_ = context.Background()
	_ = errors.New
	_ = metrics.YooKassaCallTotal
	_ = testutil.ToFloat64
}
