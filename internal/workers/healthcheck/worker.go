package healthcheck

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"kurut-bot/internal/storage"
)

const (
	checkInterval = 30 * time.Second
	httpTimeout   = 10 * time.Second
)

type serverStatus struct {
	isUp         bool
	lastCheck    time.Time
	failureCount int
	messageIDs   map[int64]int
}

type Worker struct {
	storage   Storage
	telegram  TelegramNotifier
	adminIDs  []int64
	logger    *slog.Logger
	httpClient *http.Client
	
	statusMu sync.RWMutex
	statuses map[int64]*serverStatus
	
	stopCh chan struct{}
	doneCh chan struct{}
}

func NewWorker(
	storage Storage,
	telegram TelegramNotifier,
	adminIDs []int64,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		storage:  storage,
		telegram: telegram,
		adminIDs: adminIDs,
		logger:   logger,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		statuses: make(map[int64]*serverStatus),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (w *Worker) Name() string {
	return "healthcheck"
}

func (w *Worker) Start() error {
	w.logger.Info("Starting health check worker",
		"interval", checkInterval,
		"admin_count", len(w.adminIDs))
	
	go w.run()
	return nil
}

func (w *Worker) Stop() {
	w.logger.Info("Stopping health check worker")
	close(w.stopCh)
	<-w.doneCh
}

func (w *Worker) run() {
	defer close(w.doneCh)
	
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	
	ctx := context.Background()
	w.checkServers(ctx)
	
	for {
		select {
		case <-ticker.C:
			w.checkServers(ctx)
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) checkServers(ctx context.Context) {
	servers, err := w.storage.ListEnabledWGServers(ctx)
	if err != nil {
		w.logger.Error("Failed to list enabled WG servers", "error", err)
		return
	}
	
	w.logger.Debug("Checking health of WG servers", "count", len(servers))
	
	for _, server := range servers {
		w.checkServer(server)
	}
}

func (w *Worker) checkServer(server *storage.WGServer) {
	endpoint := server.Endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	if !strings.HasSuffix(endpoint, "/health") {
		if strings.HasSuffix(endpoint, "/") {
			endpoint += "health"
		} else {
			endpoint += "/health"
		}
	}
	
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		w.logger.Error("Failed to create health check request",
			"server", server.Name,
			"error", err)
		w.updateStatus(server, false)
		return
	}
	
	resp, err := w.httpClient.Do(req)
	isHealthy := err == nil && resp != nil && resp.StatusCode == http.StatusOK
	
	if resp != nil {
		resp.Body.Close()
	}
	
	if err != nil {
		w.logger.Warn("Health check failed",
			"server", server.Name,
			"endpoint", endpoint,
			"error", err)
	} else if !isHealthy {
		w.logger.Warn("Health check returned non-OK status",
			"server", server.Name,
			"endpoint", endpoint,
			"status", resp.StatusCode)
	} else {
		w.logger.Debug("Health check passed",
			"server", server.Name,
			"endpoint", endpoint)
	}
	
	w.updateStatus(server, isHealthy)
}

func (w *Worker) updateStatus(server *storage.WGServer, isUp bool) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	
	prevStatus, exists := w.statuses[server.ID]
	now := time.Now()
	
	if !exists {
		w.statuses[server.ID] = &serverStatus{
			isUp:       isUp,
			lastCheck:  now,
			messageIDs: make(map[int64]int),
		}
		if !isUp {
			w.notifyServerDown(server, 1)
		}
		return
	}
	
	if prevStatus.isUp && !isUp {
		prevStatus.isUp = false
		prevStatus.failureCount = 1
		prevStatus.lastCheck = now
		w.notifyServerDown(server, 1)
	} else if !prevStatus.isUp && !isUp {
		prevStatus.failureCount++
		prevStatus.lastCheck = now
		w.updateServerStillDown(server, prevStatus)
	} else if !prevStatus.isUp && isUp {
		downtime := now.Sub(prevStatus.lastCheck)
		prevStatus.isUp = true
		prevStatus.failureCount = 0
		prevStatus.lastCheck = now
		w.notifyServerRecovered(server, downtime)
	} else {
		prevStatus.lastCheck = now
	}
}

func (w *Worker) notifyServerDown(server *storage.WGServer, failureCount int) {
	message := fmt.Sprintf(
		"🚨 *WG Server Down*\n\n"+
			"Server: `%s`\n"+
			"Endpoint: `%s`\n"+
			"Status: ❌ *FAILED*\n"+
			"Failed checks: `%d`\n"+
			"Time: `%s`",
		escapeMarkdownV2(server.Name),
		escapeMarkdownV2(server.Endpoint),
		failureCount,
		escapeMarkdownV2(time.Now().Format("2006-01-02 15:04:05")),
	)
	
	w.sendToAdminsAndSaveMessageID(server.ID, message)
}

func (w *Worker) updateServerStillDown(server *storage.WGServer, status *serverStatus) {
	message := fmt.Sprintf(
		"🚨 *WG Server Down*\n\n"+
			"Server: `%s`\n"+
			"Endpoint: `%s`\n"+
			"Status: ❌ *FAILED*\n"+
			"Failed checks: `%d`\n"+
			"Time: `%s`",
		escapeMarkdownV2(server.Name),
		escapeMarkdownV2(server.Endpoint),
		status.failureCount,
		escapeMarkdownV2(time.Now().Format("2006-01-02 15:04:05")),
	)
	
	w.updateAdminMessages(server.ID, status.messageIDs, message)
}

func (w *Worker) notifyServerRecovered(server *storage.WGServer, downtime time.Duration) {
	w.statusMu.Lock()
	status := w.statuses[server.ID]
	messageIDs := status.messageIDs
	status.messageIDs = make(map[int64]int)
	w.statusMu.Unlock()
	
	message := fmt.Sprintf(
		"✅ *WG Server Recovered*\n\n"+
			"Server: `%s`\n"+
			"Endpoint: `%s`\n"+
			"Status: ✅ *OK*\n"+
			"Downtime: `%s`\n"+
			"Time: `%s`",
		escapeMarkdownV2(server.Name),
		escapeMarkdownV2(server.Endpoint),
		escapeMarkdownV2(formatDuration(downtime)),
		escapeMarkdownV2(time.Now().Format("2006-01-02 15:04:05")),
	)
	
	w.updateAdminMessages(server.ID, messageIDs, message)
}

func (w *Worker) sendToAdminsAndSaveMessageID(serverID int64, message string) {
	for _, adminID := range w.adminIDs {
		if err := w.telegram.SendMessage(adminID, message); err != nil {
			w.logger.Error("Failed to send notification to admin",
				"admin_id", adminID,
				"error", err)
		}
	}
}

func (w *Worker) updateAdminMessages(serverID int64, messageIDs map[int64]int, message string) {
	for adminID, messageID := range messageIDs {
		if err := w.telegram.EditMessage(adminID, messageID, message); err != nil {
			w.logger.Warn("Failed to edit notification for admin, sending new",
				"admin_id", adminID,
				"message_id", messageID,
				"error", err)
			if err := w.telegram.SendMessage(adminID, message); err != nil {
				w.logger.Error("Failed to send notification to admin",
					"admin_id", adminID,
					"error", err)
			}
		}
	}
}

func escapeMarkdownV2(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d sec", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d min %d sec", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}

