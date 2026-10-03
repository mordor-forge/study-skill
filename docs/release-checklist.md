# v2.0 release checklist

The maintainer's steps to ship Lamplight v2.0 (issue #36). CI already proves the learner
loop (`internal/e2e/loop_test.go`) and the acceptance walkthrough
(`internal/e2e/journey_test.go`: `study setup` with fake agents and the MCP server started
from what it registered, a Lesson with a failing then passing Check and a rubric item, a
break and a resume, the Milestone's Assessment, and an imported and adopted v1 workspace).
What is left needs real agents, real v1 workspaces and repository settings, so it is done
by hand, in this order. `docs/release.md` (from #34) covers the release pipeline itself.

Until the rename in step 6 the repository is `mordor-forge/study-skill`; the commands use
`$REPO` for it:

```sh
REPO=mordor-forge/study-skill        # bash, zsh; in fish: set REPO mordor-forge/study-skill
```

## 1. Before you start

- [ ] Every v2.0 issue except #18, #36 and #37 has landed on `v2`, including #34 (the
      GoReleaser pipeline). Issues fixed on `v2` are closed by hand, so this lists only
      those three:

      ```sh
      gh issue list -R $REPO --milestone v2.0 --state open
      gh pr list -R $REPO --base v2
      ```

- [ ] #34's prerequisites, from `docs/release.md`, are done:
  - a `LICENSE` file at the repository root (the README says MIT), which the archives and
    packages ship;
  - the tap repository `mordor-forge/homebrew-tap` (public, with a `Casks/` folder), and a
    fine-grained token with contents write access to it alone, stored as the Actions
    secret `HOMEBREW_TAP_TOKEN`;
  - an AUR account with a password-less SSH key, `lamplight-bin` claimed, and the private
    key stored as the Actions secret `AUR_KEY`;
  - the maintainer named in `.goreleaser.yaml` checked.

  ```sh
  git cat-file -e upstream/v2:LICENSE && echo LICENSE present
  gh repo view mordor-forge/homebrew-tap
  gh secret list -R $REPO                 # HOMEBREW_TAP_TOKEN and AUR_KEY, names only
  ```

- [ ] #18's first half is done: the `v1.0.0` tag points at the last v1 commit, `main`'s
      README says v2 is in development on `v2` (#40), and the five dependabot PRs open
      against `main` are closed. They bump actions in v1's workflow, which the merge
      replaces; dependabot opens new ones against v2's workflow once `main` is v2.

      ```sh
      gh pr close 4 -R $REPO --delete-branch --comment "v1 is frozen at bug fixes, and v2 replaces this workflow."
      gh pr close 5 -R $REPO --delete-branch --comment "v1 is frozen at bug fixes, and v2 replaces this workflow."
      gh pr close 6 -R $REPO --delete-branch --comment "v1 is frozen at bug fixes, and v2 replaces this workflow."
      gh pr close 7 -R $REPO --delete-branch --comment "v1 is frozen at bug fixes, and v2 replaces this workflow."
      gh pr close 12 -R $REPO --delete-branch --comment "v1 is frozen at bug fixes, and v2 replaces this workflow."
      gh pr list -R $REPO --base main         # only the v2 merge, later, belongs here
      ```

- [ ] CI is green on `v2`, and the full suite passes locally:

      ```sh
      gh run list -R $REPO --branch v2 --limit 3
      git fetch upstream && git switch --detach upstream/v2
      go test -race -count=1 ./...
      go test -shuffle=on -count=2 ./...
      GOOS=darwin GOARCH=arm64 go vet ./...
      go test -count=1 -v -run TestTheV2Journey ./internal/e2e
      ```

## 2. Install the v2 build and a trial Study home

Build `study` from `v2`, and point every `study` (the CLI and `study mcp` under any agent)
at a trial Study home through the config file. Agents may not pass environment variables
such as `STUDY_HOME` on to MCP servers, but every `study` reads the config file. Leave
`STUDY_HOME` unset, since it overrides the file.

```sh
git switch --detach upstream/v2
go install ./cmd/study                 # the trial build, in $(go env GOPATH)/bin
command -v study                       # must print $(go env GOPATH)/bin/study
study --version
# The config file is in $XDG_CONFIG_HOME when that is set, else in ~/.config. Run through sh,
# so it reads the same in fish; set -C refuses to replace a config you already have.
sh -c 'config="${XDG_CONFIG_HOME:-$HOME/.config}/lamplight/config.toml"
mkdir -p "$(dirname "$config")" && set -C &&
printf "format = 1\nstudy_home = \"~/study-v2-trial\"\n" > "$config" && echo "wrote $config"'
"$(go env GOPATH)/bin/study" doctor    # checks the trial build; study_home must be ~/study-v2-trial
```

If the `sh -c` line fails because the file exists, add the `study_home` line to that file by
hand. Go on only once `doctor` names the trial Study home: with any other, step 3 would import
into your real one.

v1 stays installed and working: v2's skill is `lamplight`, and nothing in v2 writes to v1's
`~/.agents/skills/study`.

## 3. The acceptance import

Import each of your v1 workspaces into the trial Study home, dry run first. The import
copies; the v1 workspace is left untouched and keeps working with v1.

```sh
git -C ~/path/to/workspace status --short     # note it, to compare afterwards
study import ~/path/to/workspace --dry-run    # read the report: done, open, dropped
study import ~/path/to/workspace
study status
git -C ~/path/to/workspace status --short     # unchanged
```

- [ ] The Lessons shown done are the ones you finished. If one is not, move the Topic aside
      and import again keeping it open:

      ```sh
      study topic remove <topic>      # prints the mv command that would restore it
      study import ~/path/to/workspace --not-done lesson-NN
      ```

- [ ] `study status` recommends adopting each imported Topic; adopt one in an agent Session
      in step 4.

## 4. Manual Sessions in Claude Code and Codex

Register the agents for real with the trial build, then run the same journey once in each.

```sh
study setup --dry-run      # what it will write: the lamplight skill and the MCP registrations
study setup
study setup --check
"$(go env GOPATH)/bin/study" doctor    # the trial build, now registered with both agents
```

In a new Claude Code conversation (`claude`), then again in Codex (`codex`):

- [ ] Ask to study something new. The agent uses the `lamplight` skill and the `study`
      tools (not v1's skill): it creates a Topic and proposes a Syllabus you approve in chat.
- [ ] It teaches a Lesson with a Check. You work in `practice/<lesson>/`; the agent runs
      `study check` in its shell; you see a failing Check, fix it, and see it pass.
- [ ] The Lesson is completed with a draft Card, and you review it.
- [ ] Ask for a break at a Break point. The agent closes the Session with a Next step, and
      the stop Checkpoint holds your work:

      ```sh
      study status
      git -C ~/study-v2-trial/<topic> log --oneline -3
      ```

- [ ] Quit, open a new conversation and ask to continue: the agent resumes from the Next
      step without you repeating it.
- [ ] Adopt one imported Topic (step 3) in a Session: the agent walks the adoption and you
      approve its Syllabus.

## 5. The maintainer's test run

- [ ] Use v2 for your own studying from the `v2` build and the trial Study home for as long
      as you need. File what you find as v2.0 issues, fix them through PRs into `v2`, and
      reinstall with `go install ./cmd/study` after each merge.
- [ ] When v2 is approved for release, say so on #36.

## 6. Release

In this order: the v1 branch, the checks `main` requires, the rename, the merge, the tag.

- [ ] Create the `v1` branch from the v1 tag, for later v1 bug fixes:

      ```sh
      git fetch upstream --tags
      git log --oneline v1.0.0..upstream/main    # v1 commits after the tag, such as #18's notice
      git push upstream 'v1.0.0^{commit}:refs/heads/v1'
      ```

      Protect it in Settings → Rules → Rulesets with a new ruleset for `v1` that requires
      v1's checks: "Python catalog", "Go FSRS" and "Study lifecycle smoke test". v1's own
      workflow runs them on pull requests into `v1`.

- [ ] Change the checks `main` requires. The "main protection" ruleset requires v1's three
      checks, which v2's CI does not have, so the merge would wait for them forever. In
      Settings → Rules → Rulesets → "main protection", replace them with "Lamplight core".
      #34's "Release snapshot" job runs on every pull request too, but it is not a required
      check, as `docs/release.md` decides: do not add it.
- [ ] Rename the repository to `lamplight`, before the tag: the module path
      (`github.com/mordor-forge/lamplight/v2`), the packages, the plugin marketplace and the
      README already use the new name, and Go's module proxy remembers the first fetch.

      ```sh
      gh repo rename lamplight -R mordor-forge/study-skill
      REPO=mordor-forge/lamplight      # in fish: set REPO mordor-forge/lamplight
      git remote set-url upstream git@github.com:mordor-forge/lamplight.git
      git ls-remote https://github.com/mordor-forge/study-skill.git HEAD   # the old URL redirects
      ```

      Renaming the fork as well is optional:
      `gh repo rename lamplight -R Gdetrane/study-skill`, then
      `git remote set-url origin git@github.com:Gdetrane/lamplight.git`.

- [ ] Merge `v2` into `main` with a pull request and a merge commit, which keeps v2's
      history. If `main` has commits `v2` lacks, merge `main` into `v2` first through a PR
      into `v2`, keeping v2's files (the README in particular).

      ```sh
      git fetch upstream
      git log --oneline upstream/v2..upstream/main     # must be empty, or merged into v2 first
      gh pr create -R $REPO --base main --head v2 --title "feat: Lamplight v2.0" \
        --body "Merges v2 into main for the v2.0.0 release (#36)."
      gh pr checks <pr> -R $REPO --watch
      gh pr merge <pr> -R $REPO --merge
      ```

- [ ] Tag `v2.0.0` on `main`. The tag starts #34's release workflow. GitHub runs the
      workflow file of the tagged commit, so push `v*` tags only on release commits.

      ```sh
      git fetch upstream
      git log --oneline -1 upstream/main               # the merge commit
      git tag -a v2.0.0 -m "Lamplight v2.0.0" upstream/main
      git push upstream v2.0.0
      gh run list -R $REPO --limit 3
      ```

## 7. After the release

- [ ] The release has every package, and each one installs and passes `study doctor`. Run
      each package's own `study` by its path: the trial build from step 2 may come first on
      `PATH`.

      ```sh
      gh release view v2.0.0 -R $REPO
      go install github.com/mordor-forge/lamplight/v2/cmd/study@v2.0.0   # replaces the trial build
      "$(go env GOPATH)/bin/study" --version && "$(go env GOPATH)/bin/study" doctor
      brew install --cask mordor-forge/tap/lamplight                      # on a Mac
      "$(brew --prefix)/bin/study" --version && "$(brew --prefix)/bin/study" doctor
      yay -S lamplight-bin                                                # on Arch
      /usr/bin/study --version && /usr/bin/study doctor
      ```

- [ ] The Claude Code plugin installs, now that `main` holds `.claude-plugin/`. The plugin
      and `study setup` never both serve Claude Code, so hand Claude Code over first, and
      afterwards keep whichever you prefer:

      ```sh
      study setup --remove --agent claude
      claude plugin marketplace add mordor-forge/lamplight
      claude plugin install lamplight@lamplight
      ```

      Start `claude` and check the agent has the `lamplight` skill and the `study` tools.
- [ ] Close what the release finished. GitHub closes issues automatically only for merges
      into the default branch, so close the rest by hand: #18 once its checklist is done,
      #36, then #37, then the v2.0 milestone (Issues → Milestones → v2.0 → Close).

      ```sh
      gh issue list -R $REPO --milestone v2.0 --state open
      gh issue close <n> -R $REPO --comment "Released in v2.0.0."
      ```

- [ ] Refresh the published design page from `docs/design/lamplight-v2.md`, `CONTEXT.md`
      and the ADRs as tagged in `v2.0.0`.
- [ ] Move to the real Study home when you are ready: remove `study_home` from the config
      file step 2 wrote (or point it at the real Study home), import your v1
      workspaces there, dry run first, and keep or delete `~/study-v2-trial`.
- [ ] Keep the `v2` branch until the release has settled. To delete it, first delete the
      "v2 protection" ruleset, which forbids deleting the branch, and drop `v2` from the
      branches in `.github/workflows/ci.yml`.
