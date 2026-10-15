#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Regenerate flat brand vectors, outlined lockups and raster exports."""
from pathlib import Path
from math import ceil
import hashlib
from io import BytesIO
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from PIL import Image
import resvg_py

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'docs/assets'
NAVY, ORANGE, WHITE, ACCENT = '#0b2545', '#f05032', '#ffffff', '#ff8066'
word = instantiateVariableFont(TTFont(OUT / 'fonts/BricolageGrotesque-Variable.ttf'), {'wght':700,'opsz':72}, inplace=True)
body_font = TTFont(OUT / 'fonts/IBMPlexSans-Regular.woff2')


def mark(optical=False):
    edge, radius, stroke = (55,4,4) if optical else (52,3.4,2.8)
    return f'''<path fill="{ORANGE}" d="M22 5H42L38 14H26Z"/>
<path fill="{NAVY}" d="M25 17H39L{edge} 48 32 61 {64-edge} 48Z"/>
<g fill="{WHITE}" stroke="{WHITE}" stroke-width="{stroke}" stroke-linecap="round" stroke-linejoin="round">
<path fill="none" d="M29 28V49M29 35L40 41"/>
<circle cx="29" cy="28" r="{radius}"/><circle cx="29" cy="49" r="{radius}"/><circle cx="40" cy="41" r="{radius}"/>
</g>'''


def tile(optical=False, full_bleed=False):
    return f'<rect width="64" height="64" rx="{0 if full_bleed else 12}" fill="{WHITE}"/>\n' + mark(optical)


def svg(name, width, height, content, description, view=None):
    text = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {view or f'{width} {height}'}" width="{width}" height="{height}" role="img" aria-labelledby="title description">
<title id="title">SourceSteward</title><desc id="description">{description}</desc>
{content}
</svg>\n'''
    (OUT / (name + '.svg')).write_text(text)
    return text


def textpath(text, font, size, x, y, fill):
    glyphs, cmap = font.getGlyphSet(), font.getBestCmap()
    scale = size / font['head'].unitsPerEm
    pen = SVGPathPen(glyphs)
    for char in text:
        name = cmap[ord(char)]
        glyphs[name].draw(TransformPen(pen, (scale,0,0,-scale,x,y)))
        x += font['hmtx'][name][0] * scale
    return f'<path fill="{fill}" d="{pen.getCommands()}"/>', x


def width(text, font, size):
    return sum(font['hmtx'][font.getBestCmap()[ord(c)]][0] for c in text) * size / font['head'].unitsPerEm


def wordmark(x, y, size, foreground, accent):
    first, end = textpath('Source',word,size,x,y,foreground)
    second, end = textpath('Steward',word,size,end,y,accent)
    return first + '\n' + second, end


svg('logo-mark',256,256,mark(),'Wide navy tie, separated orange knot and white three-node branch.','64 64')
svg('logo',256,256,tile(),'Wide navy tie with an orange knot and white branch on a white rounded tile.','64 64')
svg('favicon',64,64,tile(True),'Optically strengthened wide tie icon with orange knot, navy body and white branch.','64 64')
for theme, fg, accent in [('light',NAVY,ORANGE),('dark',WHITE,ACCENT)]:
    letters,end = wordmark(80,46,40,fg,accent)
    svg('logo-'+theme,ceil(end+8),64,tile()+letters,'SourceSteward horizontal lockup for '+theme+' backgrounds.')
    letters,end = wordmark(0,46,40,fg,accent)
    svg('wordmark-'+theme,ceil(end+2),64,letters,'Outlined SourceSteward wordmark for '+theme+' backgrounds.')

letters,_ = wordmark(182,81,64,WHITE,ACCENT)
tagline,_ = textpath('Explainable version control.',body_font,25,184,119,WHITE)
svg('banner',1000,150,f'<rect width="1000" height="150" rx="18" fill="{NAVY}"/>\n<g transform="translate(42 27) scale(1.5)">{tile()}</g>\n'+letters+tagline,'SourceSteward. Explainable version control. Wide tie logo and outlined wordmark.')
size=88
block_width = 256+48+width('SourceSteward',word,size)
x = (1280-block_width)/2
letters,_ = wordmark(x+304,304,size,WHITE,ACCENT)
tagline,_ = textpath('Explainable version control.',body_font,32,x+307,365,WHITE)
subline,_ = textpath('Early foundation · stw',body_font,25,x+307,412,ACCENT)
svg('social-preview',1280,640,f'<rect width="1280" height="640" fill="{NAVY}"/>\n<g transform="translate({x} 192) scale(4)">{tile()}</g>\n'+letters+tagline+subline,'SourceSteward. Explainable version control. Early foundation, stw CLI.')


def render(name, size=None):
    options={'svg_string':(OUT / (name+'.svg')).read_text()}
    if size: options.update(width=size,height=size)
    return resvg_py.svg_to_bytes(**options)


for name in ('logo','logo-mark','logo-light','logo-dark','banner','social-preview'):
    (OUT / (name+'.png')).write_bytes(render(name))
for size in (16,24,32,48,64):
    (OUT / f'favicon-{size}.png').write_bytes(render('favicon',size))
Image.open(BytesIO(render('favicon',64))).save(OUT / 'favicon.ico',sizes=[(16,16),(32,32),(48,48),(64,64)])
# iOS applies its own mask: opaque square background, no pre-rounded transparent corners.
for name,size in [('apple-touch-icon',180),('android-chrome-192',192),('android-chrome-512',512)]:
    raw = resvg_py.svg_to_bytes(svg_string=f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">{tile(full_bleed=True)}</svg>',width=size,height=size)
    Image.open(BytesIO(raw)).convert('RGB').save(OUT / (name+'.png'))
assets = sorted(p for p in OUT.iterdir() if p.suffix in ('.svg','.png','.ico'))
(ROOT / 'brand/SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(ROOT)}\n' for p in assets))
print(f'Generated {len(assets)} flat brand assets and SHA256SUMS.')
# Review sheet uses actual exported pixels at native size, never scaled concept thumbnails.
from PIL import ImageDraw
sheet = Image.new('RGB',(1000,360),WHITE)
pen = ImageDraw.Draw(sheet)
for row,(name,dark) in enumerate([('logo',False),('favicon',False),('logo',True),('favicon',True)]):
    y=row*90
    bg=NAVY if dark else WHITE
    pen.rectangle((0,y,1000,y+90),fill=bg)
    pen.text((12,y+10),f'{name} / {"navy" if dark else "white"}',fill=WHITE if dark else NAVY)
    for index,size in enumerate((16,24,32,48,64)):
        x=200+index*150
        image=Image.open(BytesIO(render(name,size))).convert('RGBA')
        sheet.paste(image,(x,y+12),image)
        pen.text((x,y+76),f'{size}px',fill=WHITE if dark else NAVY)
sheet.save(ROOT/'brand/small-sizes.png')
