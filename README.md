# yc1

Personal development environment manager for configs, profiles, and agent skills
on macOS and Linux.

## Installation

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/yingca1/yc1/main/install.sh)"
```

This installs the `yc1` command to `~/.local/bin/yc1`. It does not install
configuration files or dependencies automatically.

## yc1

`yc1` manages configs, profiles, and agent skills
from `~/.config/yc1`:

- `~/.config/yc1/source`: the git-managed yc1 source
- `~/.config/yc1/state`: install state for configs and skills
- `~/.config/yc1/backups`: backups created before replacing existing files
- `~/.config/yc1/local`: local overrides sourced by managed configs
- `~/.config/yc1/source/profiles`: committed, shared profile definitions
- `~/.config/yc1/source/_profiles`: local profile definitions ignored by git

Common commands:

```bash
yc1 up -p default       # install the built-in default profile
yc1 up -p minimal       # install git/curl/wget only
yc1 up -f yc1.yml       # install configs and skills from a profile file
yc1 config up vim       # install selected config resources
yc1 config up vim --copy
yc1 skill up my-skill   # link a skill declared in ./yc1.yml
yc1 status -f yc1.yml
yc1 pull                # update ~/.config/yc1/source
yc1 update              # update the yc1 binary from GitHub Releases
```

Configs are organized by tool: `zsh`, `vim`, `tmux`, `kitty`, `git`, `curl`, and
`wget`.

Each config is driven by a `configs/<name>/yc1.yml` manifest. YAML is used so notes and
comments can live next to file mappings, OS-specific targets, and dependency
commands.

Profile files use the same name at the top level:

```yaml
version: 1
vars:
  proj_skills: ~/Code/workspace/proj/skills
  agents_project_target: .agents/skills
  claude_project_target: .claude/skills
configs:
  - git
  - vim
skills:
  - name: proj-onboarding
    source: ${vars.proj_skills}
    targets:
      - ${vars.agents_project_target}
      - ${vars.claude_project_target}
```

For skills, `source` is the skills root and yc1 links `<source>/<name>`.
Targets are install directory paths. Relative target paths are resolved from the
profile file directory, and status/state labels are derived from the target path.
Use top-level `vars` and `${vars.name}` references to reuse source roots and
target directories inside skill definitions.

Named profiles use `yc1 up/down/status -p <name>`. Resolution checks local
profiles first, then shared profiles, then the hardcoded `minimal` and `default`
fallbacks:

1. `~/.config/yc1/source/_profiles/<name>/yc1.yml`
2. `~/.config/yc1/source/profiles/<name>/yc1.yml`
3. built-in fallback for `minimal` and `default`

This lets each machine keep private choices in `_profiles/` while shared
profiles stay committed in `profiles/`.

Bare `yc1 up/down/status` reads `./yc1.yml`. If the current directory does not
have one, pass `-f` or `-p`.

`yc1 up` installs dependencies only for the configs being activated. `yc1 down`
removes managed configuration files and restores backups, but does not uninstall software dependencies.

## How to use

### tmux

- both `<C-a>` and `<C-b>` are the prefix
- tmux uses `tmux-256color` with focus and clipboard support enabled
- copy mode uses tmux's native clipboard integration
- `prefix v` makes a vertical split
- `prefix s` makes a horizontal split
- pane numbering starts at `1`

If you have three or more panes:

- `prefix +` opens up the main-horizontal-layout
- `prefix =` opens up the main-vertical-layout

You can adjust the size of the smaller panes in `tmux.conf` by lowering or increasing the `other-pane-height` and `other-pane-width` options.

### kitty

The `kitty` config writes:

- `~/.config/kitty/kitty.conf`
- `~/.config/kitty/current-theme.conf`

The default profile is tuned for tmux + vim + zsh:

- `JetBrainsMonoNL Nerd Font Mono` at 15pt
- `xterm-kitty` with kitty shell integration enabled
- `Option` works as `Alt`, so common shell and vim meta bindings work naturally
- `Cmd+C`/`Cmd+V` use the macOS clipboard
- `Cmd+T`, `Cmd+Enter`, and `Cmd+\` provide light kitty tab/window/split controls while tmux remains the main multiplexer
- Catppuccin Mocha is isolated in `current-theme.conf` so themes can be swapped without touching keybindings

### vim

The following assume your leader key is set to `\`.

- `\d` brings up [NERDTree](https://github.com/scrooloose/nerdtree), a sidebar buffer for navigating and manipulating files
- `\t` brings up [ctrlp.vim](https://github.com/ctrlpvim/ctrlp.vim), a project file filter for easily opening specific files
- `\b` restricts ctrlp.vim to open buffers
- `\a` starts project search with [ag.vim](https://github.com/rking/ag.vim) using [the silver searcher](https://github.com/ggreer/the_silver_searcher) (like ack, but faster)
- `ds`/`cs` delete/change surrounding characters (e.g. `"Hey!"` + `ds"` = `Hey!`, `"Hey!"` + `cs"'` = `'Hey!'`) with [vim-surround](https://github.com/tpope/vim-surround)
- `gcc` toggles current line comment
- `gc` toggles visual selection comment lines
- `vii`/`vai` visually select _in_ or _around_ the cursor's indent
- `Vp`/`vp` replaces visual selection with default register _without_ yanking selected text (works with any visual selection)
- `\[space]` strips trailing whitespace
- `<C-]>` jump to definition using ctags
- `\l` begins aligning lines on a string, usually used as `\l=` to align assignments
- `<C-hjkl>` move between windows, shorthand for `<C-w> hjkl`

