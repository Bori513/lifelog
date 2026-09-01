(() => {
  let theme = "lifelog";
  let mode = "system";
  try {
    const storedTheme = localStorage.getItem("lifelog-theme");
    if (storedTheme === "lifelog") theme = storedTheme;
    const stored = localStorage.getItem("lifelog-color-mode");
    if (stored === "system" || stored === "light" || stored === "dark") mode = stored;
  } catch (_) {}
  const dark = mode === "dark" || (mode === "system" && typeof matchMedia === "function" && matchMedia("(prefers-color-scheme: dark)").matches);
  const scheme = dark ? "dark" : "light";
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.colorScheme = scheme;
  document.querySelector('meta[name="theme-color"]').content = dark ? "#171a18" : "#27684e";
})();
