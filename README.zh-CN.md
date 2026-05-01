# yc1

个人开发环境管理工具，用于管理 configs、profiles 和 agent skills，支持 macOS 和 Linux。

## 安装

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/yingca1/yc1/main/install.sh)"
```

这只会把 `yc1` 命令安装到 `~/.local/bin/yc1`，不会自动安装配置文件或依赖。

## yc1

`yc1` 使用 `~/.config/yc1` 管理 configs、profiles 和 agent skills：

- `~/.config/yc1/source`：git 管理的 yc1 源码
- `~/.config/yc1/state`：configs 和 skills 的安装状态
- `~/.config/yc1/backups`：替换已有文件前创建的备份
- `~/.config/yc1/local`：被托管配置读取的本地覆盖配置
- `~/.config/yc1/source/profiles`：提交到仓库、可共享的 profiles
- `~/.config/yc1/source/_profiles`：本机私有、被 git ignore 的 profiles

常用命令：

```bash
yc1 up -p default       # 安装内置 default profile
yc1 up -p minimal       # 只安装 git/curl/wget
yc1 up -f yc1.yml       # 从 profile 文件安装 configs 和 skills
yc1 config up vim       # 安装指定 config
yc1 config up vim --copy
yc1 skill up my-skill   # 链接 ./yc1.yml 中声明的 skill
yc1 status -f yc1.yml
yc1 pull                # 更新 ~/.config/yc1/source
yc1 update              # 从 GitHub Releases 更新 yc1 二进制
```

configs 按工具组织：`zsh`、`vim`、`tmux`、`kitty`、`git`、`curl`、`wget`。

每个 config 由 `configs/<name>/yc1.yml` manifest 驱动。使用 YAML 是为了能把说明和注释
直接写在文件映射、OS 条件和依赖命令旁边。

profile 文件也使用顶层 `yc1.yml`：

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

对于 skills，`source` 表示 skills 根目录，yc1 会链接 `<source>/<name>`。
targets 是实际安装目录路径。相对 target path 会按 profile 文件所在目录解析，
status/state 的标识会从 target path 派生。可以用顶层 `vars` 和 `${vars.name}`
引用复用 skill source root 和 target 目录。

命名 profile 使用 `yc1 up/down/status -p <name>`。解析顺序是本地优先、
共享其次，最后只对 `minimal` 和 `default` 使用硬编码兜底：

1. `~/.config/yc1/source/_profiles/<name>/yc1.yml`
2. `~/.config/yc1/source/profiles/<name>/yc1.yml`
3. `minimal` 和 `default` 的内置兜底

这样每台机器可以把自己的选择放在 `_profiles/`，而通用配置继续放在
`profiles/` 里提交和共享。

裸 `yc1 up/down/status` 会读取当前目录的 `./yc1.yml`。如果当前目录没有该文件，
需要传 `-f` 或 `-p`。

`yc1 up` 只会安装当前激活 config 所需的依赖。依赖检查会把声明的
`command`、`commands`、`path`、`paths`、`probe`、`probes` 作为 OR 条件。
`command` 检查 `PATH`，`path` 检查磁盘路径，`probe` 会运行一个非交互 shell 命令并要求
成功退出，因此可以识别不是通过包管理器安装的软件。`yc1 config status` 会在 config 汇总状态
下面打印依赖检查结果。`yc1 down` 会移除托管配置并恢复备份，但不会卸载软件依赖。

## 如何使用

### tmux

- `<C-a>`和`<C-b>`都是前缀键
- tmux 使用 `tmux-256color`，并启用焦点与剪贴板能力
- 复制模式使用 tmux 原生剪贴板集成
- `prefix v`进行垂直分割
- `prefix s`进行水平分割
- pane 编号从 `1` 开始

如果您有三个或更多窗格：

- `prefix +`打开主水平布局
- `prefix =`打开主垂直布局

您可以通过降低或增加`tmux.conf`中的`other-pane-height`和`other-pane-width`选项来调整较小窗格的大小。

### kitty

`kitty` config 会写入：

- `~/.config/kitty/kitty.conf`
- `~/.config/kitty/current-theme.conf`

默认配置针对 tmux + vim + zsh：

- 使用 `JetBrainsMonoNL Nerd Font Mono`，字号 15pt
- 使用 `xterm-kitty`，并启用 kitty shell integration
- `Option` 作为 `Alt`，方便 shell 与 vim 的 meta 快捷键
- `Cmd+C`/`Cmd+V` 使用 macOS 剪贴板
- `Cmd+T`、`Cmd+Enter`、`Cmd+\` 提供轻量的 kitty tab/window/split 控制，主分屏仍交给 tmux
- Catppuccin Mocha 主题放在 `current-theme.conf`，换主题时不用改快捷键

### vim

以下假设您的领导键设置为`\`。

- `\d`打开[NERDTree](https://github.com/scrooloose/nerdtree)，一个用于导航和操作文件的侧边栏缓冲区
- `\t`打开[ctrlp.vim](https://github.com/ctrlpvim/ctrlp.vim)，一个项目文件过滤器，可轻松打开特定文件
- `\b`限制ctrlp.vim打开缓冲区
- `\a`使用[银色搜索器](https://github.com/ggreer/the_silver_searcher)（类似于ack，但更快）启动项目搜索，使用[ag.vim](https://github.com/rking/ag.vim)
- `ds`/`cs`删除/更改周围字符（例如，`"Hey!"` + `ds"` = `Hey!`，`"Hey!"` + `cs"'` = `'Hey!'`）使用[vim-surround](https://github.com/tpope/vim-surround)
- `gcc`切换当前行注释
- `gc`切换可视选择注释行
- `vii`/`vai`在光标缩进处选择_in_或_around_
- `Vp`/`vp`用默认寄存器替换可视选择 _without_ 复制选定文本（适用于任何可视选择）
- `\[space]`删除尾随空格
- `<C-]>`使用ctags跳转到定义
- `\l`开始将行对齐到字符串，通常用作`\l=`以对齐分配
- `<C-hjkl>`在窗口之间移动，缩写为`<C-w> hjkl`

