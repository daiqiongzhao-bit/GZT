#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""GZT 浏览器插件打包脚本（Python 3 标准库，零依赖）。

把 ``extension/`` 目录打包成可分发的 ``extension.zip``，并保持两处产物同步：

* ``frontend/public/extension.zip`` —— 随前端构建被拷贝进 ``web/dist``，
  最终由 ``backend/main.go`` 的 ``//go:embed`` 嵌入二进制 / 镜像分发；
* ``extension.zip``（仓库根）—— 便于直接下载 / 归档，消除历史游离产物。

设计要点：

1. **版本号单一来源**：从 ``backend/internal/config/config.go`` 读取
   ``AppVersion: "vX.Y.Z"``，解析失败即报错退出（绝不静默使用默认值）。
2. **回写 manifest**：把解析到的版本号写回 ``extension/manifest.json`` 的
   ``version`` 字段，使「源码 manifest」与「zip 内 manifest」永远一致，
   从根本上消除版本漂移。
3. **可重复构建**：zip 条目按名称排序、时间戳固定、无额外元数据；
   同一输入连续打包两次，输出字节完全一致（便于 diff / 校验）。

用法::

    python3 tools/pack_extension.py           # 打包（幂等）
    python3 tools/pack_extension.py --check    # 仅检查产物是否最新
