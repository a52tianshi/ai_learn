# OKX ETH/USDT Telegram Price Monitor

这是一个纯 Python 编写的轻量无状态监控程序，用于实时监听 OKX 交易所上的 ETH/USDT 价格，并根据历史波动折算出的日涨跌幅进行 Telegram 预警通知。

## 核心逻辑
1. **历史数据初始化**：程序启动时，通过 OKX 历史 K 线 API 自动拉取最近 24 小时内（共计 1450 个）的 1 分钟线，建立初始的价格历史字典。
2. **每 10 秒轮询（无状态）**：
   - 每 10 秒拉取 OKX 最新的 5 条 K 线，实时更新当前价格与本地的价格历史字典。
   - 自动清理超过 1500 分钟（25 小时）前的历史数据以维持内存稳定。
3. **价格对比与日涨幅折算**：
   - 将当前最新价 $P_{now}$ 与 $1$ 分钟前至 $1440$ 分钟前（共 1440 个时间点）的 K 线收盘价 $P_k$ 进行对比。
   - 如果对比点数据缺失，采用二分查找在 $\pm 2$ 分钟的范围内寻找最接近的历史时间点价格。
   - 对每个时间点（$k$ 分钟前），将实际价格变化率线性折算为**等效日涨幅/跌幅**（以 1440 分钟为一日）：
     $$\text{daily\_rate} = \frac{P_{now} - P_k}{P_k} \times \frac{1440}{k}$$
4. **消息通知与防刷合并**：
   - 检查这 1440 个时间点的折算值，如果任意一处绝对值超过设定的阈值（默认 5%），则触发通知。
   - 同一次检查中触发的所有预警点会融合成一条汇总消息，仅展示**最大折算涨幅**和**最大折算跌幅**。
   - 设有消息防刷冷却机制（默认 5 分钟），在冷却时间内抑制重复警报，并在控制台打印节流日志。

## 环境准备

### 安装依赖
程序只需要 `httpx` 和 `python-dotenv` 两个第三方包。

你可以直接使用外层项目的虚拟环境运行，或者在当前目录中通过以下命令安装：
```bash
pip install -r requirements.txt
```

### 配置文件
复制 `.env.example` 并重命名为 `.env`，然后填写您的 Telegram Bot 配置：
```env
# Telegram Bot Token (从 @BotFather 获取)
TG_BOT_TOKEN=your_bot_token

# 您的 Telegram 账户 Chat ID (可通过 @userinfobot 获取)
TG_CHAT_ID=your_chat_id

# 监控的交易对 (默认 ETH-USDT)
INST_ID=ETH-USDT

# 轮询间隔 (秒, 默认 10)
CHECK_INTERVAL=10

# 日涨幅报警阈值百分比 (默认 5.0 即 5%)
ALERT_THRESHOLD=5.0

# 警报冷却时间 (秒, 默认 300 即 5分钟)
ALERT_COOLDOWN=300

# 日志级别 (DEBUG / INFO / WARNING / ERROR)
LOG_LEVEL=INFO
```
*注：如果不配置 `TG_BOT_TOKEN` 或 `TG_CHAT_ID`，程序仍将正常运行并将预警消息输出到本地控制台日志中。*

## 运行程序

### 1. 从项目根目录运行
```bash
python -m sub_agent_tg_okx.main
```
如果使用 uv：
```bash
uv run python -m sub_agent_tg_okx.main
```

### 2. 在 `sub_agent_tg_okx` 目录下直接运行
```bash
cd sub_agent_tg_okx
python main.py
```

### 3. 云端后台长驻运行 (通过 sh 脚本)
我已经在项目目录中为您编写了后台控制脚本 `run.sh` 和 `stop.sh`：

*   **启动后台监控**（自动创建 `monitor.pid` 并将日志重定向到 `monitor.log`）：
    ```bash
    ./run.sh
    ```
*   **查看实时日志**：
    ```bash
    tail -f monitor.log
    ```
*   **优雅关闭后台监控**：
    ```bash
    ./stop.sh
    ```

