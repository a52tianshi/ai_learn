# pip install -qU langchain "langchain[google-genai]"
import os
import time
import logging

from dotenv import load_dotenv

# 先加载 .env,让日志配置能读到其中的参数
load_dotenv()

# 日志级别由 .env 的 LOG_LEVEL 控制(DEBUG/INFO/WARNING/ERROR),默认 INFO
_log_level = os.getenv("LOG_LEVEL", "INFO").upper()

logging.basicConfig(
    level=getattr(logging, _log_level, logging.INFO),
    format="%(asctime)s.%(msecs)03d %(levelname)s %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("ai_learn")
log.info("LOG_LEVEL=%s", _log_level)

_t0 = time.perf_counter()

def _lap(label: str) -> None:
    """打印从上一次计时点到现在的耗时。"""
    global _t0
    now = time.perf_counter()
    log.info("%-24s +%6.2fs", label, now - _t0)
    _t0 = now


_lap("start")

from langchain.agents import create_agent

_lap("import langchain")


def get_weather(city: str) -> str:
    """Get weather for a given city."""
    return f"It's always sunny in {city}!"

_model = os.getenv("MODEL", "google_genai:gemini-2.5-flash")
_max_retries = int(os.getenv("MAX_RETRIES", "3"))

agent = create_agent(
    model=_model,
    tools=[get_weather],
    system_prompt="You are a helpful assistant",
)
_lap("create_agent")


def invoke_with_retry(payload, max_retries: int = _max_retries, base_delay: float = 1.5):
    """调用 agent,失败时按指数退避重试,并打印每次尝试的日志。"""
    for attempt in range(1, max_retries + 1):
        t = time.perf_counter()
        try:
            log.info("invoke attempt %d/%d ...", attempt, max_retries)
            res = agent.invoke(payload)
            log.info("invoke attempt %d 成功,耗时 %.2fs", attempt, time.perf_counter() - t)
            return res
        except Exception as e:
            elapsed = time.perf_counter() - t
            if attempt == max_retries:
                log.error("invoke attempt %d 失败(耗时 %.2fs),已达最大重试次数: %s", attempt, elapsed, e)
                raise
            delay = base_delay * (2 ** (attempt - 1))
            log.warning("invoke attempt %d 失败(耗时 %.2fs): %s -> %.2fs 后重试", attempt, elapsed, e, delay)
            time.sleep(delay)


result = invoke_with_retry(
    {"messages": [{"role": "user", "content": "What's the weather in Redhill, Singapore?"}]}
)
_lap("agent.invoke")

print(result["messages"][-1].content_blocks)