"""
from __future__ import annotations

import argparse
import io
import json
import re
import sys
import zipfile
from pathlib import Path

# 固定的 zip 内条目时间戳，保证可重复构建（ZIP 可表示的最早时间）。
FIXED_DATE_TIME = (1980, 1, 1, 0, 0, 0)

# 需要排除的垃圾文件 / 目录。
EXCLUDE_NAMES = {".DS_Store", "Thumbs.db"}
EXCLUDE_SUFFIXES = {".bak", ".zip", ".log"}
EXCLUDE_DIR_PARTS = {"__MACOSX"}

# 仓库根目录（本脚本位于 <root>/tools/pack_extension.py）。
ROOT = Path(__file__).resolve().parent.parent
EXT_DIR = ROOT / "extension"
MANIFEST = EXT_DIR / "manifest.json"
CONFIG_GO = ROOT / "backend" / "internal" / "config" / "config.go"

# 分发产物：两处保持字节级同步。
OUTPUTS = (
    ROOT / "frontend" / "public" / "extension.zip",
    ROOT / "extension.zip",
)

# AppVersion: "v0.15.0"  ->  group(1) = "v0.15.0"
APP_VERSION_RE = re.compile(r'AppVersion:\s*"(v?[0-9]+\.[0-9]+\.[0-9]+)"')
# "version": "0.15.0"
MANIFEST_VERSION_RE = re.compile(r'("version"\s*:\s*")([^"]*)(")')


class PackError(Exception):
    """打包过程中的致命错误（解析失败、文件缺失等）。"""


def _rel(path: Path) -> str:
    """返回相对仓库根的展示路径；不在根目录下时回退为绝对路径。"""
    try:
        return str(path.relative_to(ROOT))
    except ValueError:
        return str(path)


def read_app_version() -> str:
    """从 ``config.go`` 解析 App 版本号（返回不带 ``v`` 前缀的 ``X.Y.Z``）。

    Raises:
        PackError: 配置文件缺失或无法解析出 ``AppVersion``。
    """
    if not CONFIG_GO.is_file():
        raise PackError(f"找不到配置文件: {CONFIG_GO}")
    text = CONFIG_GO.read_text(encoding="utf-8")
    match = APP_VERSION_RE.search(text)
    if not match:
        raise PackError(
            f'无法在 {_rel(CONFIG_GO)} 中解析 AppVersion'
            '（期望形如 AppVersion: "v0.15.0"）'
        )
    raw = match.group(1)
    return raw[1:] if raw.startswith("v") else raw


def render_manifest(current_text: str, version: str) -> str:
    """返回把 ``version`` 字段替换为指定值后的 manifest 文本。

    仅替换版本号字面量，其余内容与缩进保持原样；替换后校验仍是合法 JSON。

    Raises:
        PackError: 定位不到 version 字段，或替换后 JSON 非法。
    """
    new_text, count = MANIFEST_VERSION_RE.subn(
        lambda m: m.group(1) + version + m.group(3), current_text, count=1
    )
    if count != 1:
        raise PackError(f'无法在 {_rel(MANIFEST)} 中定位 "version" 字段')
    try:
        json.loads(new_text)
    except json.JSONDecodeError as exc:  # pragma: no cover - 防御性分支
        raise PackError(f"回写版本号后 manifest.json 不是合法 JSON: {exc}") from exc
    return new_text


def collect_entries() -> list[tuple[str, Path]]:
    """收集 ``extension/`` 下待打包文件。

    Returns:
        按 zip 内相对路径升序排列的 ``(arcname, 本地绝对路径)`` 列表。
        垃圾文件（.DS_Store / Thumbs.db / *.bak / *.zip / *.log / __MACOSX）已排除。

    Raises:
        PackError: 插件源目录缺失。
    """
    if not EXT_DIR.is_dir():
        raise PackError(f"找不到插件源目录: {EXT_DIR}")

    entries: list[tuple[str, Path]] = []
    for path in EXT_DIR.rglob("*"):
        if not path.is_file():
            continue
        rel = path.relative_to(EXT_DIR)
        if any(part in EXCLUDE_DIR_PARTS for part in rel.parts):
            continue
        if path.name in EXCLUDE_NAMES or path.suffix.lower() in EXCLUDE_SUFFIXES:
            continue
        entries.append((rel.as_posix(), path))

    entries.sort(key=lambda item: item[0])
    return entries


def build_zip_bytes(
    entries: list[tuple[str, Path]],
    manifest_content: bytes | None = None,
) -> bytes:
    """按确定性规则把 ``entries`` 打成 zip 字节流。

    Args:
        entries: ``(arcname, 本地路径)`` 列表。
        manifest_content: 可选；非 None 时用它覆盖 zip 内 ``manifest.json``
            的内容。调用方始终传入「回写版本号后的 manifest 字节」，从而保证
            源码 manifest 与 zip 内 manifest 字节级一致（且不依赖落盘状态）。

    Returns:
        zip 文件的完整字节内容。
    """
    buffer = io.BytesIO()
    with zipfile.ZipFile(
        buffer, "w", zipfile.ZIP_DEFLATED, compresslevel=9
    ) as archive:
        for arcname, path in entries:
            if manifest_content is not None and arcname == "manifest.json":
                data = manifest_content
            else:
                data = path.read_bytes()
            info = zipfile.ZipInfo(filename=arcname, date_time=FIXED_DATE_TIME)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3  # 固定为 Unix，跨平台可重复
            info.external_attr = 0o644 << 16
            archive.writestr(
                info, data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9
            )
    return buffer.getvalue()


def run(check_only: bool = False) -> int:
    """执行打包（或检查）。

    Args:
        check_only: 为 True 时仅检查产物是否已是最新，不写任何文件。

    Returns:
        进程退出码：0 成功/已最新；1 检查发现过期；2 致命错误。
    """
    version = read_app_version()
    entries = collect_entries()

    # 以「原始字节」读写 manifest，避免 write_text 在不同平台把 \n 翻译成 \r\n
    # 造成换行风格漂移（Windows 上会把 802 字节的 LF 文件写坏成 834 字节 CRLF）。
    source_manifest_bytes = MANIFEST.read_bytes()
    source_manifest = source_manifest_bytes.decode("utf-8")
    desired_manifest = render_manifest(source_manifest, version)
    desired_manifest_bytes = desired_manifest.encode("utf-8")

    # 统一用期望的 manifest 字节去打 zip：无论 --check 还是正式打包，
    # 产物内容完全一致，且不依赖文件是否已经落盘。
    zip_bytes = build_zip_bytes(entries, manifest_content=desired_manifest_bytes)

    print(f"[pack] 源目录     : {EXT_DIR}")
    print(f"[pack] 解析版本   : {version}  (来自 {_rel(CONFIG_GO)})")
    print(f"[pack] 输出路径   : {', '.join(_rel(o) for o in OUTPUTS)}")
    print(f"[pack] 条目数     : {len(entries)}")
    print(f"[pack] 字节数     : {len(zip_bytes)}")

    if check_only:
        problems: list[str] = []
        if source_manifest_bytes != desired_manifest_bytes:
            problems.append(f"{_rel(MANIFEST)}: version 不是 {version}")
        for out in OUTPUTS:
            if not out.is_file() or out.read_bytes() != zip_bytes:
                problems.append(f"{_rel(out)}: 需要重新打包")
        if problems:
            for item in problems:
                print(f"[pack] 待更新   : {item}")
            print("[pack] --check  : 产物不是最新")
            return 1
        print("[pack] --check  : 产物已是最新")
        return 0

    # 正式打包：先回写 manifest（仅在确有变化时），再写到两处产物。
    if source_manifest_bytes != desired_manifest_bytes:
        MANIFEST.write_bytes(desired_manifest_bytes)
        print(f"[pack] 更新       : {_rel(MANIFEST)} -> version {version}")

    for out in OUTPUTS:
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_bytes(zip_bytes)
        print(f"[pack] 写出       : {_rel(out)} ({len(zip_bytes)} 字节)")

    print(
        f"[pack] 完成       : 版本 {version}，{len(entries)} 个条目，"
        f"{len(zip_bytes)} 字节"
    )
    return 0


def main(argv: list[str] | None = None) -> int:
    """命令行入口。"""
    parser = argparse.ArgumentParser(
        description="GZT 浏览器插件打包（可重复构建，零依赖）"
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="仅检查产物是否最新（不写文件）；最新退出 0，否则退出 1",
    )
    args = parser.parse_args(argv)
    try:
        return run(check_only=args.check)
    except PackError as exc:
        print(f"[pack] ✗ 错误: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
