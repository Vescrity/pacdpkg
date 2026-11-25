# pacdpkg：pacman 7.1 CLI 与 deb 后端的映射矩阵

基准：本机 **pacman 7.1.0 / libalpm 16.0.1**（`pacman --help`、`pacman -<op> -h`、
`pacman(8)` 以及 pacman 源码）；后端：dpkg 1.22 / apt 2.8（Deepin 25 / UOS）。

本文件的「选项 × 操作」合法性不是手写的，而是对真机 pacman 逐条探测生成的：
80 个选项 token × 7 个操作 = **560 组**，判据是真机 stderr 是否出现
`error: invalid option`。`pacdpkg` 在同一组探测下与真机 **560/560 一致**。

## 操作覆盖状态

| 操作 | 状态 | deb 实现方式 |
|---|---|---|
| `-Q` / `-Qq` | 已实现 | 直接解析 `/var/lib/dpkg/status`（不启动 dpkg），按包名排序 |
| `-Qi` / `-Qii` | 已实现 | 直接解析 `/var/lib/dpkg/status` + `/var/lib/dpkg/info` + copyright；conffiles 在需要时才计算哈希 |
| `-Ql` | 已实现 | 读 `/var/lib/dpkg/info/*.list`，目录补尾斜杠，按路径排序 |
| `-Qo` | 已实现 | 只解析目录部分（与 pacman 的 `lrealpath` 一致），再从各包 `.list` 建索引查属主；比 `dpkg -S` 的文本解析更完整 |
| `-Qs` | 已实现 | 本地库对 name/description 做大小写不敏感正则匹配，仓库标签固定为 `local` |
| `-Qe` / `-Qd` | 已实现 | 直接读 `/var/lib/apt/extended_states` 的 `Auto-Installed` |
| `-Qm` / `-Qn` | 已实现 | 名字是否出现在 apt lists 中（本地库 + Release/pin 自行计算候选） |
| `-Qdt` / `-Qdtt` | 已实现 | 自己算：auto 标记且无反向依赖；`-tt` 忽略 Recommend/Suggest。内核包会被列出（pacman 语义），apt 的 autoremove 会保护它们 |
| `-Qu` | 已实现 | 自己从 apt lists + Release 元数据 + `/etc/apt/preferences` 选 candidate；hold/IgnorePkg → `[ignored]` |
| `-Qk` | 已实现 | 文件列表逐个 `Lstat`，统计缺失数（不哈希） |
| `-Qkk` | 已实现 | 自己比对 `.md5sums` 与 conffiles 哈希，统计变更/缺失 |
| `-Qc` | 已实现 | `/usr/share/doc/<pkg>/changelog.Debian.gz`（或 `changelog.gz`），无则报错但退出码仍为 0（与 pacman 一致） |
| `-Qg` / `-Sg` | 已实现（近似） | deb 无 group 概念，用 `Section` 近似 |
| `-Qp` / `-Qpi` / `-Qpl` | 已实现 | `dpkg-deb -f` 解析控制字段，`dpkg-deb -c` 取文件列表；`Compressed Size` 为文件大小，`Signatures` 固定 `None` |
| `-Ss` / `-Ssq` | 已实现 | 解析 apt lists（压缩文件在进程内解压），多个目标是 AND 关系，匹配包名/描述/Provides/Section，输出 `repo/name ver` 与 `[installed]` |
| `-Si` / `-Sii` | 已实现 | apt lists 字段；`Download Size` 来自 `Size`，`-Sii` 追加 `SHA-256 Sum` 与 `Signatures` |
| `-Sl` | 已实现 | 按仓库列出；仓库标签为 `<suite>/<component>`，flat 仓库取主机名；同一 (仓库, 包, 版本) 去重 |
| `-Sg` / `-Sgg` | 已实现（近似） | 仍以 `Section` 当 group |
| `-Sc` / `-Scc` | 已实现 | 自己清理 `/var/cache/apt/archives`：`-Sc` 保留所有已安装版本（pacman 的 KeepInstalled 语义），`-Scc` 清空 |
| `-Sy` / `-Syy` | 已实现 | `apt-get update` 直接流式输出；`-Syy` 先把旧 lists 移到 `/var/cache/pacdpkg/lists-backup`，失败或索引缺失时精确回滚。apt 在部分仓库失败时仍返回 0，因此额外检查「备份中每个索引是否重新出现」以及「每个 (URI,suite) 至少有一个索引」 |
| `-S` / `-Su` / `-Suu` | 已实现 | libapt 的 `pkgDepCache` / `pkgProblemResolver` 计算计划 → pacman 摘要 → 确认 → 同一份 depcache 走 `pkgPackageManager` 执行；`--needed`、`--asdeps`、`--asexplicit` 已映射 |
| `-Sw` / `-Sp` / `--print-format` | 已实现 | `-Sw` 走 `--download-only` 并提示 `Proceed with download?`；`-Sp` 直接从计划与 sources 拼 URL（与 apt 的镜像选择、百分号转义一致），支持 `%n %v %r %s %a %l %f %d %p %u` |
| `--dbonly` / `--noscriptlet` / `--assume-installed` / `--overwrite` | 明确拒绝 | deb 后端无等价语义（dpkg 只能全局覆盖，无法按路径限定），直接报错而不是静默降级 |
| `--nodeps` | 按操作映射 | `-R` 用 `dpkg -r --force-depends`（apt 会连带删依赖者），`-U` 对纯本地包用 `dpkg -i --force-depends`；`-S` 无等价语义，报错 |
| `--root` / `--dbpath` / `--sysroot` | 明确拒绝 | 会改变操作的数据库根，静默忽略不安全 |
| `--cachedir` / `--arch` / `--config` / `--noconfirm` / `--confirm` / `-v` | 已映射 | `-o Dir::Cache::archives` / `-o APT::Architecture` / pacdpkg.conf 方言 / 单布尔三态 / 配置横幅 |
| `--color` / `-v` / 配置文件 | 已实现 | `--color always/never/auto`；只读取 `/etc/pacdpkg.conf`；`Color` 选项按 pacman 语义（仅在 tty 上生效），颜色码与 pacman 的 `enable_colors()` 完全一致 |
| `--debug` / `--ask` / `--hookdir` / `--gpgdir` / `--logfile` | 仅校验/接受 | 无 hooks；apt 管理自身信任与日志 |
| `-R` / `-Rs` / `-Rss` / `-Rn` / `-Rns` | 已实现 | libapt 标记删除；`-Rs` 用 `MarkAndSweep` 删除不再需要的自动安装包，`-Rn` 走 purge |
| `-Rc` / `-Ru` / `-Rd` | 已实现 | `-Rc` 按删除后仍被满足的依赖级联；`-Ru` 先过滤仍被需要的目标；`-Rdd` 仍走 `dpkg --force-depends` |
| `-R` 依赖保护 | 已实现 | 删除会破坏未删除包的依赖时，按 pacman 拒绝事务并打印 `removing X breaks dependency ...` |
| `-Rp` / `--print-format` | 已实现 | `name-version` / `%n %v %r %l`；`-n` + `-p` 按 pacman 报冲突 |
| `-U` / `-Udd` / `-U --needed` | 已实现 | 本地 deb 通过 `debDebPkgFileIndex` 加进 libapt 缓存后求解，执行走 `pkgDPkgPM`；`-Udd` 仍走 `dpkg --force-depends` |
| `-Up` / `--print-format` | 已实现 | 默认输出文件路径；`%a %d %p %u` 来自包的 control 字段 |
| `-T` | 已实现 | 解析 pacman 依赖语法（`= >= <= > <`），支持 Provides，未满足输出并以 **127** 退出 |
| `-D --asdeps` / `--asexplicit` | 已实现 | `apt-mark auto/manual`，成功提示与 `-q` 抑制同 pacman |
| `-Dk` | 已实现 | 直接扫描 status 的非干净状态；无问题输出 `No database errors have been found!` |
| `-Dkk` | 已实现 | 用本地库自己做依赖满足检查，输出 pacman 的 `missing '<dep>' dependency for '<pkg>'` |
| `-F` / `-Fq` / `-Fx` / `--machinereadable` | 已实现 | 基于 apt Contents 索引；含 `/` 的目标按完整路径精确匹配，否则匹配 basename，`-Fx` 正则作用于同一范围 |
| `-Fl` | 已实现 | 从 Contents 索引按包列出文件；`-Fl repo/pkg` 支持含斜杠的仓库标签 |
| `-Fy` / `-Fyy` | 已实现 | 优先复用 apt 已维护的 `Contents-*`（`50apt-file.conf` 启用后由 `apt-get update` 更新），没有时按 Release 声明的压缩格式（bz2/xz/zst/gz/lz4/未压缩）下载；`-Fyy` 强制走网络。发布方没有 Contents 的仓库只报一条警告，至少一个仓库成功即返回 0 |

