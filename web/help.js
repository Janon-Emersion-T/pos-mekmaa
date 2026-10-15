const helpState = {
  documents: [],
  selected: null,
  query: "",
  content: ""
};

function helpEscape(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function helpMarkdown(markdown) {
  const lines = String(markdown).split(/\r?\n/);
  const output = [];

  for (const line of lines) {
    const value = line.trim();

    if (!value) {
      output.push('<div class="h-3"></div>');
      continue;
    }

    if (/^### /.test(value)) {
      output.push(`<h3 class="mt-5 text-base font-semibold">${helpEscape(value.slice(4))}</h3>`);
    } else if (/^## /.test(value)) {
      output.push(`<h2 class="mt-7 text-lg font-semibold">${helpEscape(value.slice(3))}</h2>`);
    } else if (/^# /.test(value)) {
      output.push(`<h1 class="mb-5 text-2xl font-bold">${helpEscape(value.slice(2))}</h1>`);
    } else if (/^[-*] /.test(value)) {
      output.push(`<p class="pl-4 text-sm leading-7 text-gray-700">• ${helpEscape(value.slice(2))}</p>`);
    } else if (/^\d+\. /.test(value)) {
      output.push(`<p class="pl-4 text-sm leading-7 text-gray-700">${helpEscape(value)}</p>`);
    } else {
      output.push(`<p class="text-sm leading-7 text-gray-700">${helpEscape(value)}</p>`);
    }
  }

  return output.join("");
}

function helpView() {
  const term = helpState.query.toLowerCase();

  const documents = helpState.documents.filter(doc =>
    `${doc.title} ${doc.description} ${doc.category}`
      .toLowerCase()
      .includes(term)
  );

  const cards = documents.map(doc => `
    <button type="button"
      data-help-article="${helpEscape(doc.slug)}"
      class="w-full rounded-xl border border-gray-200 bg-white p-4 text-left transition hover:border-orange-300 hover:shadow-sm">
      <span class="text-xs font-semibold text-orange-600">${helpEscape(doc.category)}</span>
      <h3 class="mt-2 text-base font-semibold">${helpEscape(doc.title)}</h3>
      <p class="mt-2 text-sm leading-6 text-gray-500">${helpEscape(doc.description)}</p>
    </button>
  `).join("");

  const article = helpState.selected
    ? `
      <section class="min-w-0 rounded-xl border bg-white p-4 sm:p-6">
        <button type="button" data-help-back
          class="mb-5 min-h-11 rounded-lg border px-4 text-sm font-semibold">
          ← All guides
        </button>
        <div class="break-words">
          ${helpMarkdown(helpState.content)}
        </div>
      </section>
    `
    : `
      <div class="mb-5">
        <label for="help-search" class="block text-sm font-medium">
          Search documentation
        </label>
        <input id="help-search" type="search"
          value="${helpEscape(helpState.query)}"
          placeholder="Search sales, customers, inventory..."
          class="mt-2 h-12 w-full rounded-xl border border-gray-200 bg-white px-4 text-base">
      </div>

      <div id="help-results" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        ${cards || '<p class="text-sm text-gray-500">No matching guides found.</p>'}
      </div>
    `;

  return `<div id="help-workspace" class="mx-auto w-full max-w-6xl">${article}</div>`;
}

async function helpLoadIndex() {
  const response = await api("/api/help");
  helpState.documents = response;
  helpState.selected = null;
  helpState.content = "";
}

async function helpOpenArticle(slug) {
  const allowed = helpState.documents.some(doc => doc.slug === slug);
  if (!allowed) return;

  const article = await api("/api/help/" + encodeURIComponent(slug));

  if (page !== "help") return;

  helpState.selected = slug;
  helpState.content = article.content;

  const workspace = document.querySelector("#help-workspace");
  if (workspace) {
    workspace.outerHTML = helpView();
  }

  window.scrollTo(0, 0);
}

document.addEventListener("click", async event => {
  const article = event.target.closest("[data-help-article]");

  if (article) {
    try {
      await helpOpenArticle(article.dataset.helpArticle);
    } catch (error) {
      toast(error.message);
    }
    return;
  }

  if (event.target.closest("[data-help-back]")) {
    helpState.selected = null;
    helpState.content = "";

    const workspace = document.querySelector("#help-workspace");
    if (workspace) workspace.outerHTML = helpView();
  }
});

document.addEventListener("input", event => {
  if (event.target.id !== "help-search") return;

  helpState.query = event.target.value;

  const results = document.querySelector("#help-results");
  if (!results) return;

  const replacement = document.createElement("div");
  replacement.innerHTML = helpView();

  const next = replacement.querySelector("#help-results");
  if (next) results.innerHTML = next.innerHTML;
});
