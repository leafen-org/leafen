# Gives a CFF-flavoured OpenType font TrueType outlines (fontTools' cu2qu),
# keeping its names and everything else; build-onlyoffice.mjs runs it for the
# Korean fonts, whose CFF form x2t's PDF leaves out.
#
#	python3 otf2ttf.py <font.otf> <font.ttf>
import sys
from fontTools.pens.cu2quPen import Cu2QuPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib import TTFont, newTable

source, target = sys.argv[1], sys.argv[2]
font = TTFont(source)
if font.sfntVersion != "OTTO":
    raise SystemExit(f"{source} has no CFF outlines")
order = font.getGlyphOrder()
glyphs = font.getGlyphSet()
glyf = newTable("glyf")
glyf.glyphOrder = order
glyf.glyphs = {}
for name in order:
    pen = TTGlyphPen(glyphs)
    glyphs[name].draw(Cu2QuPen(pen, 1.0, reverse_direction=True))
    glyf.glyphs[name] = pen.glyph()
font["glyf"] = glyf
font["loca"] = newTable("loca")
for table in ("CFF ", "VORG"):
    if table in font:
        del font[table]
maxp = newTable("maxp")
maxp.tableVersion = 0x00010000
for field in ("maxZones", "maxTwilightPoints", "maxStorage", "maxFunctionDefs", "maxInstructionDefs",
              "maxStackElements", "maxSizeOfInstructions", "maxComponentElements"):
    setattr(maxp, field, 0)
maxp.maxZones = 1
font["maxp"] = maxp
font["post"].formatType = 3.0
font.sfntVersion = "\x00\x01\x00\x00"
font.save(target)
