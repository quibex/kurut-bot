package metrics

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestOpFromUpdate_Command(t *testing.T) {
	tests := map[string]string{
		"my_subs":     OpCmdMySubs,
		"stats":       OpCmdStats,
		"tariffs":     OpCmdTariffs,
		"servers":     OpCmdServers,
		"partners":    OpCmdPartners,
		"new_client":  OpCmdNewClient,
		"unknown_cmd": OpOther,
	}
	for cmd, want := range tests {
		u := &tgbotapi.Update{
			Message: &tgbotapi.Message{
				Text: "/" + cmd,
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: len(cmd) + 1},
				},
			},
		}
		if got := OpFromUpdate(u); got != want {
			t.Errorf("cmd %q: want %q, got %q", cmd, want, got)
		}
	}
}

func TestOpFromUpdate_CallbackPrefix(t *testing.T) {
	tests := map[string]string{
		"pay_123":   OpCallbackPay,
		"trf_chg":   OpCallbackOther,
		"lkp_xxx":   OpLookupCallback,
		"act_step1": OpTariffCreateFlow,
		"asv_step1": OpServerAddFlow,
		"amc_step1": OpMigrateClientFlow,
		"weird":     OpCallbackOther,
	}
	for data, want := range tests {
		u := &tgbotapi.Update{
			CallbackQuery: &tgbotapi.CallbackQuery{Data: data},
		}
		if got := OpFromUpdate(u); got != want {
			t.Errorf("callback %q: want %q, got %q", data, want, got)
		}
	}
}

func TestOpFromUpdate_NoMatch(t *testing.T) {
	u := &tgbotapi.Update{}
	if got := OpFromUpdate(u); got != OpOther {
		t.Errorf("empty update: want %q, got %q", OpOther, got)
	}
}
