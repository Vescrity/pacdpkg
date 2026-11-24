# pacdpkg

pacman 7.1 的命令行接口，后端是 deb 系（dpkg/apt）。不是「能力表 + 透传」的
wrapper：参数解析、输出排版、字段顺序、退出码都按真 pacman 实现，底层只借
dpkg/apt 干活。

目标环境：Deepin / UOS / Debian / Ubuntu 等 deb 系系统；开发机上用真 pacman
当 oracle 对拍。

## 现状

pacman 7.1 的操作已全部覆盖：

```
-Q -Qq -Qi -Qii -Ql -Qo -Qs -Qe -Qd -Qm -Qn -Qdt -Qdtt -Qu -Qk -Qkk -Qc -Qg -Qp
-S -Ss -Si -Sii -Sl -Sg -Sc -Scc -Sy -Syy -Su -Suu -Sw -Sp --print-format
-R -Rs -Rss -Rn -Rns -Rc -Ru -Rp
-U -Up --needed --downloadonly
-T  -D --asdeps --asexplicit -Dk -Dkk  -F -Fy -Fl -Fx -Fq --machinereadable
```

`-F` 基于 apt 的 Contents 索引（`pacdpkg -Fy` 构建），其余操作都直接映射到 dpkg/apt。

> 注意：Deepin/UOS 的仓库不发布 `Contents-<arch>.gz`，在这些机器上 `-Fy` 会如实报
> “failed to retrieve some files”，`-F` 查询会提示没有 files database。接入发布了
> Contents 的 Debian/Ubuntu 源（或自己构建索引）后即可使用。

`pacman.conf` 的 `VerbosePkgLists`、`IgnorePkg`、`IgnoreGroup`、`NoProgressBar`
已支持，事务摘要会按 pacman 的两种排版输出（classic 列表 / 表格）。

`-h/--help`（含每个操作各自的帮助）、`-V`、getopt_long 的全部行为（短选项簇、
长选项前缀缩写、`--`、参数置换）与真机一致；**80 选项 × 7 操作的合法性
矩阵与 pacman 7.1 560/560 一致**（见 `docs/matrix.md`）。

## 构建

```sh
go build -o bin/pacdpkg ./cmd/pacdpkg     # 或 make build
go test ./...                             # 或 make test
```

默认静态构建（`CGO_ENABLED=0`），无运行时依赖；交叉编译到 deb 机器：

```sh
make cross            # 产出 dist/pacdpkg-linux-amd64
scp dist/pacdpkg-linux-amd64 root@host:/usr/local/bin/pacdpkg
```

## 用法

和 pacman 一样：

```sh
pacdpkg -Q                    # 已安装包列表
pacdpkg -Qi bash              # 包信息（-Qii 含 backup files）
pacdpkg -Ql bash              # 包内文件
pacdpkg -Qo /usr/bin/bash     # 文件属主
pacdpkg -Qs '^bash$'          # 本地搜索（正则）
pacdpkg -Qe                   # 显式安装（apt-mark manual）
pacdpkg -Qd                   # 作为依赖安装（apt-mark auto）
pacdpkg -Qdt                  # 孤儿依赖（-Qdtt 忽略 optdepends）
pacdpkg -Qm                   # 仓库里不存在的包（foreign）
pacdpkg -Qu                   # 可升级
pacdpkg -Qk / -Qkk            # 文件缺失 / 属性校验
pacdpkg -Qc bash              # changelog
pacdpkg -Qp ./foo.deb         # 查询包文件

pacdpkg -Ss '^bash$'          # 仓库搜索（含 [installed] 标记）
pacdpkg -Si bash              # 仓库包信息
pacdpkg -S bash               # 安装（事务模拟 → 摘要 → 确认）
pacdpkg -Su                   # 全系统升级
pacdpkg -Sw bash              # 仅下载
pacdpkg -Sp bash              # 打印下载地址
pacdpkg -R bash               # 卸载（-Rs 连带依赖，-Rn 连配置）
pacdpkg -U ./foo.deb          # 安装本地包
pacdpkg -T bash>=5            # 依赖检查，未满足退出 127
pacdpkg -D --asdeps bash      # 标记为依赖安装
pacdpkg -Fy && pacdpkg -F bin/ls   # 文件数据库查询
```

在 Arch 上也能跑 `-h`/`-V`/参数解析，但查询类命令需要 dpkg 数据库。

## 配置

按顺序读取 `/etc/pacman.conf` 和 `/etc/pacdpkg.conf`（后者覆盖前者；`--config`
指定时只读该文件），使用 pacman 的 `[options]` 方言：

```
[options]
Color                 # tty 上启用彩色输出（与 pacman 的 --color 语义一致）
VerbosePkgLists       # 事务摘要用表格
IgnorePkg = foo bar
IgnoreGroup = grp
HoldPkg = baz
CacheDir = /var/cache/apt/archives
Architecture = amd64  # auto 表示按本机
```

命令行 `--color always|never|auto` 优先于配置。颜色码与 pacman 完全相同。

## 数据来源

本地库、安装原因、文件校验、候选版本、仓库地址、`.deb` 元数据全部直接读文件或归档
（`/var/lib/dpkg/status`、`/var/lib/dpkg/info`、`/var/lib/apt/extended_states`、
`/var/lib/apt/lists`、`/etc/apt/sources.list*`、`/etc/apt/preferences*`、`.deb` 的
ar/tar），不解析任何命令输出。事务计划由内置 resolver 计算，执行时用
`pkg=version` 锁定，apt 只负责搬运与落盘。

## 一致性是怎么保证的

1. **真机 oracle**：本机 pacman 7.1 只读命令 + `-Sp/--print-format` 无需 root，
   输出、stderr、退出码直接对拍
2. **源码对照**：字段排版、`humanize_size`、`dump_pkg_full` 的字段顺序、查询过滤
   逻辑（`filter()`/`display()`）逐段照 `src/pacman/*.c` 实现
3. **矩阵探测**：`docs/matrix.md` 里的 560 组合法性由脚本对真机探测生成，代码中的
   选项表以此为输入
4. **版本比较对拍**：`internal/alpm` 的 dpkg 版本比较有单测；机器上装有 dpkg 时，
   `go test` 会随机生成 300 组版本号与 `dpkg --compare-versions` 交叉验证
5. **deb 端集成**：在 Deepin 容器（root）上跑 `-Q` 全家，含本地 .deb 安装后的
   foreign 判定、`apt-mark` 切换后的 reason 变化、目录属主查询等

## 目录

```
cmd/pacdpkg         入口
internal/cli        getopt_long 仿真、选项表、Config、帮助文本（内嵌真机输出）
internal/alpm       Pkg/Dep 模型、dpkg 版本比较
internal/deb        dpkg/apt 数据库读取、.deb 解析、依赖求解、Contents 索引
internal/render     pacman 排版：字段、列表、大小、日期、搜索结果
internal/app        操作分发与 -Q 实现
docs/matrix.md      选项×操作矩阵与字段映射
```

## 许可

GPL-2.0-or-later，与 pacman 一致。
