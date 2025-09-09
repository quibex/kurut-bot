package telegram

import (
	"context"
	"fmt"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/time/rate"
)

type Client struct {
	api     *tgbotapi.BotAPI
	logger  *slog.Logger
	limiter *rate.Limiter
	updates <-chan tgbotapi.Update
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewClient(token string, logger *slog.Logger) (*Client, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("создание telegram бота: %w", err)
	}

	// Rate limiting - 30 сообщений в секунду
	limiter := rate.NewLimiter(30, 1)

	return &Client{
		api:     bot,
		logger:  logger,
		limiter: limiter,
	}, nil
}

// Start начинает получение обновлений (long polling)
func (c *Client) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updateChan := c.api.GetUpdatesChan(u)
	c.updates = updateChan

	c.logger.Info("Telegram бот запущен")
	return nil
}

// Stop останавливает получение обновлений
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.api.StopReceivingUpdates()
	c.logger.Info("Telegram бот остановлен")
}

// GetUpdates возвращает канал с обновлениями
func (c *Client) GetUpdates() <-chan tgbotapi.Update {
	return c.updates
}

// SendMessage отправляет сообщение с rate limiting
func (c *Client) SendMessage(chatID int64, text string) error {
	if err := c.limiter.Wait(c.ctx); err != nil {
		return fmt.Errorf("rate limiting: %w", err)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	_, err := c.api.Send(msg)
	if err != nil {
		c.logger.Error("ошибка отправки сообщения",
			slog.Int64("chat_id", chatID),
			slog.String("error", err.Error()))
		return fmt.Errorf("отправка сообщения: %w", err)
	}

	return nil
}

// SendKeyboard отправляет сообщение с клавиатурой
func (c *Client) SendKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) error {
	if err := c.limiter.Wait(c.ctx); err != nil {
		return fmt.Errorf("rate limiting: %w", err)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard

	_, err := c.api.Send(msg)
	return err
}

// Send отправляет любое сообщение с rate limiting (для интерфейса botApi)
func (c *Client) Send(chattable tgbotapi.Chattable) (tgbotapi.Message, error) {
	if err := c.limiter.Wait(c.ctx); err != nil {
		return tgbotapi.Message{}, fmt.Errorf("rate limiting: %w", err)
	}

	message, err := c.api.Send(chattable)
	if err != nil {
		c.logger.Error("ошибка отправки", slog.Any("error", err))
		return tgbotapi.Message{}, fmt.Errorf("отправка: %w", err)
	}

	return message, nil
}

// Request отправляет запрос к API (для интерфейса botApi)
func (c *Client) Request(chattable tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	if err := c.limiter.Wait(c.ctx); err != nil {
		return nil, fmt.Errorf("rate limiting: %w", err)
	}

	resp, err := c.api.Request(chattable)
	if err != nil {
		c.logger.Error("ошибка запроса к API", slog.Any("error", err))
		return nil, fmt.Errorf("запрос к API: %w", err)
	}

	return resp, nil
}

// GetBotAPI возвращает внутренний BotAPI объект
func (c *Client) GetBotAPI() *tgbotapi.BotAPI {
	return c.api
}
