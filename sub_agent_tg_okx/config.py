import os
import argparse
from pathlib import Path
from dotenv import load_dotenv

# Load env variables from parent/root and local
local_env = Path(__file__).resolve().parent / ".env"
parent_env = Path(__file__).resolve().parent.parent / ".env"

if parent_env.exists():
    load_dotenv(parent_env)
if local_env.exists():
    load_dotenv(local_env, override=True)
if not parent_env.exists() and not local_env.exists():
    load_dotenv()

# Setup argument parser
parser = argparse.ArgumentParser(description="OKX Price Monitor")
parser.add_argument("-inst", default=os.getenv("TG_INST", os.getenv("INST_ID", "ETH-USDT")), help="Instrument ID (e.g. ETH-USDT)")
parser.add_argument("-interval", type=int, default=int(os.getenv("CHECK_INTERVAL", "10")), help="Console refresh interval in seconds")
parser.add_argument("-tg-every", type=int, default=int(os.getenv("ALERT_COOLDOWN", "300")), help="TG notification interval in seconds, 0 = disabled")
parser.add_argument("-token", default=os.getenv("TG_BOT_TOKEN", "8756472344:AAErQT7k-3XlWBey7srrutZvkJvZp-fCQ6c"), help="Telegram bot token")
parser.add_argument("-chat", default=os.getenv("TG_CHAT_ID", ""), help="Telegram chat ID (leaves empty for auto-detection)")
parser.add_argument("-alert", type=float, default=float(os.getenv("ALERT_THRESHOLD", "5.0")), help="Alert threshold percentage (e.g. 5.0 for 5%)")
parser.add_argument("-no-color", action="store_true", default=os.getenv("NO_COLOR", "").lower() in ("true", "1"), help="Disable terminal colors")

args, unknown = parser.parse_known_args()

INST_ID = args.inst
CHECK_INTERVAL = args.interval
ALERT_COOLDOWN = args.tg_every
TG_BOT_TOKEN = args.token
TG_CHAT_ID = args.chat
ALERT_THRESHOLD = args.alert / 100.0  # Convert percentage to decimal (e.g. 3.0 -> 0.03)
NO_COLOR = args.no_color
LOG_LEVEL = os.getenv("LOG_LEVEL", "INFO")
