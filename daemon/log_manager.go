package daemon

import (
	"log"
	"os"

	"github.com/robfig/cron/v3"
)

// LogManager handles log rotation and cleanup.
// It replaces the log rotation logic that was previously coupled in Monitor.
type LogManager struct {
	cron *cron.Cron
}

func NewLogManager() *LogManager {
	return &LogManager{}
}

func (m *LogManager) Start() {
	if m.cron != nil {
		return
	}
	m.cron = cron.New()
	_, err := m.cron.AddFunc("0 10 * * *", func() {
		log.Println("⏰ Scheduled log cleanup: Truncating /var/log/network-router.log")
		if err := os.Truncate("/var/log/network-router.log", 0); err != nil {
			log.Printf("Error truncating log file: %v", err)
		}
	})
	if err != nil {
		log.Printf("Error scheduling log cleanup cron: %v", err)
		return
	}
	m.cron.Start()
	log.Println("LogManager started (Log cleanup scheduled at 10:00 AM)")
}

func (m *LogManager) Stop() {
	if m.cron != nil {
		log.Println("Stopping LogManager...")
		m.cron.Stop()
		m.cron = nil
	}
}
