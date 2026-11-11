# Wordbot 生产部署

目标主机：Ubuntu `bd-inst-mwNVXeuFnH`（SSH 别名 `bd`），运行用户/组 `chenquan`，应用根目录 `/home/chenquan/apps/ai_learn`。本文命令供运维人员审核后在服务器执行；本次只新增部署文件，没有执行部署。

## 远端已核实的事实

以下是 2026-11-11 通过 `ssh bd` 只读侦察确认的真实值，两个 unit 的占位符已按此填好：

| 项 | 实测值 | 来源 |
|---|---|---|
| uv 绝对路径 | `/home/chenquan/.local/bin/uv` | `bash -lc 'which uv'` + `ls -l` |
| Go 可执行文件 | `/home/chenquan/apps/ai_learn/service-data/server` | `readlink /proc/501297/exe` |
| Go 工作目录 | `/home/chenquan/apps/ai_learn/service-data` | `readlink /proc/501297/cwd` |
| Go 启动命令 | `./server`（无参数，run.sh 预编译） | `cat /proc/501297/cmdline` |
| 当前监听 | `*:8080`，`/healthz` 返回 200 | `ss -ltnp` + `curl` |
| Python 进程 | `uv run python main.py`，PPID=1（nohup），已连续运行 15 天 | `ps -eo pid,ppid,cmd` |

有两点必须知道：

- **uv 不在非登录 shell 的 PATH 里**，所以 `ExecStart` 必须是绝对路径，写裸 `uv` 会因为 `status=203/EXEC` 起不来。
- **Go 服务监听 `*:8080`（所有网卡），且 API 完全没有鉴权**。从外部探测 `http://157.10.163.14:8080/healthz` 超时不可达，说明安全组已挡住；unit 里仍显式设 `HTTP_ADDR=127.0.0.1:8080` 收敛为回环绑定，纵深防御。

```bash
# 需要复核时重跑（只读，不要 cat .env）：
ssh bd 'bash -lc "which uv"; ss -ltnp | grep 8080; ps -eo pid,ppid,args | grep -E "[u]v run|[m]ain.py|[s]erver"'
```

- `wordbot.service`：`ExecStart=/home/chenquan/.local/bin/uv run python main.py`，已按实测路径填写。systemd 不读取交互 shell 配置，不能写裸 `uv` 或 `~`。
- `wordbot-data.service`：`ExecStart=/home/chenquan/apps/ai_learn/service-data/server`，`WorkingDirectory=.../service-data`，与远端 `/proc` 实测一致。
- Go 使用默认相对路径 `./wordbot.db`（实际由 `.env` 的 `SQLITE_PATH` 覆盖为绝对路径）。工作目录必须与原进程一致，错误 cwd 可能创建新空库。不要因为 Python 使用根目录，就随意把 Go 的 cwd 也改成根目录。
- 两个服务均从 `/home/chenquan/apps/ai_learn/.env` 加载环境；确认它的格式兼容 systemd `EnvironmentFile`（`KEY=value`，不要 shell 命令替换或 `export`）。前缀 `-` 表示文件缺失不阻止启动，不表示应用可以缺少 token 等必需配置。

安装前在服务器应用根目录跑一遍检查，占位符残留就停止：

```bash
cd /home/chenquan/apps/ai_learn
if grep -nE 'REPLACE_WITH|BLOCKED' deploy/*.service; then
    echo '停止：先完成远端启动路径与工作目录确认' >&2
    exit 1
fi
sudo systemd-analyze verify deploy/wordbot.service deploy/wordbot-data.service
```

## 文件及运行约定

- `wordbot.service`：Python long polling，`Type=simple`，退出后每 5 秒重启，stdout/stderr 交给 journald，停止时清理整个进程组，包括 uv 的 Python 子进程。
- `wordbot-data.service`：Go 数据服务，退出后每 5 秒重启，日志也交给 journald。默认 `HTTP_ADDR=127.0.0.1:8080`；`.env` 中同名变量会覆盖它，必须保持回环绑定。
- `logrotate-wordbot`：兼容现有 nohup 和回滚后的两个文件日志。
- `healthcheck.sh`：检查 Go HTTP 200、bot 进程、最近 300 秒日志；成功静默退出 0，失败输出原因并退出 1。

安全加固使用 `NoNewPrivileges`、`PrivateTmp`、`ProtectSystem=full`、`UMask=0077`。保留应用目录、用户目录和 uv 缓存的读写能力，避免阻断 SQLite、虚拟环境或依赖缓存。没有增加入站端口，也没有修改 Python 的 IPv4 传输设置。

