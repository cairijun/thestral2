package tools

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/pkg/errors"
	"github.com/richardtsai/thestral2/db"
	"github.com/richardtsai/thestral2/lib"
)

func init() {
	allTools = append(allTools, &usersTool{})
}

type usersTool struct {
	consoleTool
	dao *db.UserDAO
}

func (usersTool) Name() string {
	return "users"
}

func (usersTool) Description() string {
	return "Manage user database"
}

func (t *usersTool) Run(args []string) {
	fs := flag.NewFlagSet("users", flag.ExitOnError)
	configFile := fs.String("c", "", "thestral2 configuration file")
	if fs.Parse(args[1:]) == flag.ErrHelp {
		fs.Usage()
	} else if *configFile == "" {
		_, _ = fmt.Fprintf(
			os.Stderr, "Error: a configuration file is needed\n\n")
		fs.Usage()
		os.Exit(0)
	}

	config, err := lib.ParseConfigFile(*configFile)
	if err != nil {
		panic(err)
	} else if config.DB == nil {
		panic("'db' is not specified in the configuration file")
	} else if err = db.InitDB(*config.DB); err != nil {
		panic(err)
	} else if t.dao, err = db.NewUserDAO(); err != nil {
		panic(err)
	}

	if err := t.setupConsole("users> "); err != nil {
		panic(err)
	}
	defer t.teardownConsole()
	t.addCmd("add", "add SCOPE/NAME", t.addUser)
	t.addCmd("delete", "delete SCOPE/NAME", t.deleteUser)
	t.addCmd("list", "list [SCOPE]", t.listUsers)
	t.addCmd("passwd", "passwd SCOPE/NAME", t.changePasswd)
	t.runLoop()
}

func (t *usersTool) addUser(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(term, "exactly one argument is required")
		return true
	}

	us := userSpec{}
	if err := us.FromString(args[0]); err != nil {
		_, _ = fmt.Fprintf(term, "invalid user '%s': %s\n", args[0], err)
		return true
	}

	u := db.User{Scope: us.Scope, Name: us.Name}
	if pw, err := term.ReadPassword("Password (optional): "); err != nil {
		_, _ = fmt.Fprintf(term, "failed to read password: %s\n", err)
		return true
	} else if len(pw) > 0 {
		hash := db.HashUserPass(pw)
		u.PWHash = &hash
	}

	if err := t.dao.Add(&u); err != nil {
		_, _ = fmt.Fprintf(term, "failed to add user '%s': %v\n", us, err)
	} else {
		_, _ = fmt.Fprintf(term, "user '%s' added\n", us)
	}
	return true
}

func (t *usersTool) deleteUser(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(term, "exactly one argument is required")
		return true
	}

	us := userSpec{}
	if err := us.FromString(args[0]); err != nil {
		_, _ = fmt.Fprintf(term, "invalid user '%s': %s\n", args[0], err)
		return true
	}

	if err := t.dao.Delete(us.Scope, us.Name); err != nil {
		_, _ = fmt.Fprintf(term, "failed to delete user '%s': %v\n", us, err)
	} else {
		_, _ = fmt.Fprintf(term, "user '%s' deleted\n", us)
	}
	return true
}

func (t *usersTool) listUsers(term *terminal.Terminal, args []string) bool {
	var users []*db.User
	var err error
	switch len(args) {
	case 0:
		users, err = t.dao.ListAll()
	case 1:
		users, err = t.dao.List(args[0])
	default:
		_, _ = fmt.Fprintln(term, "no more than one argument is accepted")
		return true
	}

	if err != nil {
		_, _ = fmt.Fprintf(term, "failed to list users: %v\n", err)
		return true
	}
	w := tabwriter.NewWriter(term, 4, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tScope\tName\tPassword\tCreated At")
	for _, user := range users {
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%t\t%s\n",
			user.ID, user.Scope, user.Name, user.PWHash != nil,
			user.CreatedAt.Format(time.RFC822))
	}
	_ = w.Flush()
	return true
}

func (t *usersTool) changePasswd(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(term, "exactly one argument is required")
		return true
	}

	us := userSpec{}
	if err := us.FromString(args[0]); err != nil {
		_, _ = fmt.Fprintf(term, "invalid user '%s': %s\n", args[0], err)
		return true
	}

	u, err := t.dao.Get(us.Scope, us.Name)
	if err != nil {
		_, _ = fmt.Fprintf(term, "failed to get user '%s': %v\n", us, err)
		return true
	}

	pw, err := term.ReadPassword("Password: ")
	if err != nil {
		_, _ = fmt.Fprintf(term, "failed to read password: %s\n", err)
		return true
	} else if pw == "" {
		_, _ = fmt.Fprintf(term, "a valid password is required\n")
		return true
	}

	pwhash := db.HashUserPass(pw)
	u.PWHash = &pwhash
	if err = t.dao.Update(u); err != nil {
		_, _ = fmt.Fprintf(
			term, "failed to change password for '%s': %v\n", us, err)
	} else {
		_, _ = fmt.Fprintln(term, "password changed")
	}
	return true
}

type userSpec struct {
	Scope string
	Name  string
}

func (u userSpec) String() string {
	return fmt.Sprintf("%s/%s", u.Scope, u.Name)
}

func (u *userSpec) FromString(s string) error {
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("user specification should be 'scope/name'")
	}
	u.Scope = parts[0]
	u.Name = parts[1]
	return nil
}
