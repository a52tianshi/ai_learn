import time
import bisect
import math
import logging
import config
from okx_client import fetch_history_candles, fetch_latest_candles
from notifier import TelegramNotifier

logger = logging.getLogger("tg_okx_monitor")

KEY_OFFSETS = [1, 3, 5, 10, 15, 30, 45, 60, 120, 240, 720, 1440]

def format_label(m: int) -> str:
    if m < 60:
        return f"{m}m"
    if m % 60 == 0:
        return f"{m//60}h"
    return f"{m//60}h{m%60}m"

def color_pct(v: float, width: int, no_color: bool) -> str:
    s = f"{v:+.2f}%"
    # Pad string to target width
    padding = max(0, width - len(s))
    s = " " * padding + s
    if no_color:
        return s
    if v > 0:
        return f"\033[32m{s}\033[0m"  # Green
    elif v < 0:
        return f"\033[31m{s}\033[0m"  # Red
    return s

def arrow(v: float) -> str:
    if v > 0:
        return "🟢"
    elif v < 0:
        return "🔴"
    return "⚪"

class OKXPriceMonitor:
    def __init__(self):
        self.inst_id = config.INST_ID
        self.alert_threshold = config.ALERT_THRESHOLD * 100.0  # percentage (e.g. 3.0%)
        self.tg_every = config.ALERT_COOLDOWN  # periodic report interval in seconds
        self.notifier = TelegramNotifier()
        
        # History store: timestamp_ms -> close_price
        self.history = {}
        self.sorted_timestamps = []
        
        # Timing trackers
        self.last_tg_time = 0
        self.last_alert_time = 0
        
    def initialize_history(self):
        """
        Populate the historical data from OKX.
        """
        candles = fetch_history_candles(self.inst_id, limit_candles=1450)
        if not candles:
            logger.error("Failed to initialize history candles. Will retry during updates.")
            return
            
        for ts, price in candles:
            self.history[ts] = price
            
        self.sorted_timestamps = sorted(self.history.keys())
        logger.info(f"History initialized with {len(self.history)} points.")

    def _prune_history(self, current_ts: int):
        """
        Prunes history older than 1500 minutes from current timestamp.
        """
        cutoff = current_ts - 1500 * 60 * 1000
        keys_to_remove = [k for k in self.history if k < cutoff]
        for k in keys_to_remove:
            del self.history[k]
            
        if keys_to_remove:
            self.sorted_timestamps = sorted(self.history.keys())

    def _get_closest_price(self, target_ts: int, max_diff_ms: int = 120000) -> tuple[int, float] | None:
        """
        Finds the closest price in history to target_ts.
        """
        if not self.sorted_timestamps:
            return None
            
        idx = bisect.bisect_left(self.sorted_timestamps, target_ts)
        
        candidates = []
        if idx < len(self.sorted_timestamps):
            candidates.append(self.sorted_timestamps[idx])
        if idx > 0:
            candidates.append(self.sorted_timestamps[idx - 1])
            
        if not candidates:
            return None
            
        best_ts = min(candidates, key=lambda t: abs(t - target_ts))
        if abs(best_ts - target_ts) <= max_diff_ms:
            return best_ts, self.history[best_ts]
            
        return None

    def tick(self):
        """
        Called every interval to update price, print console table, and handle TG.
        """
        # Check and reply to TG messages first
        self.notifier.check_and_reply_updates()
        
        latest = fetch_latest_candles(self.inst_id, limit=5)
        if not latest:
            logger.warning("Failed to fetch latest candles in this tick. Skipping.")
            return
            
        # Update history
        for ts, price in latest:
            self.history[ts] = price
            
        self.sorted_timestamps = sorted(self.history.keys())
        
        # Current price is from the newest candle
        current_ts, current_price = latest[0]
        
        # Prune old history
        self._prune_history(current_ts)
        
        # Calculate all 1440 offsets
        rows = {}
        max_abs_scaled = 0.0
        
        for k in range(1, 1441):
            target_ts = current_ts - k * 60 * 1000
            res = self._get_closest_price(target_ts)
            if not res:
                rows[k] = {"label": format_label(k), "ok": False}
                continue
                
            hist_ts, hist_price = res
            pct = (current_price - hist_price) / hist_price * 100.0
            
            # Volatility-scaled daily equivalent return: pct * sqrt(1440 / k)
            scaled = pct * math.sqrt(1440.0 / k)
            
            rows[k] = {
                "label": format_label(k),
                "past": hist_price,
                "pct": pct,
                "scaled": scaled,
                "ok": True
            }
            
            if abs(scaled) > max_abs_scaled:
                max_abs_scaled = abs(scaled)

        # 1. Print console output (All 1440 rows as requested)
        now_str = time.strftime('%H:%M:%S', time.localtime(current_ts / 1000))
        lines = [
            f"\n[{now_str}] {self.inst_id} 当前价: {current_price:.2f}",
            f"{'时间前':<8} {'历史价':<12} {'涨跌幅':<10} {'日化折算':<12}"
        ]
        
        for k in range(1, 1441):
            r = rows[k]
            if not r["ok"]:
                lines.append(f"{r['label']:<8} {'-':<12} {'数据不足':<10}")
            else:
                pct_str = color_pct(r["pct"], 8, config.NO_COLOR)
                scaled_str = color_pct(r["scaled"], 10, config.NO_COLOR)
                lines.append(f"{r['label']:<8} {r['past']:<12.2f} {pct_str} {scaled_str}")
                
        # Write entire table to stdout at once
        print("\n".join(lines), flush=True)

        # 2. Telegram Send Logic
        now_sec = time.time()
        
        # Check alerts
        alerted = self.alert_threshold > 0 and max_abs_scaled >= self.alert_threshold
        # Check periodic report
        due = self.tg_every > 0 and (now_sec - self.last_tg_time >= self.tg_every)
        
        # Alert cooldown check (prevent flooding, default 60s cooldown for alerts)
        alert_cooldown_ok = (now_sec - self.last_alert_time >= min(60, self.tg_every)) if self.tg_every > 0 else True
        
        if (alerted and alert_cooldown_ok) or due:
            # Sort rows by absolute value of daily-scaled return descending and take top 10
            valid_rows = [r for r in rows.values() if r.get("ok")]
            sorted_rows = sorted(valid_rows, key=lambda x: abs(x["scaled"]), reverse=True)
            top_10 = sorted_rows[:10]
            
            tg_lines = []
            title = f"📊 {self.inst_id}  当前价 {current_price:.2f}"
            if alerted:
                title = "⚠️ 波动告警  " + title
                
            tg_lines.append(title)
            tg_lines.append(time.strftime('%Y-%m-%d %H:%M:%S', time.localtime(current_ts / 1000)))
            tg_lines.append("<pre>")
            tg_lines.append(f"{'周期':<6} {'涨跌幅':<9} {'日化':<8} {'':<4}")
            
            for r in top_10:
                tg_lines.append(
                    f"{r['label']:<6} {r['pct']:+7.2f}% {r['scaled']:+7.2f}% {arrow(r['scaled'])}"
                )
            tg_lines.append("</pre>")
            
            # Send message
            tg_msg = "\n".join(tg_lines)
            if self.notifier.send_notification(tg_msg, force=True):
                self.last_tg_time = now_sec
                if alerted:
                    self.last_alert_time = now_sec