## 字段映射（`-Qi` / `-Si`）

| pacman 字段 | deb 来源 | 说明 |
|---|---|---|
| Version | `Version` | 原样保留 `[epoch:]upstream-revision`，比较时按 dpkg 规则 |
| Architecture | `Architecture` | `amd64` 等，不做 `x86_64` 转换 |
| Description | `Description` 首行 | 仅 synopsis |
| URL | `Homepage` | |
| Licenses | `/usr/share/doc/<pkg>/copyright` 的 `License:` 行 | 取不到时为 `None` |
| Groups | `Section` | 明确偏差 |
| Provides | `Provides` | |
| Depends On | `Depends` + `Pre-Depends` | 约束改写为 pacman 语法（`>=`），`a \| b` 保留为 `a\|b` |
| Optional Deps | `Recommends` + `Suggests` | deb 无描述文本，只有包名与 `[installed]` 标记 |
| Required By | 本地库反向 `Depends`/`Pre-Depends`（含 Provides 匹配） | 排序去重 |
| Optional For | 本地库反向 `Recommends`/`Suggests` | |
| Conflicts With | `Conflicts` + `Breaks` | pacman 无 Breaks，合并展示 |
| Replaces | `Replaces` | |
| Installed Size | `Installed-Size`（KiB→字节） | 自动选单位；`-Si` 额外强制与 Download Size 同单位（照抄 pacman 的 `humanize_size(isize, label[0])`） |
| Packager | `Maintainer` | |
| Build Date | 无 | 输出 `None`（deb 不记录构建时间） |
| Install Date | `/var/lib/dpkg/info/<pkg>.list` 的 mtime | 近似 |
| Install Reason | `/var/lib/apt/extended_states` 的 `Auto-Installed` | 读不到时输出 `Unknown` 且不回填 |
| Install Script | maintainer scripts 是否存在 | `preinst/postinst/prerm/postrm/config` |
| Validated By | 无 | 输出 `Unknown`（pacman 在无签名时也输出 `Unknown`） |
| Backup Files（`-Qii`） | `Conffiles` 字段 | 当前文件 md5 与记录一致 → `[unmodified]`，否则 `[modified]` |
| Extended Data（`-Qii`） | 固定 | `pkgtype=deb` |

