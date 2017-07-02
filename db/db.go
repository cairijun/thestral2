package db

import (
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

var (
	enabledDrivers []string
	dbConfig       *Config
)

// Config contains configuration about how to connect to the database.
type Config struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// InitDB initializes the database for later use.
func InitDB(config Config) error {
	if checkDriver(config.Driver) {
		dbConfig = &config
		db, err := getDB()
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&User{}).Error // create tables when necessary
		return errors.Wrap(err, "failed to initialize database")
	}
	return errors.Errorf(
		"driver '%s' is not supported or not enabled", config.Driver)
}

func checkDriver(driver string) bool {
	for _, d := range enabledDrivers {
		if driver == d {
			return true
		}
	}
	return false
}

func getDB() (*gorm.DB, error) {
	if dbConfig == nil {
		panic("database configuration not set")
	}
	db, err := gorm.Open(dbConfig.Driver, dbConfig.DSN)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open database")
	}
	// logging is not needed as all errors are reported
	db.LogMode(false)
	return db, nil
}
