package metrics

import (
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// OpFromUpdate derives an op label from a Telegram update.
// Recognized commands and callback-data prefixes return their op constant;
// anything unrecognized returns OpOther.
func OpFromUpdate(u *tgbotapi.Update) string {
	if u == nil {
		return OpOther
	}
	if u.Message != nil && u.Message.IsCommand() {
		return opForCommand(u.Message.Command())
	}
	if u.CallbackQuery != nil {
		return opForCallback(u.CallbackQuery.Data)
	}
	return OpOther
}

func opForCommand(cmd string) string {
	switch cmd {
	case "my_subs":
		return OpCmdMySubs
	case "stats":
		return OpCmdStats
	case "tariffs":
		return OpCmdTariffs
	case "servers":
		return OpCmdServers
	case "partners":
		return OpCmdPartners
	case "new_client":
		return OpCmdNewClient
	default:
		return OpOther
	}
}

func opForCallback(data string) string {
	switch {
	case strings.HasPrefix(data, "pay_"):
		return OpCallbackPay
	case strings.HasPrefix(data, "lkp_"):
		return OpLookupCallback
	case strings.HasPrefix(data, "act_"):
		return OpTariffCreateFlow
	case strings.HasPrefix(data, "asv_"):
		return OpServerAddFlow
	case strings.HasPrefix(data, "amc_"):
		return OpMigrateClientFlow
	default:
		return OpCallbackOther
	}
}