#### 插件说明

- 使用Plug管理插件，请在打开vim后使用命令:PlugInstall安装插件
- 插件安装取决于访问GitHub上的存储库，请确保连接到github.com
- 需要将本地id_isa.pub添加到GitHub个人设置中的SSH密钥中
- 对于更多插件，请访问[https://vimawesome.com/](https://vimawesome.com/)

##### 默认安装

|插件|备注|
|---|---|
|christoomey/vim-tmux-navigator|与tmux分屏无缝集成|
|preservim/nerdtree|文件导航侧边栏|
|junegunn/fzf.vim|命令行模糊搜索工具，ripgrep，bat，fd-find，silversearcher-ag，fzf|
|ctrlpvim/ctrlp.vim|在项目中检索项目文件|
|justinmk/vim-sneak|定位前后两个字符|
|easymotion/vim-easymotion|通过锚点快速导航文件内部|
|tpope/vim-surround|更改、删除周围字符或标记|
|tpope/vim-dispatch|用于异步使用的工具。[视频演示](https://vimeo.com/63116209)|
|tpope/vim-unimpaired|支持许多配对操作，如交换行|
|tpope/vim-repeat|增强本地' . '重复功能，支持多个插件|
|airblade/vim-gitgutter|在文件左侧显示git状态|
|tpope/vim-fugitive|常见的git命令|
|bling/vim-airline|漂亮的底部状态栏|
|nathanaelkane/vim-indent-guides|可视化行缩进|
|godlygeek/tabular|对齐文本，例如表格。[屏幕录制演示](http://vimcasts.org/episodes/aligning-text-with-tabular-vim/)|
|vim-scripts/Align|对齐代码，例如声明变量时|
|tpope/vim-commentary|注释和取消注释|
|mattn/emmet-vim|快速编写xml的工具|

##### 可选安装

|插件|备注|
|---|---|
|majutsushi/tagbar|为轻松浏览生成文件内容轮廓|
|rking/ag.vim|项目内的全文搜索|
|[garbas/vim-snipmate](https://github.com/garbas/vim-snipmate)|管理代码片段|
|tomtom/tlib_vim|SnipMate的依赖项|
|MarcWeber/vim-addon-mw-utils|SnipMate的依赖项|
|pangloss/vim-javascript|js的缩进和语法支持|
|scrooloose/syntastic|语法检查工具|
|psf/black|格式化python代码|

## 感谢

- [https://github.com/square/maximum-awesome](https://github.com/square/maximum-awesome)
- [https://github.com/gpakosz/.tmux](https://github.com/gpakosz/.tmux)
- [https://github.com/VSCodeVim/Vim](https://github.com/VSCodeVim/Vim)
