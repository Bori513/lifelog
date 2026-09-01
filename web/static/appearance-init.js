(() => {
  const themes = new Set(["lifelog", "neon-pink", "matrix", "warm", "minimal", "midnight", "forest", "lavender"]);
  const themeColors = {
    lifelog: {light: "#f4f6f3", dark: "#171a18"},
    "neon-pink": {light: "#fbf4f8", dark: "#120e13"},
    matrix: {light: "#f1f6f1", dark: "#071009"},
    warm: {light: "#f7f0e3", dark: "#211a16"},
    minimal: {light: "#f7f7f7", dark: "#151515"},
    midnight: {light: "#eef3f8", dark: "#0d1724"},
    forest: {light: "#f1f3e9", dark: "#18221b"},
    lavender: {light: "#f4f1f8", dark: "#1d1824"}
  };
  let theme = "lifelog";
  let mode = "system";
  try {
    const storedTheme = localStorage.getItem("lifelog-theme");
    if (themes.has(storedTheme)) theme = storedTheme;
    const stored = localStorage.getItem("lifelog-color-mode");
    if (stored === "system" || stored === "light" || stored === "dark") mode = stored;
  } catch (_) {}
  const dark = mode === "dark" || (mode === "system" && typeof matchMedia === "function" && matchMedia("(prefers-color-scheme: dark)").matches);
  const scheme = dark ? "dark" : "light";
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.colorScheme = scheme;
  document.querySelector('meta[name="theme-color"]').content = themeColors[theme][scheme];
})();
