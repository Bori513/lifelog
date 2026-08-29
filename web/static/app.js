document.addEventListener("DOMContentLoaded", () => {
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
  form.addEventListener("submit", () => { dirty = false; });
  window.addEventListener("beforeunload", event => { if (dirty) { event.preventDefault(); event.returnValue = ""; } });
});
