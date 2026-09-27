if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => navigator.serviceWorker.register("/sw.js").catch(() => {}));
}

const appearance = (() => {
  const modes = new Set(["system", "light", "dark"]);
  const themes = new Set(["lifelog", "neon-pink", "matrix", "warm", "minimal", "midnight", "forest", "lavender"]);
  const themeColors = {
    lifelog: {light: "#f4f6f3", dark: "#171a18"}, "neon-pink": {light: "#fbf4f8", dark: "#120e13"},
    matrix: {light: "#f1f6f1", dark: "#071009"}, warm: {light: "#f7f0e3", dark: "#211a16"},
    minimal: {light: "#f7f7f7", dark: "#151515"}, midnight: {light: "#eef3f8", dark: "#0d1724"},
    forest: {light: "#f1f3e9", dark: "#18221b"}, lavender: {light: "#f4f1f8", dark: "#1d1824"}
  };
  const media = typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : null;
  let theme = "lifelog";
  let mode = "system";
  try {
    const storedTheme = localStorage.getItem("lifelog-theme");
    if (themes.has(storedTheme)) theme = storedTheme;
    const stored = localStorage.getItem("lifelog-color-mode");
    if (modes.has(stored)) mode = stored;
  } catch (_) {}
  const apply = () => {
    const scheme = mode === "dark" || (mode === "system" && media?.matches) ? "dark" : "light";
    document.documentElement.dataset.theme = theme;
    document.documentElement.dataset.colorScheme = scheme;
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", themeColors[theme][scheme]);
    document.querySelectorAll("[data-color-mode]").forEach(button => button.setAttribute("aria-pressed", String(button.dataset.colorMode === mode)));
    document.querySelectorAll("[data-theme-option]").forEach(button => button.setAttribute("aria-pressed", String(button.dataset.themeOption === theme)));
  };
  const systemChanged = () => { if (mode === "system") apply(); };
  if (media?.addEventListener) media.addEventListener("change", systemChanged);
  else media?.addListener?.(systemChanged);
  return {apply, selectMode(value) {
    if (!modes.has(value)) return;
    mode = value;
    try { localStorage.setItem("lifelog-color-mode", mode); } catch (_) {}
    apply();
  }, selectTheme(value) {
    if (!themes.has(value)) return;
    theme = value;
    try { localStorage.setItem("lifelog-theme", theme); } catch (_) {}
    apply();
  }};
})();