## 输出与退出码

- 字段标题左对齐到 15 字符（pacman 7.1 最长标题为 `Compressed Size`），后接 ` : `
- 空列表输出 `None`；`Optional Deps` 每项一行，续行缩进 18 空格
- 大小格式化完全照抄 `humanize_size`：目标单位为空时取第一个使数值 `<= 2048` 的单位
- 日期格式为 C locale 的 `%c`：`Mon Jan _2 15:04:05 2006`
- 非 tty 输出不换行（`cols = 0`），便于管道
- 退出码：成功 0；未找到包 / 无匹配 / 文件无属主 `1`；`-T` 依赖未满足 `127`（M5）
- 错误前缀 `error:`、警告前缀 `warning:`；getopt 级错误前缀为程序名

## 已知偏差

1. `Groups` 用 `Section` 近似，`-Qg` 的「组未找到」语义随之改变
2. deb 无 AUR、无 `--aur`、无 hooks、无 `-F` 的独立文件库（需 Contents 索引）
3. `-Qu` 的候选版本取 apt 的 Candidate，因此受 pin/hold/phased-update 影响，和纯「sync db 最高版本」略有差异；held 包按 pacman 习惯标 `[ignored]`
4. `Build Date`、`Validated By` 无对应，输出 `None` / `Unknown`
5. `-Qo` 对目录的行为与 pacman 一致（列出所有属主），但 deb 的目录属主信息来自 `.list`，与实际目录内容无关
6. `-S` 的组目标与 provides 目标不做展开：deb 的 group 只是 Section 近似，按组安装会牵动成百上千个包，因此按 `target not found` 处理
7. `-R` 的依赖保护按 pacman 拒绝会破坏依赖的删除；但 `-Rp` 的输出顺序取 apt 的字母序，pacman 是删除列表的反向顺序
8. `-U` 的本地包没有构建时间与签名信息，`Build Date` 显示 `None`、`Signatures` 显示 `None`；`%a/%d/%p/%u` 取自 deb 的 control 字段
9. `-F` 的 Contents 索引不含版本，版本号从 apt lists 尽力补全；索引来自哪个源就显示哪个源的版本，可能与本机 apt 列表版本不同
10. `-F` 优先复用 apt 维护的 Contents 索引；没有索引时按 Release 声明的压缩格式下载。从未执行过 `apt-get update` 的机器可能发现不了仓库
11. 颜色输出未实现（全部纯文本）；`--color` 仅校验取值

