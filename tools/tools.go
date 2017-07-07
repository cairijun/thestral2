package tools

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

var allTools = []Tool{}

type Tool interface {
	Name() string
	Description() string
	Run(args []string)
}

func Init() {
	sort.SliceStable(allTools, func(i, j int) bool {
		return strings.Compare(allTools[i].Name(), allTools[j].Name()) < 0
	})
}

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

func PrintUsage() {
	_, _ = fmt.Fprintf(os.Stderr, "Available tools:\n")
	for _, t := range allTools {
		_, _ = fmt.Fprintf(
			os.Stderr, "  %s\n    %s\n", t.Name(), t.Description())
	}
}