主机仅有 **874MB 内存**：Go unit 设置 `GOMEMLIMIT=192MiB` 作为初始预算，可在根目录 `.env` 调整；环境文件优先于 unit 的 `Environment=`。它是 Go 运行时的软目标，**不是进程 RSS 上限**，SQLite 等开销还需另留空间。观察 Python、Go 和系统总内存后调整，避免同时在小内存主机编译 Go 和安装大量 Python 依赖。unit 不在每次启动时编译；核实并准备可执行文件和 uv 环境后再切换。

## 从 nohup 切到 systemd：先停旧，再启新

允许有一次短暂停机；这一流程以不产生双实例为优先。Telegram long polling 不能让两个相同 token 的实例同时运行。以下命令只在上述阻塞项全部消除后执行。

1. 在应用根目录核对 PID 文件和真实进程。`stop.sh` 只检查 PID 是否存在，不核对命令，陈旧 PID 可能指向无关进程。

   ```bash
   cd /home/chenquan/apps/ai_learn
   for f in wordbot.pid service-data.pid; do
       if [ -f "$f" ]; then
           printf '%s: ' "$f"
           cat "$f"
           ps -p "$(cat "$f")" -o user,pid,ppid,args
       fi
   done
   pgrep -a -u chenquan -f 'uv run python main[.]py|python[^ ]* main[.]py|[s]ervice-data|[s]erver' || true
   lsof -nP -iTCP:8080 -sTCP:LISTEN
   ```

   **先清理陈旧的 `wordbot.pid`，再运行 `stop.sh`，且必须在启动新 unit 前完成**：若 PID 不存在或已被复用，确认后删除该陈旧文件；`service-data.pid` 同样处理。若 PID 确实属于旧 uv/bot，则保留它供 `stop.sh` 杀旧进程。不要盲目先删有效 PID，否则 `stop.sh` 会漏停旧 bot。Go 的停止逻辑还会根据 8080 端口找 PID，应先确认监听者确实是本应用。

2. 停止旧进程，并确认 uv 的 Python 子进程也已退出。仅杀 uv 父进程可能留下孤儿 Python。

   ```bash
   ./stop.sh
   pgrep -a -u chenquan -f 'uv run python main[.]py|python[^ ]* main[.]py' || true
   lsof -nP -iTCP:8080 -sTCP:LISTEN
   ```

   若仍有 bot 或 Go 进程，先用 `ps -p <PID> -o user,pid,ppid,args` 核对身份，然后仅对确认的旧进程执行 `kill -TERM <PID>`，等待退出；不要使用宽泛 `pkill python`。**检查结果必须是旧 bot 无残留且 8080 无监听，才能继续。** 清除旧 PID 文件，防止之后误运行 `stop.sh` 时杀到复用 PID：

   ```bash
   rm -f wordbot.pid service-data.pid
   ```

3. 安装 unit，先启动并验收数据服务，再启动 bot。不要同时运行 `run.sh`。

   ```bash
   sudo install -m 0644 deploy/wordbot.service /etc/systemd/system/wordbot.service
   sudo install -m 0644 deploy/wordbot-data.service /etc/systemd/system/wordbot-data.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now wordbot-data.service
   curl --noproxy '*' --fail --silent --show-error http://127.0.0.1:8080/healthz
   sudo systemctl enable --now wordbot.service
   sudo systemctl status --no-pager wordbot-data.service wordbot.service
   ```

   上面的 curl 失败时停止操作并排查，不能继续启 bot。bot 的 `After`/`Wants` 只处理启动顺序与拉起关系，不保证 Go HTTP 已就绪。两者都用 systemd 管理后，日常停止/重启必须使用 `systemctl`；**不要再调用 `run.sh`/`stop.sh`**，后者会按端口杀掉 systemd 管理的 Go，随后 Go 又被 systemd 重启。

## 日志和健康检查

systemd 的标准输出和错误都进入 journald；不再无限追加 `wordbot.log`/`service-data.log`。journald 负责时间、unit、轮转和留存，使用现有主机策略；上线前检查 `journalctl --disk-usage` 和 journald 配置，按磁盘预算设定留存上限（例如持久日志 `SystemMaxUse=100M`、内存日志 `RuntimeMaxUse=32M`）。这是主机全局策略，需要考虑其他服务。需要跨重启保留日志时确认服务器已启用持久 journal。

