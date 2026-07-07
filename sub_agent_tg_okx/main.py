import time
import signal
import sys
import os
import logging

# Add current folder to sys.path to allow running as a standalone script
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import config
from monitor import OKXPriceMonitor

# Set up logging
logging.basicConfig(
    level=getattr(logging, config.LOG_LEVEL.upper(), logging.INFO),
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    handlers=[
        logging.StreamHandler(sys.stdout)
    ]
)

logger = logging.getLogger("tg_okx_monitor")

running = True

def handle_signal(signum, frame):
    global running
    logger.info(f"Received stop signal ({signum}). Exiting gracefully...")
    running = False

def main():
    global running
    
    # Register signal handlers for graceful exit
    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)
    
    logger.info("Initializing OKX Telegram Price Monitor...")
    logger.info(f"Target Instrument: {config.INST_ID}")
    logger.info(f"Check Interval: {config.CHECK_INTERVAL}s")
    logger.info(f"Alert Threshold: {config.ALERT_THRESHOLD * 100:.2f}% (Daily equivalent)")
    logger.info(f"Alert Cooldown: {config.ALERT_COOLDOWN}s")
    
    monitor = OKXPriceMonitor()
    
    # 1. Fetch history
    try:
        monitor.initialize_history()
    except Exception as e:
        logger.error(f"Failed to initialize history: {e}", exc_info=True)
        # We don't crash, we'll try to recover or fetch on subsequent ticks
        
    logger.info("Starting monitor loop...")
    
    while running:
        start_time = time.time()
        
        try:
            monitor.tick()
        except Exception as e:
            logger.error(f"Error during monitor tick: {e}", exc_info=True)
            
        # Calculate time to sleep to maintain a steady check interval
        elapsed = time.time() - start_time
        sleep_time = max(0.1, config.CHECK_INTERVAL - elapsed)
        
        # Sleep in small chunks to respond quickly to termination signals
        steps = int(sleep_time / 0.5)
        for _ in range(steps):
            if not running:
                break
            time.sleep(0.5)
            
        # Sleep remaining fraction
        rem = sleep_time % 0.5
        if running and rem > 0:
            time.sleep(rem)
            
    logger.info("Monitor stopped. Goodbye!")

if __name__ == "__main__":
    main()
