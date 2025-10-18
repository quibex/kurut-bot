package states

type State string

const (
	StateNone    State = "none"
	StateDone    State = "done"
	StateWelcome State = "welcome" // Состояние приветствия с сохраненным MessageID
)

// ubs -> user buy sub
// acs -> admin create sub
// act -> admin create tariff
// saa -> superadmin add admin

// user buy sub states
const (
	UserBuySubWaitTariff   State = "ubs_wt_tariff"
	UserBuySubWaitQuantity State = "ubs_wt_quantity"
	UserBuySubWaitPayment  State = "ubs_wt_payment"
)

// admin create sub states
const (
	AdminCreateSubWaitClientName State = "acs_wt_client_name"
	AdminCreateSubWaitTariff     State = "acs_wt_tariff"
	AdminCreateSubWaitPayment    State = "acs_wt_payment"
)

// admin disable sub states
const (
	AdminDisableSubWaitUser State = "ads_wt_user"
)

// admin create tariff states
const (
	AdminCreateTariffWaitName         State = "act_wt_name"
	AdminCreateTariffWaitPrice        State = "act_wt_price"
	AdminCreateTariffWaitDuration     State = "act_wt_duration"
	AdminCreateTariffWaitConfirmation State = "act_wt_confirmation"
)

// admin disable tariff states
const (
	AdminDisableTariffWaitSelection State = "adt_wt_selection"
)

// admin enable tariff states
const (
	AdminEnableTariffWaitSelection State = "aet_wt_selection"
)

// user renew sub states
const (
	UserRenewSubWaitSelection State = "urs_wt_selection"
	UserRenewSubWaitTariff    State = "urs_wt_tariff"
	UserRenewSubWaitPayment   State = "urs_wt_payment"
)