```bash
sudo journalctl -u wordbot.service -u wordbot-data.service -n 100 --no-pager
sudo journalctl -u wordbot.service -f

# 旧 nohup：默认以 wordbot.log 的非空和 mtime 作为最近写入日志的信号。
PROCESS_MODE=pgrep ./deploy/healthcheck.sh

# 新 systemd：等 bot 第一次输出 polling 日志（通常约 10 秒）后检查。
# 必须切换到 journal，否则历史文件不更新，会在 5 分钟后误报。
sudo env PROCESS_MODE=systemd LOG_SOURCE=journal ./deploy/healthcheck.sh
```

检查 journal 需要日志读取权限；root 可以读取，也可以为告警执行用户配置适当的 journal 读取权限。脚本的 systemctl 模式失败时不回退到 pgrep，避免遗留 nohup 掩盖 unit 故障。`auto` 在系统存在 systemd 运行目录时选 systemctl，否则选 pgrep。文件模式以 mtime 判断近期写入，不解析日志内容；journal 模式判断指定时间窗口内是否有日志。二者是活性信号，不保证每条业务请求成功。

可用环境变量：`APP_DIR`、`HEALTH_URL`、`SERVICE_NAME`、`RUN_USER`、`PROCESS_MODE=auto|systemd|pgrep`、`PROCESS_PATTERN`、`LOG_SOURCE=file|journal`、`LOG_FILE`、`MAX_LOG_AGE=300`、`CURL_TIMEOUT=5`。时间单位为秒，脚本不加载 `.env`。未来接 cron 时使用脚本绝对路径和明确的 `LOG_SOURCE`，当前交付不安装 cron。

**IPv4 保持原样**：Python 已通过 `httpx.AsyncHTTPTransport(local_address="0.0.0.0")` 强制 IPv4 出站，避免 VPS IPv6 路由导致 Telegram 请求失败；不要删除或替换它。Python 的 `WorkingDirectory` 必须为应用根目录，`EnvironmentFile` 必须指向根目录 `.env`。

## 遗留日志轮转

在服务器安装并干跑；以下命令本次未执行：

```bash
sudo install -m 0644 deploy/logrotate-wordbot /etc/logrotate.d/wordbot
sudo logrotate -d /etc/logrotate.d/wordbot
```

配置为 `daily`、保留 7 份、`maxsize 50M`、压缩并延迟一个周期压缩、缺失/空文件跳过，使用 `su chenquan chenquan`。`copytruncate` 必须保留：nohup 持有原日志文件句柄，rename 后无法主动 reopen。复制与截断之间存在少量日志丢失窗口，也需要暂时的复制空间。这里的路径必须与实际日志路径一致。

`maxsize` 仅在 logrotate 被调度执行时检查，不是实时 50MB 硬上限；若增长很快，应提高主机 logrotate 调度频率。`-d` 只读调试，不修改日志或状态；需要实测轮转时可人工执行 `sudo logrotate -v -f /etc/logrotate.d/wordbot`，这会真正轮转，需安排时机。systemd 上线后该规则处理历史文件以及未来回滚的 nohup 日志；journal 本身不通过 logrotate 管理。

## 回滚到 nohup

先停止并禁用 **两个** unit，确认所有进程退出，再运行手工脚本，不能只停 bot：

```bash
cd /home/chenquan/apps/ai_learn
sudo systemctl disable --now wordbot.service wordbot-data.service
sudo systemctl is-active wordbot.service wordbot-data.service  # 预期均 inactive，退出码非 0
pgrep -a -u chenquan -f 'uv run python main[.]py|python[^ ]* main[.]py' || true
lsof -nP -iTCP:8080 -sTCP:LISTEN
# 确认没有残留 bot 和 8080 监听后再继续：
rm -f wordbot.pid service-data.pid
./run.sh
PROCESS_MODE=pgrep LOG_SOURCE=file ./deploy/healthcheck.sh
```

`run.sh` 会重新编译 Go，低内存主机编译可能较慢；不要并发重复启动。它用 `>` 打开日志，会覆盖旧的未轮转文件；需要留存时应先归档。回滚后保留 logrotate 规则，并将未来告警从 journal 模式改回 file 模式。unit 可以留在 `/etc/systemd/system/`，保持 disabled 即可。

## 验证边界

本地为 macOS，没有 `systemd-analyze` 和 `logrotate`，无法在本机完成 unit 语义验证及 logrotate 干跑。已在本机执行 `bash -n deploy/healthcheck.sh` 和隔离的健康检查行为验证；这些不代表 Ubuntu 上的服务已经启动或通过验收。远端路径、Go 工作目录和启动参数待上述只读输出确认后补齐。
