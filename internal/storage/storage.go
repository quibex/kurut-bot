package storage

import (
	"reflect"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jmoiron/sqlx"
)

type storageImpl struct {
	db  *sqlx.DB
	now func() time.Time
}

func New(db *sqlx.DB) *storageImpl {
	return &storageImpl{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *storageImpl) stmpBuilder() sq.StatementBuilderType {
	return sq.StatementBuilder.PlaceholderFormat(sq.Question)
}

// moscowLocation is Moscow timezone (UTC+3)
var moscowLocation = time.FixedZone("MSK", 3*60*60)

// todayStart returns the start of today (00:00:00) in Moscow time, converted to UTC
// Used for stable daily queries - lists don't change throughout the day
// Returns UTC to ensure consistent database comparisons (database stores times in UTC)
func (s *storageImpl) todayStart() time.Time {
	nowMoscow := s.now().In(moscowLocation)
	startOfDayMoscow := time.Date(nowMoscow.Year(), nowMoscow.Month(), nowMoscow.Day(), 0, 0, 0, 0, moscowLocation)
	return startOfDayMoscow.UTC()
}

// Fields возвращает список всех полей структуры, которые есть в БД.
func fields(data any) string {
	var s string
	r := reflect.TypeOf(data)
	for i := 0; i < r.NumField(); i++ {
		tag := r.Field(i).Tag.Get("db")
		if tag != "" {
			s += tag + ","
		}
	}
	return s[:len(s)-1]
}

// prefixWithTable добавляет префикс таблицы к полям - пока не используется, но пригодится для JOIN запросов
// func prefixWithTable(prefix string, fields string) string {
// 	strs := strings.Split(fields, ",")
//
// 	var strBuilder strings.Builder
// 	strBuilder.Grow(len(fields) + len(strs)*(len(prefix)+1))
// 	for i := 0; i < len(strs); i++ {
// 		strBuilder.WriteString(fmt.Sprintf("%s.%s,", prefix, strs[i]))
// 	}
// 	s := strBuilder.String()
// 	return s[:len(s)-1]
// }
