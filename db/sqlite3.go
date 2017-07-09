// +build !no_sqlite3,!no_db

package db

import _ "github.com/jinzhu/gorm/dialects/sqlite" // nolint: golint

func init() {
	EnabledDrivers = append(EnabledDrivers, "sqlite3")
}
