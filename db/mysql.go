// +build !no_mysql,!no_db

package db

import _ "github.com/jinzhu/gorm/dialects/mysql" // nolint: golint

func init() {
	enabledDrivers = append(enabledDrivers, "mysql")
}
