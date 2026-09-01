if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => navigator.serviceWorker.register("/sw.js").catch(() => {}));
}

const appearance = (() => {
  const modes = new Set(["system", "light", "dark"]);
  const media = typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : null;
  let mode = "system";
  try {
    const stored = localStorage.getItem("lifelog-color-mode");
    if (modes.has(stored)) mode = stored;
  } catch (_) {}
  const apply = () => {
    const scheme = mode === "dark" || (mode === "system" && media?.matches) ? "dark" : "light";
    document.documentElement.dataset.theme = "lifelog";
    document.documentElement.dataset.colorScheme = scheme;
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", scheme === "dark" ? "#171a18" : "#27684e");
    document.querySelectorAll("[data-color-mode]").forEach(button => button.setAttribute("aria-pressed", String(button.dataset.colorMode === mode)));
  };
  const systemChanged = () => { if (mode === "system") apply(); };
  if (media?.addEventListener) media.addEventListener("change", systemChanged);
  else media?.addListener?.(systemChanged);
  return {apply, select(value) {
    if (!modes.has(value)) return;
    mode = value;
    try { localStorage.setItem("lifelog-theme", "lifelog"); localStorage.setItem("lifelog-color-mode", mode); } catch (_) {}
    apply();
  }};
})();

document.addEventListener("DOMContentLoaded", () => {
  appearance.apply();
  document.querySelectorAll("[data-color-mode]").forEach(button => button.addEventListener("click", () => appearance.select(button.dataset.colorMode)));
  const timezone = document.querySelector("[data-timezone]");
  if (timezone && timezone.value === "UTC") {
    try { timezone.value = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch (_) {}
  }
  document.querySelector("[data-date-picker]")?.addEventListener("change", event => {
    if (event.target.value) location.assign("/day/" + event.target.value);
  });
  document.querySelectorAll("[data-clear]").forEach(button => button.addEventListener("click", () => {
    document.querySelectorAll(`input[name="${button.dataset.clear}"]`).forEach(input => input.checked = false);
    button.closest("form")?.dispatchEvent(new Event("change", {bubbles: true}));
  }));
  const form = document.querySelector("[data-dirty-form]");
  if (!form) return;
  let dirty = false;
  const label = document.querySelector(".dirty-label");
  const mark = () => { dirty = true; if (label) label.textContent = "Unsaved changes"; };
  form.addEventListener("input", mark); form.addEventListener("change", mark);
  document.querySelectorAll("[data-remove-photo]").forEach(input => input.addEventListener("change", () => {
    input.closest(".photo-tile")?.classList.toggle("pending-removal", input.checked);
    if (input.nextElementSibling) input.nextElementSibling.textContent = input.checked ? "Keep" : "Remove";
  }));
  const photoInput = document.querySelector("[data-photo-input]");
  const previews = document.querySelector("[data-photo-previews]");
  let pending = [], objectURLs = [];
  const renderPhotos = () => {
    objectURLs.forEach(URL.revokeObjectURL); objectURLs = [];
    previews?.replaceChildren();
    pending.forEach((file, index) => {
      const tile = document.createElement("div"); tile.className = "photo-tile";
      const image = document.createElement("img"); const url = URL.createObjectURL(file);
      objectURLs.push(url); image.src = url; image.alt = "New photo preview";
      const button = document.createElement("button"); button.type = "button"; button.textContent = "Remove";
      button.addEventListener("click", () => { pending.splice(index, 1); syncPhotos(); mark(); });
      tile.append(image, button); previews?.append(tile);
    });
  };
  const syncPhotos = () => {
    if (!photoInput) return;
    const transfer = new DataTransfer(); pending.forEach(file => transfer.items.add(file)); photoInput.files = transfer.files;
    renderPhotos();
  };
  photoInput?.addEventListener("change", () => { pending = Array.from(photoInput.files); syncPhotos(); });
  window.addEventListener("pagehide", () => objectURLs.forEach(URL.revokeObjectURL));
  form.addEventListener("submit", () => { dirty = false; });
  window.addEventListener("beforeunload", event => { if (dirty) { event.preventDefault(); event.returnValue = ""; } });
});