## 选项 × 操作合法性（真机探测结果）

### `-D, --database`

接受：`-b` `-k` `-q` `-r` `-v` `--dbpath` `--check` `--quiet` `--root` `--sysroot` `--verbose` `--noconfirm` `--confirm` `--config` `--debug` `--ask` `--cachedir` `--hookdir` `--asdeps` `--logfile` `--asexplicit` `--arch` `--gpgdir` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

### `-F, --files`

接受：`-b` `-d` `-l` `-p` `-q` `-r` `-v` `-x` `-y` `--dbpath` `--nodeps` `--list` `--print` `--quiet` `--root` `--sysroot` `--regex` `--machinereadable` `--verbose` `--refresh` `--noconfirm` `--confirm` `--config` `--assume-installed` `--debug` `--noprogressbar` `--noscriptlet` `--ask` `--cachedir` `--hookdir` `--logfile` `--arch` `--print-format` `--gpgdir` `--dbonly` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

### `-Q, --query`

接受：`-b` `-c` `-d` `-e` `-g` `-i` `-k` `-l` `-m` `-n` `-o` `-p` `-q` `-r` `-s` `-t` `-u` `-v` `--dbpath` `--changelog` `--deps` `--explicit` `--groups` `--info` `--check` `--list` `--foreign` `--native` `--owns` `--file` `--quiet` `--root` `--sysroot` `--search` `--unrequired` `--upgrades` `--verbose` `--noconfirm` `--confirm` `--config` `--debug` `--ask` `--cachedir` `--hookdir` `--logfile` `--arch` `--gpgdir` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

### `-R, --remove`

接受：`-b` `-c` `-d` `-n` `-p` `-r` `-s` `-u` `-v` `--dbpath` `--cascade` `--nodeps` `--nosave` `--print` `--root` `--sysroot` `--recursive` `--unneeded` `--verbose` `--noconfirm` `--confirm` `--config` `--assume-installed` `--debug` `--noprogressbar` `--noscriptlet` `--ask` `--cachedir` `--hookdir` `--logfile` `--arch` `--print-format` `--gpgdir` `--dbonly` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

### `-S, --sync`

接受：`-b` `-c` `-d` `-g` `-i` `-l` `-p` `-q` `-r` `-s` `-u` `-v` `-w` `-y` `--dbpath` `--clean` `--nodeps` `--groups` `--info` `--list` `--print` `--quiet` `--root` `--sysroot` `--search` `--sysupgrade` `--verbose` `--downloadonly` `--refresh` `--noconfirm` `--confirm` `--config` `--ignore` `--assume-installed` `--debug` `--overwrite` `--noprogressbar` `--noscriptlet` `--ask` `--cachedir` `--hookdir` `--asdeps` `--logfile` `--ignoregroup` `--needed` `--asexplicit` `--arch` `--print-format` `--gpgdir` `--dbonly` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

### `-T, --deptest`

接受：`-b` `-r` `-v` `--dbpath` `--root` `--sysroot` `--verbose` `--noconfirm` `--confirm` `--config` `--debug` `--ask` `--cachedir` `--hookdir` `--logfile` `--arch` `--gpgdir` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

### `-U, --upgrade`

接受：`-b` `-d` `-p` `-r` `-v` `-w` `--dbpath` `--nodeps` `--print` `--root` `--sysroot` `--verbose` `--downloadonly` `--noconfirm` `--confirm` `--config` `--ignore` `--assume-installed` `--debug` `--overwrite` `--noprogressbar` `--noscriptlet` `--ask` `--cachedir` `--hookdir` `--asdeps` `--logfile` `--ignoregroup` `--needed` `--asexplicit` `--arch` `--print-format` `--gpgdir` `--dbonly` `--color` `--disable-download-timeout` `--disable-sandbox` `--disable-sandbox-filesystem` `--disable-sandbox-syscalls`

