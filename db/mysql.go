// +build !minimal full mysql

package db

import _ "github.com/jinzhu/gorm/dialects/mysql" // nolint: golint

func init() {
	EnabledDrivers = append(EnabledDrivers, "mysql")
}
