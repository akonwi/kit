// Light rising from below the page through a grid of monospace glyphs: a
// terminal's idea of a gradient. The source is where the underscore of the
// K_ mark would sit, just under the viewport. Drawn once per resize or theme
// change.
(() => {
  const canvas = document.getElementById("horizon");
  if (!canvas) return;
  const ctx = canvas.getContext("2d", { alpha: false });
  const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;

  const BAYER = [
    [0, 8, 2, 10],
    [12, 4, 14, 6],
    [3, 11, 1, 9],
    [15, 7, 13, 5],
  ];
  // Glyph ramp, dim to bright. Index 0 is empty.
  const RAMP = [" ", "·", ":", "+"];

  const hash = (x, y) => {
    let h = (x * 374761393 + y * 668265263) | 0;
    h = ((h ^ (h >>> 13)) * 1274126177) | 0;
    return ((h ^ (h >>> 16)) >>> 0) / 4294967296;
  };

  const draw = () => {
    const root = document.documentElement;
    const s = getComputedStyle(root);
    const dark = root.dataset.theme ? root.dataset.theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
    const surface = s.getPropertyValue("--color-surface").trim();
    const accent = s.getPropertyValue("--color-accent").trim();
    const dpr = Math.min(2, devicePixelRatio || 1);
    const W = innerWidth;
    const H = innerHeight;
    canvas.width = W * dpr;
    canvas.height = H * dpr;
    canvas.style.width = W + "px";
    canvas.style.height = H + "px";
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = surface;
    ctx.fillRect(0, 0, W, H);

    const cw = 11;
    const ch = 18;
    const cols = Math.ceil(W / cw);
    const rows = Math.ceil(H / ch);
    ctx.font = `500 13px ${s.getPropertyValue("--font-mono")}`;
    ctx.textBaseline = "middle";
    ctx.textAlign = "center";

    // The light source: a wide segment just below the viewport.
    const barW = Math.min(W * 0.26, 400);
    const barX0 = (W - barW) / 2;
    const barX1 = barX0 + barW;
    const barY = H + ch;

    // Tones: four mixes of accent into the surface, darker theme gets brighter tops.
    const tones = (dark ? [0.16, 0.3, 0.52] : [0.14, 0.28, 0.5]).map(
      (t) => `color-mix(in oklab, ${accent} ${Math.round(t * 100)}%, ${surface})`,
    );

    for (let r = 0; r < rows; r++) {
      const y = r * ch + ch / 2;
      for (let c = 0; c < cols; c++) {
        const x = c * cw + cw / 2;
        // Distance from the bar segment.
        const dx = x < barX0 ? barX0 - x : x > barX1 ? x - barX1 : 0;
        const dy = Math.max(0, barY - y);
        const d = Math.sqrt(dx * dx + dy * dy);
        // Light falls off with distance; a little further to the sides than up.
        let v = Math.exp(-d / (H * 0.3));
        // Noise so the field doesn't read as a plain gradient.
        v *= 0.55 + 0.45 * hash(c, r);
        // The far field goes quiet, apart from a few idle cells.
        v = Math.max(0, v - 0.14);
        if (v === 0 && hash(r, c) < 0.004) v = 0.15;
        const t = BAYER[r & 3][c & 3] / 16;
        const level = v === 0 ? 0 : Math.min(3, Math.floor(v * 3 + t));
        if (level === 0) continue;
        ctx.fillStyle = tones[level - 1];
        ctx.fillText(RAMP[level], x, y);
      }
    }

  };

  draw();
  let raf = 0;
  addEventListener("resize", () => {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(draw);
  });
  new MutationObserver(draw).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  matchMedia("(prefers-color-scheme: dark)").addEventListener("change", draw);
  if (document.fonts?.ready) document.fonts.ready.then(draw);

  // The field drifts up a little as the chapters go by; the cursor stays put.
  if (!reduce) {
    let pending = 0;
    const onScroll = () => {
      if (pending) return;
      pending = requestAnimationFrame(() => {
        pending = 0;
        const p = scrollY / Math.max(1, document.documentElement.scrollHeight - innerHeight);
        canvas.style.transform = `translateY(${(p * 4).toFixed(2)}%)`;
      });
    };
    addEventListener("scroll", onScroll, { passive: true });
    onScroll();
  }
})();
