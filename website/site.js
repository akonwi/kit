// Shared behavior: theme toggle and copy buttons.
(() => {
  const root = document.documentElement;
  const stored =
    new URLSearchParams(location.search).get("theme") ?? localStorage.getItem("kit-theme");
  if (stored === "dark" || stored === "light") root.dataset.theme = stored;

  for (const button of document.querySelectorAll(".theme-toggle")) {
    const render = () => {
      const dark =
        root.dataset.theme === "dark" ||
        (!root.dataset.theme && matchMedia("(prefers-color-scheme: dark)").matches);
      if (button.hasAttribute("data-icon-only")) {
        // Show the scheme a click switches to.
        button.textContent = dark ? "☼" : "☾";
        button.setAttribute("aria-label", dark ? "Switch to light" : "Switch to dark");
      } else {
        button.textContent = dark ? "☾ dark" : "☼ light";
      }
    };
    render();
    button.addEventListener("click", () => {
      const dark =
        root.dataset.theme === "dark" ||
        (!root.dataset.theme && matchMedia("(prefers-color-scheme: dark)").matches);
      root.dataset.theme = dark ? "light" : "dark";
      localStorage.setItem("kit-theme", root.dataset.theme);
      render();
    });
  }

  for (const button of document.querySelectorAll(".cmd .copy, [data-copy]")) {
    button.addEventListener("click", async () => {
      const text =
        button.dataset.copy ?? button.parentElement.querySelector(".text")?.textContent ?? "";
      try {
        await navigator.clipboard.writeText(text);
        button.classList.remove("is-pulsing");
        void button.offsetWidth;
        button.classList.add("is-pulsing");
      } catch {
        // Clipboard unavailable; nothing to acknowledge.
      }
    });
  }

  // Mac app releases are tagged macos-vX.Y.Z and never marked Latest.
  const link = document.getElementById("mac-download") ?? document.querySelector("[data-mac-download]");
  if (link) {
    (async () => {
      try {
        const response = await fetch(
          "https://api.github.com/repos/akonwi/kit/releases?per_page=30",
          { headers: { Accept: "application/vnd.github+json" } },
        );
        if (!response.ok) return;
        const releases = await response.json();
        const release = releases.find(
          (r) => !r.draft && !r.prerelease && r.tag_name.startsWith("macos-v"),
        );
        const asset = release?.assets.find((a) => a.name.endsWith(".zip"));
        if (!asset) return;
        link.href = asset.browser_download_url;
        const version = link.querySelector("[data-version], [data-mac-version]");
        if (version) version.textContent = " " + release.tag_name.slice("macos-v".length);
      } catch {
        // Keep the fallback link.
      }
    })();
  }
})();