document.addEventListener("DOMContentLoaded", () => {
  appearance.apply();
  document.querySelectorAll("[data-color-mode]").forEach(button => button.addEventListener("click", () => appearance.selectMode(button.dataset.colorMode)));
  document.querySelectorAll("[data-theme-option]").forEach(button => button.addEventListener("click", () => appearance.selectTheme(button.dataset.themeOption)));
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
  document.querySelector("[data-browse-question]")?.addEventListener("change", event => {
    const browseForm = event.target.form;
    for (const name of ["op", "value", "option", "page"]) {
      browseForm?.querySelectorAll(`[name="${name}"]`).forEach(control => control.remove());
    }
    browseForm?.submit();
  });
  const moreButton = document.querySelector("[data-mobile-more]");
  const moreMenu = document.querySelector("[data-mobile-more-menu]");
  const closeMore = (restoreFocus = false) => {
    if (!moreButton || !moreMenu || moreMenu.hidden) return;
    moreMenu.hidden = true;
    moreButton.setAttribute("aria-expanded", "false");
    if (restoreFocus) moreButton.focus();
  };
  moreButton?.addEventListener("click", () => {
    if (!moreMenu) return;
    const opening = moreMenu.hidden;
    moreMenu.hidden = !opening;
    moreButton.setAttribute("aria-expanded", String(opening));
    if (opening) moreMenu.querySelector("a, button")?.focus();
  });
  moreMenu?.addEventListener("click", event => {
    if (event.target.closest("a")) closeMore();
  });
  document.addEventListener("click", event => {
    if (!moreMenu?.hidden && !moreMenu.contains(event.target) && !moreButton?.contains(event.target)) closeMore();
  });
  document.addEventListener("keydown", event => {
    if (event.key === "Escape" && !moreMenu?.hidden) closeMore(true);
  });
  const form = document.querySelector("[data-dirty-form]");
  if (!form) return;

  const parseWorkoutLine = line => {
    for (let i = 0; i < line.length; i++) {
      if (line[i] !== " " && line[i] !== "\t") continue;
      const name = line.slice(0, i).trim().replace(/:$/, "").trim();
      const setText = line.slice(i).trim();
      const first = setText.split(",", 1)[0].trim();
      if (name && /^(\d+)\s*(?:([xX+\-])\s*(\d+(?:\.\d+)?))?$/.test(first)) return {name, setText};
    }
    return null;
  };
  const parseWorkout = raw => {
    const exercises = [], warnings = [];
    raw.split("\n").forEach(line => {
      if (!line.trim()) return;
      const split = parseWorkoutLine(line);
      if (!split) { warnings.push(line); return; }
      const sets = [];
      split.setText.split(",").forEach(value => {
        const token = value.trim();
        const match = token.match(/^(\d+)\s*(?:([xX+\-])\s*(\d+(?:\.\d+)?))?$/);
        if (!match) { warnings.push(line); return; }
        const type = !match[2] ? "bodyweight" : match[2].toLowerCase() === "x" ? "external" : match[2] === "+" ? "added" : "assisted";
        sets.push({reps: Number(match[1]), type, weight: match[3] ? Number(match[3]) : 0});
      });
      if (sets.length) exercises.push({name: split.name, sets});
    });
    return {exercises, warnings: [...new Set(warnings)]};
  };
  const formatWeight = value => Number.isInteger(value) ? String(value) : String(value);
  const renderWorkout = input => {
    const target = input.closest("[data-editor]")?.querySelector("[data-workout-preview]");
    if (!target) return;
    target.replaceChildren();
    const parsed = parseWorkout(input.value);
    parsed.exercises.forEach(exercise => {
      const row = document.createElement("p");
      const title = document.createElement("strong"); title.textContent = exercise.name;
      const external = exercise.sets.filter(set => set.type === "external").map(set => set.weight);
      const added = exercise.sets.filter(set => set.type === "added").map(set => set.weight);
      const assisted = exercise.sets.filter(set => set.type === "assisted").map(set => set.weight);
      let detail = `${exercise.sets.length} ${exercise.sets.length === 1 ? "set" : "sets"}`;
      if (external.length) detail += ` · top load ${formatWeight(Math.max(...external))} kg`;
      else if (added.length) detail += ` · max added weight +${formatWeight(Math.max(...added))} kg`;
      else if (assisted.length) detail += ` · lowest assistance ${formatWeight(Math.min(...assisted))} kg`;
      else detail += ` · ${exercise.sets.reduce((sum, set) => sum + set.reps, 0)} total reps`;
      row.append(title, document.createTextNode(detail)); target.append(row);
    });
    if (parsed.warnings.length) {
      const warning = document.createElement("p"); warning.className = "workout-warning";
      warning.textContent = `Could not understand: ${parsed.warnings.join(" · ")}`; target.append(warning);
    }
  };
  document.querySelectorAll("[data-workout-input]").forEach(input => {
    renderWorkout(input);
    input.addEventListener("input", () => renderWorkout(input));
  });

  const questionList = document.querySelector("[data-question-list]");
  const pinnedQuestionList = document.querySelector("[data-pinned-question-list]");
  const pinKey = "lifelog-pinned-questions";
  const scope = questionList?.dataset.pinScope;
  let pinState = {};
  try {
    const stored = JSON.parse(localStorage.getItem(pinKey) || "{}");
    if (stored && typeof stored === "object" && !Array.isArray(stored)) pinState = stored;
  } catch (_) {}
  let pinned = new Set(Array.isArray(pinState[scope]) ? pinState[scope].filter(id => typeof id === "string" && /^\d+$/.test(id)) : []);
  const configuredCards = questionList ? Array.from(questionList.querySelectorAll("[data-question-card]")) : [];
  const orderQuestions = () => {
    if (!questionList || !pinnedQuestionList) return;
    configuredCards.filter(card => pinned.has(card.dataset.questionId)).forEach(card => pinnedQuestionList.append(card));
    configuredCards.filter(card => !pinned.has(card.dataset.questionId)).forEach(card => questionList.append(card));
    configuredCards.forEach(card => {
      const button = card.querySelector("[data-pin-question]");
      const selected = pinned.has(card.dataset.questionId);
      button?.setAttribute("aria-pressed", String(selected));
      if (button) button.textContent = selected ? "Unpin" : "Pin";
    });
  };
  orderQuestions();
  questionList?.querySelectorAll("[data-pin-question]").forEach(button => button.addEventListener("click", () => {
    const id = button.closest("[data-question-card]")?.dataset.questionId;
    if (!id) return;
    if (pinned.has(id)) pinned.delete(id); else pinned.add(id);
    pinState[scope] = Array.from(pinned);
    try { localStorage.setItem(pinKey, JSON.stringify(pinState)); } catch (_) {}
    orderQuestions();
  }));

  const focusDialog = document.querySelector("[data-focus-dialog]");
  const focusBody = focusDialog?.querySelector("[data-focus-body]");
  const focusTitle = focusDialog?.querySelector("[data-focus-title]");
  let focusedEditor = null, editorPlaceholder = null, focusButton = null;
  const closeFocus = () => {
    if (!focusedEditor || !editorPlaceholder) return;
    editorPlaceholder.replaceWith(focusedEditor);
    focusedEditor = null; editorPlaceholder = null;
    focusDialog?.close(); document.body.classList.remove("focus-editor-open"); focusButton?.focus();
  };
  document.querySelectorAll("[data-focus-editor]").forEach(button => button.addEventListener("click", () => {
    const card = button.closest("[data-question-card]");
    const editor = card?.querySelector("[data-editor]");
    if (!focusDialog || !focusBody || !editor) return;
    focusButton = button; focusedEditor = editor; editorPlaceholder = document.createComment("editor position");
    editor.replaceWith(editorPlaceholder); focusBody.append(editor);
    if (focusTitle) focusTitle.textContent = card.querySelector(".question-heading > label")?.textContent || "Focus editor";
    focusDialog.showModal(); document.body.classList.add("focus-editor-open"); editor.querySelector("textarea")?.focus();
  }));
  focusDialog?.querySelector("[data-focus-close]")?.addEventListener("click", closeFocus);
  focusDialog?.addEventListener("cancel", event => { event.preventDefault(); closeFocus(); });

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
