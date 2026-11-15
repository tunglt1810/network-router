package daemon

import (
	"log"
	"os"
	"sync"
	"time"
)

const logPath = "/var/log/network-router.log"

// LogManager truncates the daemon log every day at 10:00. It truncates in place
// instead of rotating: launchd keeps the file descriptor open, so after a
// rename the daemon would keep writing to the renamed file.
type LogManager struct {
	mu    sync.Mutex
	timer *time.Timer
}

func NewLogManager() *LogManager {
	return &LogManager{}
}

func (m *LogManager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		return
	}
	m.scheduleLocked()
	log.Println("LogManager started (Log cleanup scheduled at 10:00 AM)")
}

func (m *LogManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

func (m *LogManager) scheduleLocked() {
	m.timer = time.AfterFunc(time.Until(nextCleanup(time.Now())), m.truncate)
}

func (m *LogManager) truncate() {
	log.Printf("⏰ Scheduled log cleanup: Truncating %s", logPath)
	if err := os.Truncate(logPath, 0); err != nil {
		log.Printf("Error truncating log file: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.scheduleLocked()
	}
}

func nextCleanup(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
