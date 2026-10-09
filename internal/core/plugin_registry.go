package core

import (
	"context"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry"
)

// The Knowledge base plugin registry (ADR-0012) says which program or URL
// each plugin's name stands for on this computer. The core only reads it,
// through internal/pluginregistry. What registers and removes a plugin is in
// a package of its own, pluginregistry/registrar, which the command line
// imports and the core must not: the MCP server links the core, so anything
// the core could do to the registry, a tool could be made to do for an
// agent. A test in internal/mcpserver fails when that package is among the
// server's dependencies.

// Plugin is a Knowledge base plugin registered on this computer: a name, and
// either the command that starts it or the URL it answers at.
type Plugin = pluginregistry.Plugin

// PluginList is the Knowledge base plugins registered on this computer,
// ordered by name.
type PluginList = pluginregistry.List

// Plugins lists the Knowledge base plugins registered on this computer. A
// plugin study does not use as it is registered, because its program or an
// argument is inside the Study home this study uses, carries a Problem. It
// reads only, and fails without reading when the registry is where the agent
// could write it.
func (c *Core) Plugins(ctx context.Context) (PluginList, error) {
	return pluginregistry.Read(ctx, c.PluginRegistryEnv())
}

// PluginRegistryEnv is what the registry needs to know of this run of study:
// the Study home, the folder study started in, and the environment.
func (c *Core) PluginRegistryEnv() pluginregistry.Env {
	return pluginregistry.Env{StudyHome: c.home, Dir: c.dir, Getenv: c.getenv}
}
