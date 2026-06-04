package config

import (
	"fmt"
	"time"
)

type Config struct {
	Env              string                  `env:"ENV,default=local"`
	Logger           LoggerConfig            `env:",prefix=LOGGER_"`
	Observability    ObservabilityHTTPConfig `env:",prefix=OBSERVABILITY_"`
	ShutdownDuration time.Duration           `env:"SHUTDOWN_DURATION,default=30s"`
	DB               SQLiteConfig            `env:",prefix=DB_"`
	Telegram         TelegramConfig          `env:",prefix=TELEGRAM_"`
	YooKassa         YooKassaConfig          `env:",prefix=YOOKASSA_"`
	Web              WebConfig               `env:",prefix=WEB_"`
	Metrics          struct {
		Collector struct {
			Timeout time.Duration `env:"COLLECTOR_TIMEOUT,default=10s"`
		} `env:",prefix=COLLECTOR_"`
	} `env:"METRICS"`
}

type WebConfig struct {
	Domain             string `env:"DOMAIN,default=https://kurutvpn.com"`
	TelegramChannelURL string `env:"TG_CHANNEL_URL,default=https://t.me/kurutkyrgyz"`
	TelegramSupportURL string `env:"TG_SUPPORT_URL,default=https://t.me/kurutsupport"`
	WhatsAppSupportURL string `env:"WA_SUPPORT_URL,default=https://wa.me/79670042901"`
	// Новый VPN (kurut-pie) — клиентов курут-бота постепенно переносим на основной бот.
	NewVPNBotURL  string `env:"NEW_VPN_BOT_URL,default=https://t.me/kurut_vpn_bot"`
	NewVPNSiteURL string `env:"NEW_VPN_SITE_URL,default=https://kurutpro.ru"`
	// Авто-перенос: эндпоинт kurut-pie POST /api/migration/grant и общий секрет.
	// Пустой секрет = авто-перенос выключен, кнопки ведут на обычные ссылки выше.
	NewVPNGrantURL    string `env:"NEW_VPN_GRANT_URL,default=https://kurutpro.ru/api/migration/grant"`
	NewVPNGrantSecret string `env:"NEW_VPN_GRANT_SECRET,default="`
}

type TelegramConfig struct {
	BotToken     string        `env:"BOT_TOKEN,required"`
	Timeout      time.Duration `env:"TIMEOUT,default=30s"`
	ProxyURL     string        `env:"PROXY_URL,default=socks5://178.236.243.255:1080"`
	AdminIDs     []int64       `env:"ADMIN_IDS"`
	AssistantIDs []int64       `env:"ASSISTANT_IDS"`
}

type YooKassaConfig struct {
	ShopID        string `env:"SHOP_ID,required"`
	SecretKey     string `env:"SECRET_KEY,required"`
	ReturnURL     string `env:"RETURN_URL,default=https://example.com/payment/return"`
	ManualPayment bool   `env:"MANUAL_PAYMENT,default=false"`
}

type HTTPClientConfig struct {
	Scheme        string        `env:"SCHEME,default=http"`
	Host          string        `env:"HOST,default=127.0.0.1"`
	Port          uint16        `env:"PORT,default=9000"`
	Timeout       time.Duration `env:"TIMEOUT,default=30s"`
	MaxRetries    int           `env:"MAX_RETRIES,default=3"`
	RetryInterval time.Duration `env:"RETRY_INTERVAL,default=2s"`
	RateLimit     struct {
		Burst int     `env:"BURST,default=0"`
		RPS   float64 `env:"RPS,default=20.0"`
	} `env:",prefix=RATE_LIMIT_"`
}

func (c HTTPClientConfig) ADDR() string {
	return fmt.Sprintf("%s://%s:%d", c.Scheme, c.Host, c.Port)
}

type LoggerConfig struct {
	Level string `env:"LEVEL,default=debug"`
}

type ObservabilityHTTPConfig struct {
	Host         string        `env:"HOST,default=127.0.0.1"`
	Port         uint16        `env:"PORT,default=8383"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT,default=30s"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT,default=30s"`
	IdleTimeout  time.Duration `env:"IDLE_TIMEOUT,default=1m"`
}

func (a ObservabilityHTTPConfig) ADDR() string {
	return fmt.Sprintf("%s:%d", a.Host, a.Port)
}

type SQLiteConfig struct {
	Path         string `env:"PATH,default=./data/kurut.db"`
	MaxOpenConns int    `env:"MAX_OPEN_CONNS,default=25"`
	MaxIdleConns int    `env:"MAX_IDLE_CONNS,default=5"`
	MaxLifetime  string `env:"MAX_LIFETIME,default=5m"`
}
