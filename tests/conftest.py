"""测试公共配置:无网络、无真实 token / API key。"""
from __future__ import annotations

import os
import socket

# 必须在 import wordbot 之前设置:load_dotenv 不会覆盖已存在的变量,
# 从而保证本地 .env 里的真实密钥不会进入测试。
os.environ["TG_BOT_TOKEN"] = ""
os.environ["GOOGLE_API_KEY"] = ""
os.environ["DATA_API_BASE"] = "http://data.test"

import pytest  # noqa: E402


@pytest.fixture(autouse=True)
def _no_network(monkeypatch):
    """任何真实的 socket 连接都直接失败,确保测试不依赖网络。"""

    def _blocked(*args, **kwargs):
        raise RuntimeError("测试中禁止真实网络访问")

    monkeypatch.setattr(socket.socket, "connect", _blocked)
    monkeypatch.setattr(socket.socket, "connect_ex", _blocked)
