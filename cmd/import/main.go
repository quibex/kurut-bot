package main

import (
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	durationToTariffID = map[int]int64{
		1:  6, // 1м -> "новый 1м"
		2:  7, // 2м -> "новый 2м"
		3:  8, // 3м -> "новый 3м"
		6:  9, // 6м -> "новый 6м"
		12: 5, // 1г/12м -> "1 год" (архивный)
	}

	skipTariffs = map[string]bool{
		"отключен":     true,
		"фри":          true,
		"не пробный":   true,
		"пробный7дней": true,
	}
)

type subscription struct {
	whatsApp    string
	tariffID    int64
	serverID    int64
	activatedAt time.Time
	expiresAt   time.Time
}

func main() {
	dbPath := flag.String("db", "./kurut.db", "path to SQLite database")
	csvDir := flag.String("csv", "./subs/", "path to directory with CSV files")
	telegramID := flag.Int64("tg", 0, "Telegram ID for created_by_telegram_id")
	dryRun := flag.Bool("dry-run", false, "show what would be imported without writing to DB")
	flag.Parse()

	if *telegramID == 0 {
		log.Fatal("telegram ID is required: -tg <your_telegram_id>")
	}

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	serverMap, err := loadServers(db)
	if err != nil {
		log.Fatalf("failed to load servers: %v", err)
	}
	fmt.Printf("Loaded %d servers: %v\n", len(serverMap), serverMap)

	csvFiles := []string{"wg1.csv", "wg2.csv", "wg3.csv", "wg4.csv"}

	var totalImported, totalSkipped, totalErrors int

	for _, csvFile := range csvFiles {
		serverName := strings.TrimSuffix(csvFile, ".csv")
		serverID, ok := serverMap[serverName]
		if !ok {
			fmt.Printf("WARN: server %s not found in DB, skipping file %s\n", serverName, csvFile)
			continue
		}

		filePath := filepath.Join(*csvDir, csvFile)
		imported, skipped, errors := processCSV(db, filePath, serverID, *telegramID, *dryRun)

		fmt.Printf("\n[%s] Imported: %d, Skipped: %d, Errors: %d\n", csvFile, imported, skipped, errors)

		totalImported += imported
		totalSkipped += skipped
		totalErrors += errors
	}

	fmt.Printf("\n=== TOTAL ===\n")
	fmt.Printf("Imported: %d\n", totalImported)
	fmt.Printf("Skipped: %d\n", totalSkipped)
	fmt.Printf("Errors: %d\n", totalErrors)

	if *dryRun {
		fmt.Println("\n(DRY RUN - nothing was written to database)")
	}
}

func loadServers(db *sql.DB) (map[string]int64, error) {
	rows, err := db.Query("SELECT id, name FROM servers WHERE archived = 0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := make(map[string]int64)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		servers[name] = id
	}
	return servers, rows.Err()
}

func processCSV(db *sql.DB, filePath string, serverID int64, telegramID int64, dryRun bool) (imported, skipped, errors int) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("failed to open %s: %v", filePath, err)
		return 0, 0, 1
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		log.Printf("failed to read %s: %v", filePath, err)
		return 0, 0, 1
	}

	for i, record := range records {
		if i == 0 {
			continue
		}

		if len(record) < 5 {
			skipped++
			continue
		}

		whatsApp := cleanWhatsApp(record[1])
		tariff := strings.TrimSpace(record[2])
		activatedAtStr := strings.TrimSpace(record[3])
		expiresAtStr := strings.TrimSpace(record[4])

		if whatsApp == "" || whatsApp == "0" {
			skipped++
			continue
		}

		if skipTariffs[tariff] {
			skipped++
			continue
		}

		duration := parseDuration(tariff)
		if duration == 0 {
			fmt.Printf("  SKIP row %d: unknown tariff '%s'\n", i+1, tariff)
			skipped++
			continue
		}

		tariffID, ok := durationToTariffID[duration]
		if !ok {
			fmt.Printf("  SKIP row %d: no tariff mapping for %d months\n", i+1, duration)
			skipped++
			continue
		}

		activatedAt, err := parseDate(activatedAtStr)
		if err != nil {
			fmt.Printf("  SKIP row %d: invalid activated_at '%s': %v\n", i+1, activatedAtStr, err)
			skipped++
			continue
		}

		expiresAt, err := parseDate(expiresAtStr)
		if err != nil {
			fmt.Printf("  SKIP row %d: invalid expires_at '%s': %v\n", i+1, expiresAtStr, err)
			skipped++
			continue
		}

		sub := subscription{
			whatsApp:    whatsApp,
			tariffID:    tariffID,
			serverID:    serverID,
			activatedAt: activatedAt,
			expiresAt:   expiresAt,
		}

		if dryRun {
			fmt.Printf("  DRY: %s, tariff=%d, server=%d, expires=%s\n",
				whatsApp, tariffID, serverID, expiresAt.Format("02.01.2006"))
			imported++
			continue
		}

		subID, err := insertSubscription(db, sub, telegramID)
		if err != nil {
			fmt.Printf("  ERROR row %d: %v\n", i+1, err)
			errors++
			continue
		}

		generatedUserID := generateUserID(subID, telegramID, whatsApp)
		if err := updateGeneratedUserID(db, subID, generatedUserID); err != nil {
			fmt.Printf("  ERROR updating generated_user_id for %d: %v\n", subID, err)
		}

		imported++
	}

	return imported, skipped, errors
}

