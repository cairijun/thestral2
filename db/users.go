package db

import (
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

const pwhashCost = 10

// HashUserPass returns the hash bytes of the password for password storage.
func HashUserPass(password string) []byte {
	result, err := bcrypt.GenerateFromPassword([]byte(password), pwhashCost)
	if err != nil {
		panic("failed to generate pwhash: " + err.Error())
	}
	return result
}

// User contains the information of a user. It is stored in the database
// as table `users`.
type User struct {
	gorm.Model
	Scope  string `gorm:"unique_index:idx_scope_name"`
	Name   string `gorm:"unique_index:idx_scope_name"`
	PWHash *[]byte
}

// UserDAO is the DAO for User.
type UserDAO struct {
	db *gorm.DB
}

// NewUserDAO creates a UserDAO.
func NewUserDAO() (*UserDAO, error) {
	db, err := getDB()
	if err != nil {
		return nil, err
	}
	return &UserDAO{db}, nil
}

// Close the db connection of this DAO.
func (d *UserDAO) Close() error {
	return errors.WithStack(d.db.Close())
}

// Add a new user in the database.
func (d *UserDAO) Add(user *User) error {
	if err := d.db.Create(user).Error; err != nil {
		return errors.Wrap(err, "failed to add new user")
	}
	return nil
}

// Delete a user of the given scope and name.
func (d *UserDAO) Delete(scope, name string) error {
	q := d.db.Delete(&User{}, "scope = ? AND name = ?", scope, name)
	if q.Error != nil {
		return errors.Wrapf(
			q.Error, "failed to delete user '%s/%s'", scope, name)
	}
	if q.RowsAffected == 0 {
		return errors.New("user not found")
	}
	return nil
}

// Update saves the user to the database.
func (d *UserDAO) Update(user *User) error {
	if q := d.db.Save(user); q.Error != nil {
		return errors.Wrap(q.Error, "failed to update user")
	}
	return nil
}

// Get the user of the given scope and name.
func (d *UserDAO) Get(scope, name string) (*User, error) {
	u := User{}
	query := d.db.Where("scope = ? AND name = ?", scope, name).First(&u)
	if query.Error != nil {
		if query.RecordNotFound() {
			return nil, errors.Errorf("user '%s/%s' not found", scope, name)
		}
		return nil, errors.Wrap(query.Error, "error occurred when querying db")
	}
	return &u, nil
}

// List returns an ordered list of all the users in a scope.
func (d *UserDAO) List(scope string) ([]*User, error) {
	results := []*User{}
	query := d.db.Where("scope = ?", scope).Order("name").Find(&results)
	if query.Error != nil {
		if query.RecordNotFound() {
			return nil, errors.Errorf("scope '%s' not found", scope)
		}
		return nil, errors.Wrap(query.Error, "error occurred when querying db")
	}
	return results, nil
}

// ListAll returns an ordered list of all the users.
func (d *UserDAO) ListAll() ([]*User, error) {
	results := []*User{}
	query := d.db.Order("scope, name").Find(&results)
	if query.Error != nil {
		return nil, errors.Wrap(query.Error, "error occurred when querying db")
	}
	return results, nil
}

// CheckExists return a boolean value indicating the existence of the user.
func (d *UserDAO) CheckExists(scope, name string) bool {
	_, err := d.Get(scope, name)
	return err == nil
}

// CheckPassword checks if the given password is correct for the user.
func (d *UserDAO) CheckPassword(scope, name, password string) bool {
	u, err := d.Get(scope, name)
	if err != nil || u.PWHash == nil {
		return false
	}
	err = bcrypt.CompareHashAndPassword(*u.PWHash, []byte(password))
	return err == nil
}
