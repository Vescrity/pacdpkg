# pacdpkg

在 Debian 系系统上提供 pacman 7.1 的命令行接口。参数解析、输出格式、字段顺序和退出码
按 pacman 实现，包的安装和卸载交给 dpkg / apt。

适用于 Deepin、UOS、Debian、Ubuntu 等发行版。

## 构建与安装

```sh
make build      # bin/pacdpkg，静态链接，无运行时依赖
make test
make install    # 安装到 /usr/local，含补全脚本
make cross      # dist/pacdpkg-linux-{amd64,arm64}
```

`make install` 支持 `PREFIX`、`DATADIR`、`BASH_COMPDIR`、`ZSH_COMPDIR`，
打包时按发行版布局覆盖即可。

## 用法

命令和选项与 pacman 相同，`pacdpkg -h` 及各操作的 `-h` 可以看完整列表。

```sh
pacdpkg -Q                    # 已安装的包
pacdpkg -Qi bash              # 包信息，-Qii 附带配置文件
pacdpkg -Ql bash              # 包内文件列表
pacdpkg -Qo /bin/bash         # 文件属于哪个包
pacdpkg -Qs bash              # 在本地库里搜索
pacdpkg -Qdt                  # 不再被依赖的包
pacdpkg -Qk / -Qkk            # 检查文件是否缺失、是否被改动
pacdpkg -Qc bash              # changelog

pacdpkg -Ss bash              # 在仓库里搜索
pacdpkg -Si bash              # 仓库包信息
pacdpkg -S bash               # 安装，先给出事务摘要再确认
pacdpkg -Su                   # 升级整个系统
pacdpkg -Sw bash              # 只下载不安装
pacdpkg -Rns bash             # 卸载，连依赖和配置文件一起清掉

pacdpkg -U ./foo.deb          # 安装本地 deb
pacdpkg -Fy                   # 下载 Contents 索引
pacdpkg -F bin/ls             # 反查文件在哪个包里
```

大部分操作需要 root，查询类不需要。

## 配置

配置文件是 `/etc/pacdpkg.conf`，用 `--config` 可以指定别的文件。支持这些
`[options]` 项：

```
Color                 # 在终端上启用颜色
VerbosePkgLists       # 事务摘要用表格
IgnorePkg = foo bar
IgnoreGroup = grp
HoldPkg = baz
CacheDir = /var/cache/apt/archives
Architecture = amd64  # auto 表示跟随本机
NoProgressBar
```

`--color always|never|auto` 优先于配置文件，颜色码与 pacman 一致。

## Shell 补全

```
completions/pacdpkg.bash   → $(DATADIR)/bash-completion/completions/pacdpkg
completions/_pacdpkg       → $(ZSH_COMPDIR)/_pacdpkg
```

补全脚本改写自 pacman 自带的那份：操作和选项从同一张表里取，`-Q/-R/-D/-T`
补已安装的包名，`-S` 补可用的包名（结果缓存在 `~/.cache/pacdpkg`，apt lists
更新后失效），`-Sl/-Si` 补仓库名，`-Qg` 补组名。

## 实现

包信息、安装原因、文件校验、候选版本、仓库地址和 `.deb` 元数据都直接从
dpkg 与 apt 的数据文件或归档里读（`/var/lib/dpkg/status`、`/var/lib/dpkg/info`、
`/var/lib/apt/extended_states`、`/var/lib/apt/lists`、`sources.list`、
`preferences`、deb 的 ar/tar），压缩的仓库列表在进程内解压，不解析命令输出。

事务由内置的求解器算出完整的安装、升级、卸载计划，确认后以 `pkg=version`
的形式交给 `apt-get`，避免 apt 在两次计算之间选到别的版本。

## 与 pacman 的差异

- Debian 没有包组，`-Sg` / `-Qg` 用 apt 的 section 代替。
- `-F` 依赖仓库发布的 Contents 索引。Deepin 和 UOS 的仓库不发布，`-Fy` 会逐个提示
  这些仓库没有文件索引，`-F` 也就查不到结果。
- Recommends 和 Suggests 映射成 optdepends，`-Qdtt` 据此判断孤儿包。
- `-Qc` 读 `/usr/share/doc/<pkg>/changelog.Debian.gz`，没有时提示 changelog 不可用。
- 只读取本机架构的包。

## 许可

GPL-2.0-or-later，与 pacman 一致。
