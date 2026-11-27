"""processing-bluei 앱 아이콘(1024px PNG)을 그린다: 공정 흐름(상자·화살표)과 CCP 하나."""
import sys
from PIL import Image, ImageDraw

S = 1024
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)
d.rounded_rectangle([64, 64, S - 64, S - 64], radius=200, fill=(11, 20, 36, 255))

def box(x, y, color, outline=None):
    d.rounded_rectangle([x, y, x + 230, y + 150], radius=34, fill=color, outline=outline, width=14)

def arrow(x1, y1, x2, y2):
    d.line([x1, y1, x2, y2], fill=(148, 163, 184, 255), width=22)
    dx, dy = ((1 if x2 > x1 else -1), 0) if x2 != x1 else (0, 1 if y2 > y1 else -1)
    d.polygon([(x2 + 34 * dx, y2 + 34 * dy), (x2 - 30 * dy - 6 * dx, y2 - 30 * dx - 6 * dy),
               (x2 + 30 * dy - 6 * dx, y2 + 30 * dx - 6 * dy)], fill=(148, 163, 184, 255))

teal, red = (20, 184, 166, 255), (239, 68, 68, 255)
box(170, 250, teal)
box(624, 250, (30, 41, 59, 255), outline=red)      # CCP 단계
box(624, 624, teal)
box(170, 624, (30, 41, 59, 255), outline=teal)
arrow(400, 325, 580, 325)
arrow(739, 400, 739, 580)
arrow(624, 699, 444, 699)
# CCP 표시
d.ellipse([786, 196, 886, 296], fill=red)
d.line([812, 246, 832, 268, 862, 226], fill=(255, 255, 255, 255), width=14, joint="curve")
img.save(sys.argv[1])
