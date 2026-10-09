package cli

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mordor-forge/lamplight/v2/internal/core"
	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/registrar"
)

// knowledgeBaseCommand is study knowledge-base: the registry of the
// Knowledge base plugins this computer has (ADR-0012). It is for the
// learner. The MCP server has no tool for any of it, so no agent decides
// over MCP which program study starts.
//
// Listing goes through the core, like every reading. Registering and
// removing go to the registrar package, which this package alone imports:
// the core does not hold what changes the registry, so that the MCP server,
// which links the core, cannot be made to change it.
func (a *app) knowledgeBaseCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "knowledge-base",
		Short: "Register the Knowledge base plugins this computer has",
		Long: "Register Knowledge base plugins on this computer, each under a name: a program, or a\n" +
			"service you run, that indexes Sources and searches them.\n\n" +
			"The registry says what each name stands for, a command or a URL. It is\n" +
			"knowledge-base-plugins.json in Lamplight's configuration folder ($XDG_CONFIG_HOME/lamplight,\n" +
			"or ~/.config/lamplight): this computer's, outside the Study home, and never part of a\n" +
			"Topic. These commands are for you; agents have no tool for them.",
		Args: noArgs,
		RunE: a.groupHelp,
	}

	var spec registrar.Spec
	add := &cobra.Command{
		Use:   "add <name> [-- <command> [args...]]",
		Short: "Register a Knowledge base plugin under a name, by its command or its URL",
		Long: "Register a Knowledge base plugin on this computer under a name: lowercase letters,\n" +
			"digits and single hyphens, up to 64 characters, and neither none nor plugin.\n\n" +
			"Give the command that starts the plugin after --, as you would type it: the program,\n" +
			"then its arguments, which are kept as they are. The program is found now, on your PATH\n" +
			"or at the path you give, and registered by its absolute path. Or give the http or https\n" +
			"address of a plugin you run yourself, with --url.\n\n" +
			"A plugin is started outside your agent's sandbox, and your agent can write in the Study\n" +
			"home. So a program inside the Study home, or reached through a link there, is refused.\n" +
			"So is an argument that names something there, such as a script kept in a Topic, unless\n" +
			"you pass --allow-study-home-arguments. That check catches the honest mistake; it cannot\n" +
			"see what a command such as sh -c or npx goes on to load.\n\n" +
			"A plugin is started from the registry's folder, so give files in its arguments by their\n" +
			"full path. Registering a plugin as it is registered already changes nothing. A name that\n" +
			"stands for something else is refused unless you pass --replace.",
		Example: `  study knowledge-base add shelf -- study-shelf
  study knowledge-base add notes --dry-run -- ~/bin/notes-kb --stdio
  study knowledge-base add remote --url http://localhost:8765/mcp
  study knowledge-base add shelf --replace -- /opt/shelf/bin/study-shelf`,
		Args: func(cmd *cobra.Command, args []string) error {
			const shape = "study knowledge-base add <name> -- <command> [args...], or study knowledge-base add <name> --url <URL>"
			switch dash := cmd.ArgsLenAtDash(); {
			case len(args) == 0 || dash == 0:
				return usageError{fmt.Errorf("%q needs the plugin's name first: %s", cmd.CommandPath(), shape)}
			case dash < 0 && len(args) > 1:
				// Without --, an argument of the command that starts with a
				// hyphen would be taken for a flag of study's.
				return usageError{fmt.Errorf("put the command that starts the plugin after --: %s", shape)}
			case dash > 1:
				return usageError{fmt.Errorf("%q takes one name before --, got %d: %s", cmd.CommandPath(), dash, shape)}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			spec.Name, spec.Command = args[0], nil
			hasCommand, hasURL := cmd.ArgsLenAtDash() == 1, cmd.Flags().Changed("url")
			switch {
			case hasCommand && hasURL:
				return a.fail(usageError{errors.New("give the plugin a command after --, or --url, not both")})
			case !hasCommand && !hasURL:
				return a.fail(usageError{errors.New("give the command that starts the plugin after --, or its address with --url")})
			case hasCommand && len(args) == 1:
				return a.fail(usageError{errors.New("nothing follows --: give the command that starts the plugin")})
			case hasURL && spec.URL == "":
				// An empty --url is a mistake, such as a script's empty
				// variable, and never a way to say "no URL".
				return a.fail(usageError{errors.New("--url needs the plugin's http or https address")})
			case hasCommand:
				spec.Command = args[1:]
			}
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			res, err := registrar.Add(cmd.Context(), c.PluginRegistryEnv(), spec)
			if err != nil {
				return a.fail(err)
			}
			if res.Changed && !res.DryRun {
				a.logs.logger.Info("registered a Knowledge base plugin", "name", res.Plugin.Name, "registry", res.Registry)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: res})
			}
			return writePluginRegistration(a.out, res)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			switch {
			case a.completingAfterDash():
				// The plugin's command: files, as the shell completes them.
				return nil, cobra.ShellCompDirectiveDefault
			case len(args) == 0 && spec.Replace:
				// Only a name that is registered can be replaced.
				return a.completePluginNames(cmd)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	add.Flags().StringVar(&spec.URL, "url", "", "the http or https address of a plugin you run as a service")
	add.Flags().BoolVar(&spec.AllowStudyHomeArguments, "allow-study-home-arguments", false,
		"register the command although an argument names something inside the Study home, which your agent can change")
	add.Flags().BoolVar(&spec.Replace, "replace", false, "replace what the name is registered as")
	add.Flags().BoolVar(&spec.DryRun, "dry-run", false, "show what would be registered without writing anything")
	_ = add.RegisterFlagCompletionFunc("url", cobra.NoFileCompletions)

	list := &cobra.Command{
		Use:   "list",
		Short: "List the Knowledge base plugins registered on this computer",
		Long: "List the Knowledge base plugins registered on this computer, by name, each with the\n" +
			"command or the URL its name stands for, and where the registry is.",
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			res, err := c.Plugins(cmd.Context())
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: res})
			}
			return writePluginList(a.out, res)
		},
	}

	var removeDryRun bool
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a Knowledge base plugin from this computer's registry",
		Long: "Remove a Knowledge base plugin from this computer's registry. Only the registration\n" +
			"goes: the program, or the service, is left as it is.",
		Example: "  study knowledge-base remove shelf --dry-run\n  study knowledge-base remove shelf",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			res, err := registrar.Remove(cmd.Context(), c.PluginRegistryEnv(), args[0], removeDryRun)
			if err != nil {
				return a.fail(err)
			}
			if !res.DryRun {
				a.logs.logger.Info("removed a Knowledge base plugin", "name", res.Plugin.Name, "registry", res.Registry)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: res})
			}
			return writePluginRemoval(a.out, res)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return a.completePluginNames(cmd)
		},
	}
	remove.Flags().BoolVar(&removeDryRun, "dry-run", false, "show what would be removed without writing anything")

	group.AddCommand(add, list, remove)
	return group
}

