package tools

import (
	"fmt"

	"github.com/richardtsai/thestral2/lib"
)

func init() {
	allTools = append(allTools, versionTool{})
}

type versionTool struct{}

func (versionTool) Name() string {
	return "version"
}

func (versionTool) Description() string {
	return "Print version information"
}

func (versionTool) Run(args []string) {
	fmt.Printf("Thestral 2\nVersion: %s\nBuilt on: %s\n",
		lib.ThestralVersion, lib.ThestralBuiltTime)
}
