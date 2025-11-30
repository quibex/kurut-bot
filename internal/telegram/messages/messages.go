package messages

import (
	"fmt"
)

// Общие
const (
	Error    = "❌ Ошибка. Пожалуйста, попробуйте позже."
	Cancel   = "Отменено"
	MainMenu = "Главное меню"
	Back     = "Назад"
)

// Кнопки
const (
	ButtonStartTrial      = "🎁 Начать пробный период"
	ButtonViewTariffs     = "📋 Посмотреть тарифы"
	ButtonMySubscriptions = "📋 Мои подписки"
	ButtonMainMenu        = "🏠 Главное меню"
	ButtonCancel          = "❌ Отменить"
	ButtonPaid            = "✅ Оплатил"
	ButtonCancelPurchase  = "❌ Отменить"
	ButtonCheckAgain      = "🔄 Проверить еще раз"
	ButtonRetry           = "🔄 Попробовать еще раз"
	ButtonRenew           = "♻️ Продлить"
	ButtonOpenVPNPage     = "Открыть страницу подключения"
)

// Flow messages
const (
	FlowUseButtons = "Используйте кнопки для выбора"
)

// Приветствие
const (
	WelcomeTitle       = "👋 Добро пожаловать!"
	WelcomeDescription = `Получите быстрый и стабильный VPN доступ.

🎁 7 дней бесплатно для новых пользователей!`
	WelcomeChooseAction = "Выберите действие:"
)

// Тарифы
const (
	TariffsChoose       = "📅 Выберите тариф:"
	TariffsNoActive     = "❌ К сожалению, активных тарифов сейчас нет"
	TariffsPleaseSelect = "Пожалуйста, выберите тариф из меню"
	TariffsInvalidData  = "Неверные данные тарифа"
)

// Платежи
const (
	PaymentCreating        = "Создаём заказ..."
	PaymentChecking        = "Проверяем платеж..."
	PaymentErrorCreating   = "❌ Ошибка создания платежа"
	PaymentErrorPaymentURL = "❌ Ошибка генерации ссылки на оплату"
	PaymentErrorChecking   = "❌ Ошибка проверки платежа. Попробуйте еще раз."
	PaymentNotFound        = "❌ Ошибка: платеж не найден"
	PaymentPending         = "⏳ Платеж еще обрабатывается.\nПожалуйста, подождите немного и попробуйте еще раз."
	PaymentRejected        = "❌ Платеж был отклонен или отменен"
	PaymentUnknownStatus   = "❌ Неизвестный статус платежа. Попробуйте еще раз."
)

// Подписки
const (
	SubscriptionSuccessPaid = `✅ Оплата прошла успешно!

🎉 Ваша подписка активирована!`
	SubscriptionLinkNotReady           = "❌ Ссылка подключения не готова"
	SubscriptionConfigFile             = "📄 Конфигурационный файл WireGuard"
	SubscriptionErrorCreating          = "❌ Ошибка создания подписки"
	SubscriptionErrorCreatingWillRetry = `⚠️ Произошла временная ошибка при создании подписки.

🔄 Не переживайте! Ваш платеж успешно обработан.
Система автоматически повторит попытку создания подписки.

✅ Вы получите уведомление с доступом, как только подписка будет создана.

💡 Обычно это занимает не более 5-10 минут.`
	SubscriptionErrorSendingInstructions = "❌ Ошибка отправки инструкций"
	SubscriptionErrorServerCheck         = "❌ Ошибка проверки доступности серверов. Попробуйте позже."
	SubscriptionNoServersAvailable       = `⚠️ К сожалению, VPN серверы временно недоступны.

Пожалуйста, попробуйте позже или обратитесь в поддержку.`
	SubscriptionServersAtCapacity = `⚠️ Все VPN серверы сейчас заполнены.

Пожалуйста, попробуйте через некоторое время.`
	SubscriptionInstructions = `📋 Инструкция по подключению:

📱 1. Скачайте приложение WireGuard:
• Android: Google Play - com.wireguard.android
• iOS: App Store - WireGuard
• Desktop: wireguard.com/install

📋 2. Настройте подключение:
• Скопируйте конфигурацию выше
• Откройте WireGuard
• Нажмите + (Добавить туннель)
• Выберите "Создать из буфера обмена" или отсканируйте QR-код`
	SubscriptionTrialNote   = "💡 После окончания пробного периода используйте /buy для покупки платного тарифа"
	SubscriptionSupportNote = "❓ Проблемы с подключением? Обратитесь в поддержку"
)

// Триал
const (
	TrialAlreadyUsed = `❌ Вы уже использовали пробный период.

Используйте /buy чтобы выбрать платный тариф.`
	TrialErrorGettingTariffs = "❌ Ошибка получения тарифов"
	TrialUnavailable         = "❌ Пробный период временно недоступен"
	TrialErrorCreating       = "❌ Ошибка создания пробной подписки"
)

// Мои подписки
const (
	MySubsTitle           = "📋 Мои подписки"
	MySubsNoSubscriptions = "У вас пока нет активных подписок. Используйте /buy чтобы купить доступ."
	MySubsErrorLoading    = "❌ Ошибка загрузки подписок"
	MySubsRenewNote       = "Для продления подписки используйте /renew"
	MySubsYourConfig      = "🔧 Конфигурация WireGuard:"
)

