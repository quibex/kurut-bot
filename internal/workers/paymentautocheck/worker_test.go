package paymentautocheck

import "testing"

// The auto-checker must stop re-polling a terminally cancelled/rejected payment
// after 1 initial observation + 2 additional retries (maxTerminalPolls = 3),
// instead of re-checking it forever (kurut-z5ug).
func TestTerminalPollExhausted_AllowsTwoAdditionalRetries(t *testing.T) {
	w := &Worker{}
	const paymentID = int64(42)

	if w.terminalPollExhausted(paymentID) {
		t.Fatal("poll 1 (initial) must not exhaust the retry budget")
	}
	if w.terminalPollExhausted(paymentID) {
		t.Fatal("poll 2 (1st retry) must not exhaust the retry budget")
	}
	if !w.terminalPollExhausted(paymentID) {
		t.Fatal("poll 3 (2nd retry) must exhaust the retry budget")
	}
}

func TestTerminalPollExhausted_IndependentPerPayment(t *testing.T) {
	w := &Worker{}

	w.terminalPollExhausted(1)
	w.terminalPollExhausted(1)

	if w.terminalPollExhausted(2) {
		t.Fatal("a different payment must have its own retry budget")
	}
}

func TestForgetTerminalPolls_ResetsBudget(t *testing.T) {
	w := &Worker{}
	const paymentID = int64(7)

	w.terminalPollExhausted(paymentID)
	w.terminalPollExhausted(paymentID)
	w.forgetTerminalPolls(paymentID)

	if w.terminalPollExhausted(paymentID) {
		t.Fatal("after forgetTerminalPolls the budget must reset to a fresh count")
	}
}
