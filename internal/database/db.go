package database

import (
	"log"
	"time"

	"SysDashServer/internal/auth"
	"SysDashServer/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	*gorm.DB
}

func InitDB(dbPath string) (*DB, error) {
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	// Auto-migrate tables
	err = gormDB.AutoMigrate(
		&models.User{},
		&models.MetricRecord{},
		&models.AlertThreshold{},
		&models.AlertLog{},
	)
	if err != nil {
		return nil, err
	}

	db := &DB{gormDB}
	db.seedInitialData()

	return db, nil
}

func (db *DB) seedInitialData() {
	// Seed default Admin if no user exists
	var count int64
	db.Model(&models.User{}).Count(&count)
	if count == 0 {
		hashedPassword, err := auth.HashPassword("admin123")
		if err == nil {
			admin := models.User{
				Username: "admin",
				Password: hashedPassword,
				FullName: "Administrator",
				Role:     models.RoleAdmin,
			}
			if err := db.Create(&admin).Error; err == nil {
				log.Println("[Database] Seeded initial admin account: username='admin', password='admin123'")
			}
		}
	}

	// Seed default Alert Thresholds if none exist
	var alertCount int64
	db.Model(&models.AlertThreshold{}).Count(&alertCount)
	if alertCount == 0 {
		defaultAlert := models.AlertThreshold{
			CPUPercent:  90.0,
			MemPercent:  90.0,
			DiskPercent: 90.0,
			Enabled:     true,
			UpdatedAt:   time.Now(),
		}
		db.Create(&defaultAlert)
		log.Println("[Database] Seeded default alert thresholds: CPU 90%, RAM 90%, Disk 90%")
	}
}