// Продление
const (
	RenewTitle                 = "♻️ Продление подписки"
	RenewChooseSubscription    = "Выберите подписку для продления:"
	RenewNoSubscriptions       = "❌ У вас нет подписок для продления."
	RenewInvalidSubscription   = "Неверные данные подписки"
	RenewInvalidTariff         = "Неверные данные тарифа"
	RenewErrorLoadingTariff    = "❌ Ошибка загрузки тарифа"
	RenewChooseDifferentTariff = "Выбрать другой тариф"
	RenewErrorRenewing         = "❌ Ошибка продления подписки"
)

// Флоу
const (
	FlowErrorGettingData = "Ошибка получения данных флоу"
	FlowUnknownCommand   = "Неизвестная команда"
	FlowReturningToMenu  = "Возвращаемся в главное меню"
)

// Форматирование сообщений с параметрами
func FormatSubscriptionSuccessTrial(tariffName string, durationDays int) string {
	return fmt.Sprintf(`🎉 Пробный период активирован!

📅 Тариф: %s (%d дней)`, tariffName, durationDays)
}

func FormatPaymentOrderCreated(orderID int64, tariffName string, amount float64) string {
	return fmt.Sprintf(`💳 Заказ создан!

📋 Заказ #%d
📅 Тариф: %s
💰 Сумма: %.2f ₽

🔗 Перейдите по ссылке для оплаты.
После оплаты вернитесь сюда и нажмите «Оплатил».`, orderID, tariffName, amount)
}

func FormatPayButtonText(amount float64) string {
	return fmt.Sprintf("💳 Оплатить %.2f ₽", amount)
}

func FormatMySubsSubscriptionID(id int64) string {
	return fmt.Sprintf("🔹 Подписка #%d", id)
}

func FormatMySubsTariff(name string) string {
	return fmt.Sprintf("📦 Тариф: %s", name)
}

func FormatMySubsClient(name string) string {
	return fmt.Sprintf("👤 Клиент: %s", name)
}

func FormatMySubsTrafficLimit(gb int) string {
	return fmt.Sprintf("📊 Трафик: %d ГБ", gb)
}

const MySubsTrafficUnlimited = "📊 Трафик: безлимитный"

func FormatMySubsDaysLeft(days int) string {
	return fmt.Sprintf("⏳ Осталось дней: %d", days)
}

func FormatMySubsExpiresAt(date string) string {
	return fmt.Sprintf("📅 Действует до: %s", date)
}

const MySubsExpiresToday = "⚠️ Подписка истекает сегодня"

// Commands
const (
	CommandsHelp = `Доступные команды:
/start — Начать работу
/buy — Купить ключ доступа
/renew — Продлить подписку
/my_subs — Мои активные подписки`
)

func FormatRenewQuickTitle(subID int64, tariffName, expiresAt string) string {
	return fmt.Sprintf(`♻️ Продление подписки

🔑 Подписка #%d
📦 Тариф: %s
📅 Действует до: %s

Выберите способ продления:`, subID, tariffName, expiresAt)
}

func FormatRenewQuickSame(duration string, price float64) string {
	return fmt.Sprintf("%s - %.2f ₽", duration, price)
}

func FormatRenewSubscriptionButton(subID int64, tariffName, expiresAt string) string {
	return fmt.Sprintf("#%d: %s (до %s)", subID, tariffName, expiresAt)
}

func FormatRenewSuccess(subID int64, daysAdded int, expiresAt string) string {
	return fmt.Sprintf(`✅ Подписка успешно продлена!

🔑 Подписка #%d
➕ Добавлено: %d дней
📅 Новая дата истечения: %s`, subID, daysAdded, expiresAt)
}

// Duration formatters
func FormatDuration1Day() string {
	return "1 день"
}

func FormatDurationDays(days int) string {
	if days%10 == 1 && days != 11 {
		return fmt.Sprintf("%d день", days)
	} else if days%10 >= 2 && days%10 <= 4 && (days < 10 || days > 20) {
		return fmt.Sprintf("%d дня", days)
	}
	return fmt.Sprintf("%d дней", days)
}

func FormatDuration1Month() string {
	return "1 месяц"
}

func FormatDurationMonths(months int) string {
	if months%10 == 1 && months != 11 {
		return fmt.Sprintf("%d месяц", months)
	} else if months%10 >= 2 && months%10 <= 4 && (months < 10 || months > 20) {
		return fmt.Sprintf("%d месяца", months)
	}
	return fmt.Sprintf("%d месяцев", months)
}

func FormatDuration1Year() string {
	return "1 год"
}

func FormatDurationYears(years int) string {
	if years%10 == 1 && years != 11 {
		return fmt.Sprintf("%d год", years)
	} else if years%10 >= 2 && years%10 <= 4 && (years < 10 || years > 20) {
		return fmt.Sprintf("%d года", years)
	}
	return fmt.Sprintf("%d лет", years)
}

// Retry subscription worker messages
const (
	SubscriptionRetrySuccess     = "✅ Ваша подписка успешно создана!"
	SubscriptionRetrySuccessBody = "Используйте конфигурацию выше для подключения к VPN."
)

func FormatSubscriptionRetrySuccess(tariffName string) string {
	return fmt.Sprintf("✅ Ваша подписка на тариф «%s» успешно создана!", tariffName)
}