## 数据来源（零输出依赖）

pacdpkg 不解析任何命令输出。查询数据直接读文件；事务计划由 libapt 从同一份
缓存计算，执行走 `pkgPackageManager`，不会再让 apt 二次求解：

| 数据 | 来源 |
|---|---|
| 已安装包、状态、conffiles | `/var/lib/dpkg/status` |
| 安装日期、脚本、文件列表、md5sums | `/var/lib/dpkg/info/*` |
| 安装原因 | `/var/lib/apt/extended_states` |
| held | status 的 `Status: hold ...` |
| 仓库包、版本、尺寸、依赖 | `/var/lib/apt/lists/*_Packages` |
| 仓库元数据（suite/codename/NotAutomatic） | `/var/lib/apt/lists/*_InRelease` |
| 候选版本 | libapt 缓存；pacdpkg 覆写为仓库最高版本，忽略 pin/phasing |
| Contents（`-F`） | apt Contents 索引（gzip 文本） |
| `.deb` 元数据与文件列表 | Go 直接解析 ar + control.tar/data.tar（gz/xz/zstd） |
| 仓库地址与 Contents URL | 解析 `/etc/apt/sources.list` 与 `sources.list.d/*`（单行与 deb822） |
| 架构 | 从 status 统计 |

仍然会调用的命令只有**动作类**，输出不解析、只看退出码：`apt-get update`、`dpkg --force-depends`（仅 `-Rdd`/`-Udd`）、`apt-mark auto/manual`（`-D`）。

事务计划由 `internal/aptplan` 通过 libapt 的 `pkgDepCache` 计算；依赖闭包、alternatives、Provides、Conflicts/Breaks/Replaces、多架构都由 libapt 处理。候选版本在求解前统一改成仓库里的最高版本，因此 pin 和 phased updates 不生效。执行用同一份 depcache 交给 `pkgPackageManager`，安装原因由 depcache 写回 `/var/lib/apt/extended_states`。

## 事务求解的已知偏差

1. 不遵循 apt 的 pin 和 phased updates：候选版本取仓库里的最高版本
2. Recommends/Suggests 不安装（pacman 的 optdepends 语义）
3. 与已安装包冲突或替换时按 pacman 的 `Remove ...?` / `Replace ...?` 提问；为了依赖而删除未点名的包会直接拒绝
4. `-Su <targets>` 目前只升级指定目标，不做「先全系统升级再处理目标」
5. 查询用的仓库索引仍只加载本机架构；事务由 libapt 处理多架构
6. `-T` / `-Dkk` / `-Qdt` 仍使用 Go 侧依赖模型，尚未迁移到 libapt

## 事务细节

- `-Syu <targets>`：先做全系统升级，再处理指定目标（与 pacman 一致），不会变成局部升级
- Held（`apt-mark hold`）的包在 `-Su` 中跳过并告警；显式 `-S` 指定时用 `--allow-change-held-packages` 放行
- 依赖不会被静默降级：依赖要求的版本低于已安装版本时报错
- `Breaks` 与 `Conflicts` 分开处理：`Breaks` 会尝试把被破坏的包升级到安全版本，达不到才报错；`Replaces` 不再被当作删除（Debian 的 Replaces 是文件接管）
- 带版本约束的依赖按每个替代项自己的约束判定；未声明版本的 `Provides` 只满足无版本约束的依赖
- `Pin-Priority <= 0` 的候选不会被选中；pin 规则按 apt 的“首个特定 pin + 通用 pin 取最大”
- `-S repo/pkg` 与 `-S pkg=version` 都会把候选限制在指定仓库/版本内
- 有 `Replaces` 关系时计划里会包含被替换包的删除，执行时与安装放在同一次 apt 调用里
- `--nodeps` 的删除走 `dpkg -r --force-depends`（apt 会连带删依赖者），`-U` 的纯本地包走 `dpkg -i --force-depends`
- `-Qkk` 会跳过 dpkg diversion 的路径（与 `dpkg -V` 一致），逐文件输出 `(Checksum mismatch)` / `(No such file or directory)`，不再中途中断
- `-Sc` 尊重 `--cachedir`/`CacheDir`，持 apt 缓存锁，跳过 `lock`
- 事务后按“显式目标→manual、其余沿用事务前状态”回填安装原因；读不到 `extended_states` 时跳过回填（`-Rs` 也会拒绝推断依赖）
