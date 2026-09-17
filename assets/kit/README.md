# Kit logo assets

The selected direction is `K_`: JetBrains Mono Bold with a blue underscore.
Production assets use actual font outlines, rather than the image generator's
approximation. The font's natural monospace advances are preserved; the underscore is raised
to align its bottom edge with the K. App tiles use solid, flat fills without
gradients or shadows.

| Asset | Use |
| --- | --- |
| `logo-light.svg` / `.png` | Black K, blue underscore; for light backgrounds |
| `logo-dark.svg` / `.png` | Off-white K, blue underscore; for dark backgrounds |
| `logo-light-bordered.svg` / `.png` | Light-background logo with black rounded outline |
| `logo-dark-bordered.svg` / `.png` | Dark-background logo with off-white rounded outline |
| `app-icon.svg` / `.png` | Graphite app tile, used by the app |
| `app-icon-light.svg` / `.png` | Light tile alternative |
| `Kit.icns` | Graphite fallback app icon, 16–1024px representations |
| `Kit.icon` | Editable Icon Composer document with light and dark appearances |

Logo PNGs are 1024×768 with genuine alpha transparency. Borders have transparent
interiors. App icon PNGs are 1024×1024 with transparent outer margins. SVGs contain
paths, so consumers do not need to install the font. Colors: ink `#191A1D`,
off-white `#F2EFE8`, blue `#4F65FF`.

The light/dark logo names describe the intended background. On macOS Tahoe and
later, the app uses `Kit.icon`: a light tile with an ink K in Default appearance,
and a graphite tile with an off-white K in Dark appearance. Both retain the blue
underscore. System icon appearance settings select the variant independently of
Kit's in-app theme; clear/tinted styles are derived by the system.

Open `Kit.icon` in Apple's Icon Composer to edit or preview its two SVG layers.
Backgrounds are solid; layer glass, group translucency, and shadows are disabled.
The system owns the icon mask and appearance effects. The build compiles this
document into `Assets.car` with Xcode's `actool` and declares `CFBundleIconName`.
The existing graphite `Kit.icns` remains the fallback for older macOS versions.
Building requires Xcode 26 or later.

Regenerate on macOS from the repository root:

```sh
swift assets/kit/script/generate.swift "$PWD/assets/kit"
assets/kit/script/package-icon.sh
```

Font source: https://github.com/JetBrains/JetBrainsMono (Bold TTF), distributed
under the bundled `fonts/OFL.txt`. Keep the license with redistributed font files.
The app build copies `Kit.icns` into its bundle and declares CFBundleIconFile.

The app bundles Regular, Medium, and Bold JetBrains Mono fonts for Typography
settings. Interface text defaults to the system font; monospace text defaults to
JetBrains Mono. Font family and size preferences apply independently of themes.
