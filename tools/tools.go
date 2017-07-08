package tools

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
)

var allTools = []Tool{}

// Tool is the interface for auxiliary utilities that can be run as
// sub-commands.
type Tool interface {
	Name() string
	Description() string
	Run(args []string)
}

// Init initializes the tool facility.
func Init() {
	sort.SliceStable(allTools, func(i, j int) bool {
		return strings.Compare(allTools[i].Name(), allTools[j].Name()) < 0
	})
}

// Run executes a tool of the given name.
func Run(name string, args []string) {
	for _, t := range allTools {
		if t.Name() == name {
			t.Run(args)
			return
		}
	}
	_, _ = fmt.Fprintf(os.Stderr, "'%s' not found\n\n", name)
	PrintUsage()
}

// PrintUsage prints the tool help text to stderr.
func PrintUsage() {
	_, _ = fmt.Fprintf(os.Stderr, "Available tools:\n")
	for _, t := range allTools {
		_, _ = fmt.Fprintf(
			os.Stderr, "  %s\n    %s\n", t.Name(), t.Description())
	}
}

// consoleTool provides basic facilities for building a console tool
// with multiple sub-commands.
type consoleTool struct {
	prompt  string
	console *stdConsole
	term    *terminal.Terminal
	cmds    []consoleToolCmd
}

type consoleToolFunc func(term *terminal.Terminal, args []string) (cont bool)

type consoleToolCmd struct {
	name  string
	usage string
	f     consoleToolFunc
}

func (t *consoleTool) setupConsole(prompt string) error {
	var err error
	if t.console, err = getStdConsole(); err != nil {
		return err
	}
	t.prompt = prompt
	t.term = terminal.NewTerminal(t.console, prompt)
	return nil
}

func (t *consoleTool) teardownConsole() {
	if t.console != nil {
		_ = t.console.Close()
	}
	t.console = nil
	t.term = nil
}

func (t *consoleTool) addCmd(name, usage string, f consoleToolFunc) {
	// t.cmds is a slice rather than a map, so that we can preserve the order.
	t.cmds = append(t.cmds, consoleToolCmd{name: name, usage: usage, f: f})
}

func (t *consoleTool) printCmdUsage() {
	t.term.SetPrompt("")
	defer t.term.SetPrompt(t.prompt)
	_, _ = fmt.Fprintln(t.term, "Available cmds:")
	hasQuit := false
	for _, cmd := range t.cmds {
		if cmd.name == "quit" {
			hasQuit = true
		}
		_, _ = fmt.Fprintf(t.term, "  %s\n", cmd.usage)
	}
	if !hasQuit {
		_, _ = fmt.Fprintln(t.term, "  quit")
	}
}

func (t *consoleTool) runLoop() {
cmdLoop:
	for {
		line, err := t.term.ReadLine()
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}
		tokens := strings.Fields(line)
		if len(tokens) == 0 { // empty input
			continue
		}
		for _, cmd := range t.cmds {
			if cmd.name == tokens[0] {
				t.term.SetPrompt("")
				cont := cmd.f(t.term, tokens[1:])
				t.term.SetPrompt(t.prompt)
				if cont { // cmd wants to continue
					continue cmdLoop
				}
				break cmdLoop
			}
		}
		if tokens[0] == "quit" {
			break
		}
		t.printCmdUsage() // cmd not found
	}
}

// stdConsole is a wrapper around io.Stdin and os.Stdout. It sets the stdin to
// raw mode on creation, and reset on Close.
type stdConsole struct {
	oldState *terminal.State
}

func getStdConsole() (*stdConsole, error) {
	s, err := terminal.MakeRaw(syscall.Stdin)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &stdConsole{s}, nil
}

func (*stdConsole) Read(p []byte) (int, error) {
	return os.Stdin.Read(p)
}

func (*stdConsole) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
}

func (c *stdConsole) Close() error {
	return terminal.Restore(syscall.Stdin, c.oldState)
}