#### Plugin Notes

- Plugins managed with Plug, remember to use the command :PlugInstall after opening vim to install plugins
- Plugin installation depends on accessing repositories on GitHub, ensure connectivity to github.com
- Need to add the local id_isa.pub to the SSH keys in the personal settings of GitHub
- For more plugins, visit [https://vimawesome.com/](https://vimawesome.com/)

##### Default Installations

|Plugin|Remarks|
|---|---|
|christoomey/vim-tmux-navigator|Seamless integration with tmux split screens|
|preservim/nerdtree|File navigation sidebar|
|junegunn/fzf.vim|Command-line fuzzy search tool, ripgrep, bat, fd-find, silversearcher-ag, fzf|
|ctrlpvim/ctrlp.vim|Project file retrieval within the project|
|justinmk/vim-sneak|Locate by two characters before and after|
|easymotion/vim-easymotion|Quick navigation within the file by anchor points|
|tpope/vim-surround|Change, delete surrounding characters or tags|
|tpope/vim-dispatch|Tool for asynchronous use. [Video demo](https://vimeo.com/63116209)|
|tpope/vim-unimpaired|Supports many paired operations, such as swapping lines|
|tpope/vim-repeat|Enhances native ' . ' repetition functionality, supports multiple plugins|
|airblade/vim-gitgutter|Displays git status on the left side of the file|
|tpope/vim-fugitive|Common git commands|
|bling/vim-airline|Nice bottom status bar|
|nathanaelkane/vim-indent-guides|Visualizes line indentation|
|godlygeek/tabular|Aligns text, such as tables. [Screen recording demo](http://vimcasts.org/episodes/aligning-text-with-tabular-vim/)|
|vim-scripts/Align|Aligns code, for example, when declaring variables|
|tpope/vim-commentary|Comment and uncomment|
|mattn/emmet-vim|Tool for quickly writing xml|

##### Optional Installations

|Plugin|Remarks|
|---|---|
|majutsushi/tagbar|Generates outlines of file content for easy browsing|
|rking/ag.vim|Full-text search within the project|
|[garbas/vim-snipmate](https://github.com/garbas/vim-snipmate)|Manages code snippets|
|tomtom/tlib_vim|Dependency for SnipMate|
|MarcWeber/vim-addon-mw-utils|Dependency for SnipMate|
|pangloss/vim-javascript|Indentation and syntax support for js|
|scrooloose/syntastic|Syntax checking tool|
|psf/black|Formats python code|

## Thanks

- [https://github.com/square/maximum-awesome](https://github.com/square/maximum-awesome)
- [https://github.com/gpakosz/.tmux](https://github.com/gpakosz/.tmux)
- [https://github.com/VSCodeVim/Vim](https://github.com/VSCodeVim/Vim)