func cleanWhatsApp(raw string) string {
	re := regexp.MustCompile(`[^\d.eE+-]`)
	cleaned := re.ReplaceAllString(raw, "")

	if strings.Contains(strings.ToLower(cleaned), "e") {
		f, err := strconv.ParseFloat(cleaned, 64)
		if err == nil {
			cleaned = fmt.Sprintf("%.0f", f)
		}
	}

	cleaned = regexp.MustCompile(`[^\d]`).ReplaceAllString(cleaned, "")

	return cleaned
}

func parseDuration(tariff string) int {
	tariff = strings.ToLower(tariff)

	if strings.Contains(tariff, "1г") || strings.Contains(tariff, "12м") || strings.Contains(tariff, "12m") {
		return 12
	}
	if strings.Contains(tariff, "6м") || strings.Contains(tariff, "6m") {
		return 6
	}
	if strings.Contains(tariff, "3м") || strings.Contains(tariff, "3m") {
		return 3
	}
	if strings.Contains(tariff, "2м") || strings.Contains(tariff, "2m") {
		return 2
	}
	if strings.Contains(tariff, "1м") || strings.Contains(tariff, "1m") {
		return 1
	}

	return 0
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}

	formats := []string{
		"02.01.2006",
		"2.1.2006",
		"02.1.2006",
		"2.01.2006",
	}

	for _, format := range formats {
		t, err := time.Parse(format, s)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse date: %s", s)
}

func insertSubscription(db *sql.DB, sub subscription, telegramID int64) (int64, error) {
	now := time.Now()

	result, err := db.Exec(`
		INSERT INTO subscriptions (
			user_id, tariff_id, server_id, status, client_whatsapp,
			created_by_telegram_id, activated_at, expires_at,
			last_renewed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		telegramID,
		sub.tariffID,
		sub.serverID,
		"active",
		sub.whatsApp,
		telegramID,
		sub.activatedAt,
		sub.expiresAt,
		now,
		now,
		now,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

func updateGeneratedUserID(db *sql.DB, subID int64, generatedUserID string) error {
	_, err := db.Exec("UPDATE subscriptions SET generated_user_id = ? WHERE id = ?", generatedUserID, subID)
	return err
}

func generateUserID(subscriptionID int64, assistantTelegramID int64, clientWhatsApp string) string {
	tgIDStr := fmt.Sprintf("%d", assistantTelegramID)
	tgSuffix := tgIDStr
	if len(tgIDStr) > 3 {
		tgSuffix = tgIDStr[len(tgIDStr)-3:]
	}

	re := regexp.MustCompile(`\d`)
	digits := re.FindAllString(clientWhatsApp, -1)
	phoneDigits := ""
	for _, d := range digits {
		phoneDigits += d
	}

	phoneSuffix := phoneDigits
	if len(phoneDigits) > 4 {
		phoneSuffix = phoneDigits[len(phoneDigits)-4:]
	}

	return fmt.Sprintf("%d_%s_%s", subscriptionID, tgSuffix, phoneSuffix)
}
