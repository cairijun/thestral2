package db

import (
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/stretchr/testify/suite"
)

type UsersTestSuite struct {
	suite.Suite

	tmpDir string
	dao    *UserDAO
}

func (s *UsersTestSuite) SetupTest() {
	var err error
	s.tmpDir, err = ioutil.TempDir("", "thestral2_UsersTestSuite")
	s.Require().NoError(err)

	s.Require().NoError(InitDB(Config{
		Driver: "sqlite3",
		DSN:    path.Join(s.tmpDir, "test.db"),
	}))
	s.dao, err = NewUserDAO()
	s.Require().NoError(err)
}

func (s *UsersTestSuite) TearDownTest() {
	_ = os.RemoveAll(s.tmpDir)
	s.NoError(s.dao.Close())
}

func (s *UsersTestSuite) TestAddGet() {
	s.Require().NoError(s.dao.Add(&User{Scope: "test1", Name: "user"}))
	s.Require().NoError(s.dao.Add(&User{Scope: "test1", Name: "user2"}))
	s.Require().NoError(s.dao.Add(&User{Scope: "test2", Name: "user"}))
	s.Require().Error(s.dao.Add(&User{Scope: "test1", Name: "user"}))

	var err error
	_, err = s.dao.Get("not", "exists")
	s.Error(err)

	u, err := s.dao.Get("test1", "user")
	s.Require().NoError(err)
	s.Equal("test1", u.Scope)
	s.Equal("user", u.Name)

	u, err = s.dao.Get("test1", "user2")
	s.Require().NoError(err)
	s.Equal("test1", u.Scope)
	s.Equal("user2", u.Name)

	u, err = s.dao.Get("test2", "user")
	s.Require().NoError(err)
	s.Equal("test2", u.Scope)
	s.Equal("user", u.Name)
}

func (s *UsersTestSuite) TestList() {
	for i := 0; i < 10; i++ {
		s.Require().NoError(
			s.dao.Add(&User{Scope: "s1", Name: strconv.Itoa(i)}))
	}
	for i := 0; i < 10; i++ {
		s.Require().NoError(
			s.dao.Add(&User{Scope: "s2", Name: strconv.Itoa(i)}))
	}

	users1, err := s.dao.List("s1")
	if s.NoError(err) && s.Len(users1, 10) {
		for i := 0; i < 10; i++ {
			s.Equal("s1", users1[i].Scope)
			s.Equal(strconv.Itoa(i), users1[i].Name)
		}
	}

	users2, err := s.dao.List("s2")
	if s.NoError(err) && s.Len(users2, 10) {
		for i := 0; i < 10; i++ {
			s.Equal("s2", users2[i].Scope)
			s.Equal(strconv.Itoa(i), users2[i].Name)
		}
	}

	users, err := s.dao.ListAll()
	if s.NoError(err) && s.Len(users, 20) {
		for i := 0; i < 10; i++ {
			s.Equal("s1", users[i].Scope)
			s.Equal(strconv.Itoa(i), users[i].Name)
		}
		for i := 0; i < 10; i++ {
			s.Equal("s2", users[i+10].Scope)
			s.Equal(strconv.Itoa(i), users[i+10].Name)
		}
	}

	users, err = s.dao.List("not_exists")
	s.NoError(err)
	s.Empty(users)
}

func (s *UsersTestSuite) TestDelete() {
	s.Require().NoError(s.dao.Add(&User{Scope: "test1", Name: "user"}))
	s.Require().NoError(s.dao.Add(&User{Scope: "test1", Name: "user2"}))
	s.Require().NoError(s.dao.Add(&User{Scope: "test2", Name: "user"}))

	s.NoError(s.dao.Delete("test1", "user"))
	s.NoError(s.dao.Delete("test2", "user"))
	s.Error(s.dao.Delete("not", "exists"))
	s.False(s.dao.CheckExists("test1", "user"))
	s.False(s.dao.CheckExists("test2", "user"))
	s.True(s.dao.CheckExists("test1", "user2"))
}

func (s *UsersTestSuite) TestUpdate() {
	s.Require().NoError(s.dao.Add(&User{Scope: "test", Name: "user"}))
	s.Require().False(s.dao.CheckPassword("test", "user", "password"))

	u, err := s.dao.Get("test", "user")
	s.Require().NoError(err)

	pwhash := HashUserPass("password")
	u.PWHash = &pwhash
	s.NoError(s.dao.Update(u))
	s.True(s.dao.CheckPassword("test", "user", "password"))
}

func (s *UsersTestSuite) TestCheckUser() {
	pwhash := HashUserPass("password")
	s.Require().NoError(s.dao.Add(&User{Scope: "nopass", Name: "user"}))
	s.Require().NoError(s.dao.Add(&User{
		Scope: "haspass", Name: "user",
		PWHash: &pwhash,
	}))

	s.True(s.dao.CheckExists("nopass", "user"))
	s.True(s.dao.CheckPassword("haspass", "user", "password"))
	s.False(s.dao.CheckExists("nopass", "not_exists"))
	s.False(s.dao.CheckPassword("haspass", "user", "wrong_pass"))
	s.False(s.dao.CheckPassword("nopass", "user", "password"))
	s.False(s.dao.CheckPassword("haspass", "not_exists", "password"))
}

func TestUsersTestSuite(t *testing.T) {
	if CheckDriver("sqlite3") {
		suite.Run(t, new(UsersTestSuite))
	} else {
		t.Skip("sqlite3 is not enabled")
	}
}

func BenchmarkHashUserPass(b *testing.B) {
	pass := "some pass word"
	for i := 0; i < b.N; i++ {
		_ = HashUserPass(pass)
	}
}
