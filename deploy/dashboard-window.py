#!/usr/bin/env python3
"""bluei-edge 대시보드 앱 창 (주소창·탭 없는 WebKitGTK 창).

GX10 의 snap Chromium 은 AppArmor 에 막혀 127.0.0.1 접속이 ERR_ACCESS_DENIED 가 되므로
시스템 WebKitGTK(예전 Tauri 대시보드와 같은 엔진)로 직접 띄운다."""
import os
import sys

# 예전 Tauri 대시보드(scripts/start-dashboard.sh)와 같은 설정: GX10 nvidia 에서 DMABUF 렌더러가
# GBM 버퍼를 못 만들어 빈 화면이 되고, 메모리 압박 모니터는 렌더러 폭주를 일으켰다.
for _k in ("WEBKIT_DISABLE_DMABUF_RENDERER", "WEBKIT_DISABLE_COMPOSITING_MODE",
           "WEBKIT_DISABLE_MEMORY_PRESSURE_MONITOR"):
    os.environ.setdefault(_k, "1")

import gi  # noqa: E402

gi.require_version("Gtk", "3.0")
gi.require_version("WebKit2", "4.1")
from gi.repository import GLib, Gtk, WebKit2  # noqa: E402

URL = os.environ.get("BLUEI_EDGE_DASHBOARD_URL", "http://127.0.0.1:8080/")


def main() -> int:
    GLib.set_prgname("bluei-edge")  # .desktop StartupWMClass 와 맞춰 작업 표시줄에 따로 뜬다
    Gtk.init(sys.argv)
    win = Gtk.Window(title="bluei-edge")
    win.set_default_size(1600, 1000)
    icon = os.environ.get("BLUEI_EDGE_DASHBOARD_ICON")
    if icon and os.path.exists(icon):
        win.set_icon_from_file(icon)
    view = WebKit2.WebView()
    view.connect("notify::title", lambda v, _p: win.set_title(v.get_title() or "bluei-edge"))
    if os.environ.get("BLUEI_EDGE_DASHBOARD_SELFTEST"):
        def done(v, ev):
            if ev == WebKit2.LoadEvent.FINISHED:
                print("loaded:", v.get_uri(), "title:", v.get_title(), flush=True)
                Gtk.main_quit()
        view.connect("load-changed", done)
        view.connect("load-failed", lambda v, e, u, err: (print("failed:", u, err.message, flush=True), Gtk.main_quit()))
    win.add(view)
    win.connect("destroy", Gtk.main_quit)
    view.load_uri(URL)
    win.show_all()
    Gtk.main()
    return 0


if __name__ == "__main__":
    sys.exit(main())
