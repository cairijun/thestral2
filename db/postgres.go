// +build !no_postgres,!no_db

package db

import _ "github.com/jinzhu/gorm/dialects/postgres" // nolint: golint

func init() {
	EnabledDrivers = append(EnabledDrivers, "postgres")
}