// completingAfterDash reports whether the word the shell is completing
// follows a --. cobra does not tell: while it completes, the flag set's own
// count of the arguments before -- is no longer the command line's.
func (a *app) completingAfterDash() bool {
	return len(a.args) > 0 && slices.Contains(a.args[:len(a.args)-1], "--")
}

// completePluginNames offers the names registered on this computer to the
// shell. A registry that cannot be read offers none, and says nothing: the
// command itself explains it when it is run.
func (a *app) completePluginNames(cmd *cobra.Command) ([]string, cobra.ShellCompDirective) {
	c, err := core.Open(a.opts)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	list, err := c.Plugins(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(list.Plugins))
	for _, p := range list.Plugins {
		names = append(names, p.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// plainWord is a word every shell takes as it is, so it needs no quotes. One
// that starts with = is not: zsh reads =name as the path of a command.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+:,./-][A-Za-z0-9_@%+=:,./-]*$`)

// describePlugin is what a plugin's name stands for: its URL, or its command
// as a shell would take it, each word quoted when it needs to be.
func describePlugin(p core.Plugin) string {
	if p.URL != "" {
		return printable(p.URL)
	}
	words := make([]string, 0, len(p.Command))
	for _, w := range p.Command {
		if !plainWord.MatchString(w) {
			w = shellQuote(w)
		}
		words = append(words, w)
	}
	return printable(strings.Join(words, " "))
}

// allowedNote is said of a plugin registered with
// --allow-study-home-arguments.
const allowedNote = "Its arguments inside the Study home are allowed: your agent can change what they name."

func writePluginRegistration(w io.Writer, r registrar.Registration) error {
	name := styleAccent.Render(printable(r.Plugin.Name))
	if !r.Changed {
		_, err := fmt.Fprintf(w, "Knowledge base plugin %s is registered that way already: nothing changed\n", name)
		return err
	}
	verb, replaces := "Registered", "It replaces"
	if r.DryRun {
		verb, replaces = "Would register", "It would replace"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s Knowledge base plugin %s on this computer: %s\n", verb, name, describePlugin(r.Plugin))
	if r.Plugin.AllowStudyHomeArguments {
		fmt.Fprintf(&b, "%s %s\n", styleWarn.Render("!"), allowedNote)
	}
	if r.Replaced != nil {
		fmt.Fprintf(&b, "%s: %s\n", replaces, describePlugin(*r.Replaced))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writePluginRemoval(w io.Writer, r registrar.Removal) error {
	verb := "Removed"
	if r.DryRun {
		verb = "Would remove"
	}
	_, err := fmt.Fprintf(w, "%s Knowledge base plugin %s from this computer's registry: %s\n",
		verb, styleAccent.Render(printable(r.Plugin.Name)), describePlugin(r.Plugin))
	return err
}

func writePluginList(w io.Writer, l core.PluginList) error {
	if len(l.Plugins) == 0 {
		_, err := fmt.Fprintf(w, "No Knowledge base plugins are registered on this computer. Register one with:\n  %s\n  %s\n",
			styleAccent.Render("study knowledge-base add <name> -- <command> [args...]"),
			styleAccent.Render("study knowledge-base add <name> --url <URL>"))
		return err
	}
	var b strings.Builder
	b.WriteString("Knowledge base plugins on this computer:\n")
	names := make([]string, 0, len(l.Plugins))
	for _, p := range l.Plugins {
		names = append(names, printable(p.Name))
	}
	width := widest(names)
	for i, p := range l.Plugins {
		fmt.Fprintf(&b, "%s  %s\n", styleAccent.Render(pad(names[i], width)), describePlugin(p))
		if p.AllowStudyHomeArguments {
			fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("!"), allowedNote)
		}
		if p.Problem != "" {
			fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("!"), printable(p.Problem))
		}
	}
	fmt.Fprintf(&b, "%s\n", styleDim.Render("Registry: "+printable(l.Registry)))
	_, err := io.WriteString(w, b.String())
	return err
}
