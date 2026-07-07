import httpx
import time
import logging

logger = logging.getLogger("tg_okx_monitor")

OKX_API_BASE = "https://www.okx.com"

def fetch_history_candles(inst_id: str, limit_candles: int = 1450) -> list[tuple[int, float]]:
    """
    Fetches historical candles from OKX /api/v5/market/history-candles.
    Returns a list of tuples: (timestamp_ms, close_price), sorted ascending by timestamp.
    """
    url = f"{OKX_API_BASE}/api/v5/market/history-candles"
    params = {
        "instId": inst_id,
        "bar": "1m",
        "limit": "100"
    }
    
    all_candles = []
    after = None
    
    logger.info(f"Starting to fetch {limit_candles} historical 1m candles for {inst_id}...")
    
    while len(all_candles) < limit_candles:
        if after:
            params["after"] = after
            
        try:
            resp = httpx.get(url, params=params, timeout=10)
            if resp.status_code != 200:
                logger.error(f"Failed to fetch history candles: HTTP {resp.status_code}")
                break
                
            data = resp.json()
            if data.get("code") != "0":
                logger.error(f"OKX API error: {data.get('msg')} (code: {data.get('code')})")
                break
                
            candles = data.get("data", [])
            if not candles:
                logger.warning("No more candles returned by OKX API.")
                break
                
            # OKX candles: [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm]
            # Extract (ts, close_price)
            for c in candles:
                ts = int(c[0])
                close_price = float(c[4])
                all_candles.append((ts, close_price))
                
            logger.info(f"Retrieved {len(candles)} candles. Total collected: {len(all_candles)}")
            
            # The last candle in the list is the oldest
            after = candles[-1][0]
            
            # Avoid hitting rate limits (20 requests per 2 seconds, i.e., 10 req/sec)
            time.sleep(0.15)
            
        except Exception as e:
            logger.error(f"Exception during fetching history candles: {e}", exc_info=True)
            break
            
    # Sort by timestamp ascending (oldest first)
    all_candles.sort(key=lambda x: x[0])
    return all_candles

def fetch_latest_candles(inst_id: str, limit: int = 5) -> list[tuple[int, float]]:
    """
    Fetches the latest candles from OKX /api/v5/market/candles.
    Returns a list of tuples: (timestamp_ms, close_price) in descending order of timestamp (newest first).
    """
    url = f"{OKX_API_BASE}/api/v5/market/candles"
    params = {
        "instId": inst_id,
        "bar": "1m",
        "limit": str(limit)
    }
    
    try:
        resp = httpx.get(url, params=params, timeout=5)
        if resp.status_code != 200:
            logger.error(f"Failed to fetch latest candles: HTTP {resp.status_code}")
            return []
            
        data = resp.json()
        if data.get("code") != "0":
            logger.error(f"OKX API error: {data.get('msg')} (code: {data.get('code')})")
            return []
            
        candles = data.get("data", [])
        result = []
        for c in candles:
            ts = int(c[0])
            close_price = float(c[4])
            result.append((ts, close_price))
        return result
        
    except Exception as e:
        logger.error(f"Exception during fetching latest candles: {e}")
        return []
