"""Genera los íconos de Android desde el brand kit (build/appicon.png).

Se corre a mano cuando cambia la marca; los PNG resultantes se commitean,
como los de Linux y el .ico de Windows. Necesita Pillow.

    python build/android/iconos.py

Produce, en app/src/main/res/:
- mipmap-anydpi-v26/ic_launcher.xml e ic_launcher_round.xml: el ícono
  adaptativo (API 26+), fondo de color plano + la marca como capa de frente.
- mipmap-*/ic_launcher_foreground.png: la marca en la zona segura (66 de
  108 dp) de la capa de frente, por densidad.
- mipmap-*/ic_launcher.png e ic_launcher_round.png: el ícono heredado —un
  tile oscuro con la marca— para launchers que no usen el adaptativo.
- values/ic_launcher_background.xml: el color del fondo (--bg-app del tema
  oscuro), que es el que ven las máscaras del launcher.
"""

from pathlib import Path

from PIL import Image, ImageDraw

RAIZ = Path(__file__).resolve().parents[2]
MARCA = RAIZ / "build" / "appicon.png"
RES = RAIZ / "build" / "android" / "app" / "src" / "main" / "res"

# --bg-app del tema oscuro (frontend/src/styles/tokens.css).
FONDO = (0x17, 0x18, 0x1B, 255)

DENSIDADES = {"mdpi": 1, "hdpi": 1.5, "xhdpi": 2, "xxhdpi": 3, "xxxhdpi": 4}


def recortar_a_contenido(im: Image.Image) -> Image.Image:
    return im.crop(im.getbbox())


def marca_en(lado: int, proporcion: float) -> Image.Image:
    """La marca centrada en un lienzo transparente de `lado`, ocupando
    `proporcion` del lado en su dimensión mayor."""
    m = recortar_a_contenido(Image.open(MARCA).convert("RGBA"))
    objetivo = int(round(lado * proporcion))
    escala = objetivo / max(m.size)
    m = m.resize((max(1, round(m.width * escala)), max(1, round(m.height * escala))), Image.LANCZOS)
    lienzo = Image.new("RGBA", (lado, lado), (0, 0, 0, 0))
    lienzo.alpha_composite(m, ((lado - m.width) // 2, (lado - m.height) // 2))
    return lienzo


def tile(lado: int) -> Image.Image:
    """El ícono heredado: cuadrado oscuro de esquinas redondeadas con la marca."""
    fondo = Image.new("RGBA", (lado, lado), (0, 0, 0, 0))
    ImageDraw.Draw(fondo).rounded_rectangle((0, 0, lado - 1, lado - 1), radius=lado * 0.2, fill=FONDO)
    fondo.alpha_composite(marca_en(lado, 0.62))
    return fondo


def main() -> None:
    for nombre, factor in DENSIDADES.items():
        d = RES / f"mipmap-{nombre}"
        d.mkdir(parents=True, exist_ok=True)
        # Capa de frente del adaptativo: 108 dp; la zona segura son 66 dp.
        marca_en(int(108 * factor), 66 / 108 * 0.9).save(d / "ic_launcher_foreground.png", optimize=True)
        # Heredado: 48 dp.
        t = tile(int(48 * factor))
        t.save(d / "ic_launcher.png", optimize=True)
        t.save(d / "ic_launcher_round.png", optimize=True)

    any26 = RES / "mipmap-anydpi-v26"
    any26.mkdir(parents=True, exist_ok=True)
    xml = (
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n'
        '    <background android:drawable="@color/ic_launcher_background" />\n'
        '    <foreground android:drawable="@mipmap/ic_launcher_foreground" />\n'
        "</adaptive-icon>\n"
    )
    for n in ("ic_launcher.xml", "ic_launcher_round.xml"):
        (any26 / n).write_text(xml, encoding="utf-8", newline="\n")

    (RES / "values" / "ic_launcher_background.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        "<resources>\n"
        # Sin «--bg-app»: dos guiones seguidos son inválidos dentro de un
        # comentario XML y aapt rechaza el recurso.
        "    <!-- El token bg-app del tema oscuro. Lo genera build/android/iconos.py. -->\n"
        '    <color name="ic_launcher_background">#%02X%02X%02X</color>\n' % FONDO[:3]
        + "</resources>\n",
        encoding="utf-8",
        newline="\n",
    )
    print("íconos generados en", RES)


if __name__ == "__main__":
    main()
