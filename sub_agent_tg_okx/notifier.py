import httpx
import time
import logging
import sys
import config

logger = logging.getLogger("tg_okx_monitor")

class TelegramNotifier:
    def __init__(self):
        self.bot_token = config.TG_BOT_TOKEN
        self.chat_id = config.TG_CHAT_ID
        self.cooldown = config.ALERT_COOLDOWN
        self.last_alert_time = 0
        
        # Determine if TG is enabled
        self.tg_enabled = bool(self.bot_token)
        self.offset = None
        
        if self.tg_enabled and not self.chat_id:
            try:
                detected_id = self.auto_detect_chat_id()
                if detected_id:
                    self.chat_id = detected_id
                    logger.info(f"✓ Telegram chat_id 自动探测成功 = {self.chat_id}")
                else:
                    logger.warning("⚠️ 无法自动探测 chat_id，请先在 Telegram 给 bot 发一条任意消息后再试（或通过 -chat 指定）")
                    self.tg_enabled = False
            except Exception as e:
                logger.warning(f"⚠️ 自动探测 chat_id 发生异常: {e}. 将仅在本地输出。")
                self.tg_enabled = False
                
        if not self.tg_enabled:
            logger.warning(
                "Telegram 推送未启用（Token 或 Chat ID 未配置，或者自动探测失败）。"
                "数据将仅在终端和日志输出。"
            )
            
    def auto_detect_chat_id(self) -> str | None:
        """
        Polls the Telegram getUpdates endpoint to automatically find the latest chat ID.
        """
        if not self.bot_token:
            return None
            
        url = f"https://api.telegram.org/bot{self.bot_token}/getUpdates"
        try:
            resp = httpx.get(url, timeout=10)
            if resp.status_code != 200:
                logger.error(f"Telegram getUpdates 返回异常: HTTP {resp.status_code}")
                return None
                
            data = resp.json()
            if not data.get("ok"):
                logger.error(f"Telegram API 错误: {data.get('description')}")
                return None
                
            results = data.get("result", [])
            for r in reversed(results):
                message = r.get("message", {})
                chat = message.get("chat", {})
                chat_id = chat.get("id")
                if chat_id:
                    return str(chat_id)
            return None
        except Exception as e:
            logger.error(f"获取 updates 失败: {e}")
            return None

    def send_notification(self, text: str, force: bool = False) -> bool:
        """
        Sends message to Telegram and logs it. Returns True if sent successfully.
        """
        if not self.tg_enabled or not self.chat_id:
            return False
            
        now = time.time()
        
        # Check cooldown (unless forced by alert)
        if not force and now - self.last_alert_time < self.cooldown:
            return False
            
        url = f"https://api.telegram.org/bot{self.bot_token}/sendMessage"
        payload = {
            "chat_id": self.chat_id,
            "text": text,
            "parse_mode": "HTML"
        }
        
        try:
            resp = httpx.post(url, json=payload, timeout=10)
            if resp.status_code == 200:
                self.last_alert_time = now
                logger.info("  ↳ 已推送 TG")
                return True
            else:
                logger.error(f"Failed to send TG notification: HTTP {resp.status_code}, Response: {resp.text}")
                return False
        except Exception as e:
            logger.error(f"Exception during sending TG notification: {e}")
            return False



    def check_and_reply_updates(self):
        """
        Polls for new messages and replies with confirmation.
        """
        if not self.tg_enabled or not self.bot_token:
            return
            
        url = f"https://api.telegram.org/bot{self.bot_token}/getUpdates"
        params = {"timeout": 0}
        if self.offset is not None:
            params["offset"] = self.offset
            
        try:
            resp = httpx.get(url, params=params, timeout=5)
            if resp.status_code != 200:
                return
                
            data = resp.json()
            if not data.get("ok"):
                return
                
            results = data.get("result", [])
            for r in results:
                self.offset = r["update_id"] + 1
                
                message = r.get("message", {})
                chat = message.get("chat", {})
                chat_id = chat.get("id")
                text = message.get("text", "")
                
                if chat_id and text:
                    logger.info(f"Received TG message from {chat_id}: '{text}'")
                    reply_text = f"程序在跑: 刚收到 {text}"
                    
                    # Send response back
                    reply_url = f"https://api.telegram.org/bot{self.bot_token}/sendMessage"
                    payload = {
                        "chat_id": chat_id,
                        "text": reply_text
                    }
                    httpx.post(reply_url, json=payload, timeout=5)
                    logger.info(f"Replied to {chat_id} with running confirmation.")
        except Exception as e:
            logger.error(f"Error checking/replying TG updates: {e}")
