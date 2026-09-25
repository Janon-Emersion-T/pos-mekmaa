const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const $ = (s) => document.querySelector(s);
let settings = { currency: "USD", currencies: [] };
let moneyFormatter;
function applySettings(value) {
  settings = value;
  moneyFormatter = new Intl.NumberFormat("en-US", {
    style: "currency", currency: settings.currency,
    minimumFractionDigits: 2, maximumFractionDigits: 2,
  });
}
const money = (n) => moneyFormatter ? moneyFormatter.format(n / 100) : "—";
const icons = {
  settings: '<circle cx="12" cy="12" r="3"/><path d="M12 2v3m0 14v3M2 12h3m14 0h3M5 5l2 2m10 10 2 2M5 19l2-2M17 7l2-2"/><circle cx="12" cy="12" r="8"/>',
  logo: '<path d="M5 7h14v10H5zM8 4v3m8-3v3M8 11h8m-8 3h4"/>',
  grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  receipt: '<path d="M6 3l3 2 3-2 3 2 3-2v18l-3-2-3 2-3-2-3 2zM9 9h6m-6 4h6"/>',
  box: '<path d="M3 7l9-4 9 4v10l-9 4-9-4zM3 7l9 5 9-5M12 12v9M7 5l10 5"/>',
  search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="M16 16l5 5"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 1v3m0 16v3M1 12h3m16 0h3M4 4l2 2m12 12 2 2M4 20l2-2M18 6l2-2"/>',
  user: '<circle cx="12" cy="8" r="3"/><path d="M5 21v-3a7 7 0 0114 0v3"/>',
  cash: '<rect x="2" y="5" width="20" height="14" rx="2"/><circle cx="12" cy="12" r="3"/><path d="M5 12h1m12 0h1"/>',
  card: '<rect x="2" y="5" width="20" height="14" rx="2"/><path d="M2 10h20M6 15h3"/>',
  arrow: '<path d="M4 12h16m-6-6 6 6-6 6"/>',
  coffee:
    '<path d="M4 9h12v7a4 4 0 01-4 4H8a4 4 0 01-4-4zM16 9h2a3 3 0 010 6h-2M7 3v3m5-3v3"/>',
  bakery:
    '<path d="M3 16c-2-5 2-10 6-9 2-4 6-4 8 0 5 0 7 6 4 10l-5-2-8 1zM9 7l1 7m7-7-2 7"/>',
  tea: '<path d="M5 7h14l-2 14H7zM7 3h10M12 7V2m0 9h4"/>',
  kitchen:
    '<path d="M4 3v6a3 3 0 006 0V3M7 3v18m12-18c-5 4-5 10 0 10V3zm0 10v8"/>',
  cake: '<path d="M3 12h18v9H3zM3 16c3 3 3-3 6 0s3-3 6 0 3-3 6 0M6 12V8h12v4M12 8V4m0-3v1"/>',
};
const icon = (name) =>
  `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.grid}</svg>`;
document
  .querySelectorAll("[data-icon]")
  .forEach((el) => (el.innerHTML = icon(el.dataset.icon)));
function art(p) {
  let shape = "";
  if (p.art === "box")
    shape = '<path d="M45 60l55-25 55 25v65l-55 25-55-25zM45 60l55 25 55-25M100 85v65" fill="#fff" stroke="#777" stroke-width="4"/>';
  else if (p.art === "coffee")
    shape =
      '<ellipse cx="100" cy="132" rx="61" ry="17" fill="#fbf7ee"/><ellipse cx="100" cy="131" rx="45" ry="10" fill="#ddd7ca"/><path d="M139 75c34-9 34 35 4 34" fill="none" stroke="#fffdf7" stroke-width="12"/><path d="M51 69h95l-9 51c-4 20-70 20-76 0z" fill="#fcfaf4"/><ellipse cx="99" cy="70" rx="48" ry="19" fill="#fffdf7"/><ellipse cx="99" cy="71" rx="41" ry="14" fill="#ad6e3d"/><path d="M98 82c-33-7-26-21-11-17l11 8 11-8c17-4 22 10-11 17" fill="#f4e8cb"/><path d="M100 82V66" stroke="#ad6e3d" stroke-width="2"/>';
  else if (["iced", "matcha", "tea"].includes(p.art)) {
    const c =
      p.art === "matcha" ? "#849b53" : p.art === "tea" ? "#dba045" : "#b67a49";
    shape = `<path d="M126 25l-15 54" stroke="#ede6d6" stroke-width="7"/><path d="M65 54h72l-8 87q-29 12-56 0z" fill="#ffffff" opacity=".65"/><path d="M69 77h64l-7 62q-24 9-50 0z" fill="${c}"/><path d="M71 111h59l-4 28q-24 9-50 0z" fill="#efdbb2" opacity=".7"/><ellipse cx="101" cy="77" rx="32" ry="8" fill="${c}"/><g fill="#fff" opacity=".5"><rect x="78" y="64" width="17" height="17" rx="4" transform="rotate(15 86 72)"/><rect x="105" y="70" width="17" height="17" rx="4" transform="rotate(-20 110 78)"/></g><path d="M77 89l3 39" stroke="#fff" opacity=".4" stroke-width="3"/>`;
  } else if (p.art === "croissant")
    shape =
      '<path d="M39 122c-13-20-2-43 25-46 8-31 57-33 72-3 25 2 42 27 28 47l-25-7-78 3z" fill="#b86a28"/><path d="M42 115c-9-17 3-31 23-33l2 31zm94-33c20 0 32 15 22 30l-24-4z" fill="#df9b43"/><path d="M68 75q14-16 28-13l-1 62-29-9z" fill="#e9ac56"/><path d="M100 62q24-5 34 15l-7 41-28 7z" fill="#efb45f"/><path d="M78 78l4 30m30-30-1 31" stroke="#fbd38c" stroke-width="5" stroke-linecap="round"/>';
  else if (p.art === "cookie")
    shape =
      '<ellipse cx="99" cy="105" rx="56" ry="40" fill="#8c542f"/><ellipse cx="99" cy="98" rx="57" ry="39" fill="#c4925b"/><path d="M61 83l11-7 8 10-9 7zm36 20 10-8 11 6-5 12zm22-28 9-5 8 11-11 4zm-51 31 9-3 5 12-11 3zm30-37 5 5-8 9-7-7zm37 34 10 1-2 12-10-4z" fill="#513528"/><g fill="#e4b77d"><circle cx="87" cy="94" r="3"/><circle cx="114" cy="87" r="3"/><circle cx="92" cy="122" r="3"/></g>';
  else if (p.art === "toast" || p.art === "sandwich")
    shape = `<ellipse cx="100" cy="128" rx="70" ry="17" fill="#faf8ed"/><path d="M42 91l68-41 54 41-69 48z" fill="#a8672e"/><path d="M47 88l64-33 47 33-65 44z" fill="#e8bb76"/>${p.art === "toast" ? '<g fill="#9db76a" stroke="#d1da93" stroke-width="4"><path d="M66 95q14-36 45-28L86 113z"/><path d="M84 99q14-36 45-28l-25 46z"/><path d="M105 99q10-27 34-20l-17 32z"/></g><g fill="#475637"><circle cx="93" cy="90" r="2"/><circle cx="122" cy="93" r="2"/><circle cx="101" cy="104" r="2"/></g>' : '<path d="M49 94l43 35 60-38-40-28z" fill="#efb541"/><path d="M48 88l64-37 43 35-62 40z" fill="#edc884"/><path d="M77 78l38 29m-23-38 37 29" stroke="#a9733e" stroke-width="5"/>'}`;
  else if (p.art === "cake")
    shape =
      '<ellipse cx="100" cy="133" rx="65" ry="15" fill="#faf6f0"/><path d="M56 86l53-40 42 53v32l-95-12z" fill="#e9be91"/><path d="M56 80l53-34 42 49v28l-95-12z" fill="#f9efcf"/><path d="M56 79l53-35 42 51-95-10z" fill="#b74555"/><path d="M65 82l72 8" stroke="#dc8090" stroke-width="3"/><g fill="#a62e43"><circle cx="102" cy="60" r="10"/><circle cx="120" cy="76" r="9"/><circle cx="83" cy="73" r="8"/></g><path d="M105 52q-9-17 11-11l-6 14" fill="#617b43"/>';
  else
    shape =
      '<ellipse cx="100" cy="132" rx="63" ry="16" fill="#f9f5ec"/><ellipse cx="100" cy="102" rx="49" ry="36" fill="#b27537"/><ellipse cx="100" cy="92" rx="49" ry="33" fill="#dba15c"/><path d="M61 97c-12-44 84-47 79-5-3 32-70 27-65 0 3-19 49-24 49-3 0 16-31 19-31 6 0-5 11-6 15-2" fill="none" stroke="#89502f" stroke-width="7"/><path d="M59 88q35-19 81-4M67 104l59-31M82 118l57-23" stroke="#faebcc" stroke-width="5" opacity=".85"/>';
  return `<svg viewBox="0 0 200 170" role="img" aria-label="${escapeHTML(p.name)}">${shape}</svg>`;
}
let products = [],
  sales = [],
  purchases = [],
  petty = [],
  movements = [],
  users = [],
  session = null,
  currentUser = null,
  cart = new Map(),
  category = "",
  payment = "cash",
  page = "pos",
  busy = false,
  service = "Dine in";
let editingProduct = null;
let pageRequest = 0;
const pageTitles = {
  pos: "Point of sale", products: "Products", session: "Register session",
  sales: "Sales history", inventory: "Inventory", purchases: "Purchases",
  petty: "Petty cash", users: "Staff users", settings: "Settings",
};
function canVisit(next) {
  if (!Object.hasOwn(pageTitles, next)) return false;
  if (next === "users") return currentUser?.role === "superadmin";
  return !["products","inventory","purchases","settings"].includes(next) ||
    ["admin","superadmin"].includes(currentUser?.role);
}
function pageFromURL() {
  return location.pathname.replace(/^\/|\/$/g, "") || "pos";
}
window.addEventListener("popstate", () => navigate(pageFromURL(), false));
async function api(path, options) {
  const r = await fetch(path, options);
  if (r.status === 401) {
    location.replace("/login?next=" + encodeURIComponent(location.pathname));
    throw Error("Please sign in again");
  }
  const data = await r.json();
  if (!r.ok) throw Error(data.error || "Something went wrong");
  return data;
}
let toastTimer;
function toast(message) {
  $("#toast").textContent = message;
  $("#toast").classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => $("#toast").classList.add("hidden"), 3500);
}
function renderProducts() {
  const categories = [...new Set(products.map((p) => p.category))].sort();
  if (category && !categories.includes(category)) category = "";
  const term = $("#search").value.toLowerCase();
  const filtered = products.filter(
    (p) =>
      (category === "" || p.category === category) &&
      p.name.toLowerCase().includes(term),
  );
  $("#categories").innerHTML = [["", "grid"], ...categories.map((name) => [name, "box"])]
    .map(
      ([name, i]) =>
        `<button class="category ${category === name ? "active" : ""}" data-category="${escapeHTML(name)}">${icon(i)}${escapeHTML(name || "All products")}</button>`,
    )
    .join("");
  $("#category-title").innerHTML =
    `${escapeHTML(category || "All products")} <span class="ml-1 text-xs font-normal text-gray-400">(${filtered.length})</span>`;
  $("#products").innerHTML =
    filtered
      .map(
        (p) =>
          `<button class="product group ${p.stock === 0 ? "opacity-50" : ""}" data-product="${p.id}" ${p.stock === 0 ? "disabled" : ""} aria-label="Add ${escapeHTML(p.name)} to order"><div class="product-art relative h-36 p-1" style="background:${escapeHTML(p.color)}">${art(p)}<span class="absolute left-3 top-3 rounded-md bg-white/80 px-1.5 py-1 text-[8px] font-medium text-gray-600">${p.stock === 0 ? "Sold out" : p.stock + " available"}</span></div><div class="p-3.5"><p class="text-[10px] text-gray-400">${escapeHTML(p.category)}</p><h3 class="mt-1 truncate text-xs font-semibold">${escapeHTML(p.name)}</h3><div class="mt-3 flex items-center justify-between"><span class="text-sm font-semibold">${money(p.price)}</span><span class="flex h-6 w-6 items-center justify-center rounded-md bg-orange-50 text-lg font-light text-orange-500 transition group-hover:bg-accent group-hover:text-white">+</span></div></div></button>`,
      )
      .join("") ||
    `<p class="col-span-full py-20 text-center text-sm text-gray-400">${products.length ? "No matching products. Try another search." : "No products yet. Add products from the Products menu to start selling."}</p>`;
}
function renderCart() {
  for (const [id, qty] of cart) {
    const p = products.find((p) => p.id === id);
    if (!p || p.stock === 0) cart.delete(id);
    else if (qty > p.stock) cart.set(id, p.stock);
  }
  let total = 0,
    count = 0;
  $("#cart").innerHTML =
    [...cart]
      .map(([id, qty]) => {
        const p = products.find((p) => p.id === id);
        total += p.price * qty;
        count += qty;
        return `<div class="flex items-center gap-3"><div class="product-art h-14 w-14 shrink-0 rounded-lg" style="background:${escapeHTML(p.color)}">${art(p)}</div><div class="min-w-0 flex-1"><h3 class="truncate text-xs font-medium">${escapeHTML(p.name)}</h3><p class="mt-1 text-[10px] text-gray-400">${money(p.price)}</p><div class="mt-2 flex items-center gap-2"><button class="flex h-5 w-5 items-center justify-center rounded border border-gray-200 text-xs" data-delta="-1" data-id="${id}" aria-label="Remove one ${escapeHTML(p.name)}">−</button><span class="w-3 text-center text-[10px]">${qty}</span><button class="flex h-5 w-5 items-center justify-center rounded border border-gray-200 text-xs" data-delta="1" data-id="${id}" aria-label="Add one ${escapeHTML(p.name)}">+</button></div></div><span class="text-xs font-semibold">${money(p.price * qty)}</span></div>`;
      })
      .join("") ||
    `<div class="flex h-full min-h-52 flex-col items-center justify-center text-gray-300"><div class="mb-4 flex h-14 w-14 items-center justify-center rounded-full bg-gray-50">${icon("receipt")}</div><p class="text-sm font-medium text-gray-500">A good day starts here</p><p class="mt-2 text-[11px] text-gray-400">Choose a product to start an order.</p></div>`;
  $("#cart-count").textContent = `(${count})`;
  $("#subtotal").textContent = $("#total").textContent = money(total);
  $("#tax").textContent = money(0);
  $("#checkout").disabled = !count || busy || !moneyFormatter;
}
function add(id, delta = 1) {
  if (busy) return;
  const p = products.find((p) => p.id === id),
    qty = (cart.get(id) || 0) + delta;
  if (!p) return;
  if (qty > p.stock) {
    toast(`Only ${p.stock} ${p.name} available`);
    return;
  }
  if (qty <= 0) cart.delete(id);
  else cart.set(id, qty);
  renderCart();
}
const field = (label, name, type = "text", value = "") =>
  `<label class="block text-xs font-medium text-gray-600">${label}<input name="${name}" type="${type}" value="${escapeHTML(value)}" ${type === "number" ? `step="1" max="1000000" ${name === "quantity" ? "" : 'min="0"'}` : ""} ${name === "name" ? 'maxlength="200"' : name === "category" ? 'maxlength="100"' : ""} required class="mt-2 h-11 w-full rounded-lg border border-gray-200 px-3"></label>`;
const table = (heads, rows) =>
  `<div class="overflow-x-auto rounded-xl border border-gray-200 bg-white"><table class="w-full text-left text-xs"><thead class="border-b bg-gray-50 text-gray-400"><tr>${heads.map((x) => `<th class="p-4 font-medium">${x}</th>`).join("")}</tr></thead><tbody>${rows || `<tr><td colspan="${heads.length}" class="p-14 text-center text-gray-400">No records yet.</td></tr>`}</tbody></table></div>`;
const amountField = (label, name, value = "") =>
  `<label class="block text-xs font-medium text-gray-600">${label} (${settings.currency})<input name="${name}" type="number" min="0" max="100000" step="0.01" value="${value}" required class="mt-2 h-11 w-full rounded-lg border border-gray-200 px-3"></label>`;
function minorUnits(value) {
  if (!/^\d+(\.\d{1,2})?$/.test(value)) throw Error("Enter an amount with up to two decimal places");
  const n = Math.round(Number(value) * 100);
  if (!Number.isSafeInteger(n) || n > 10000000) throw Error("Amount is too large");
  return n;
}
function productManagement() {
  const p = products.find((p) => p.id === editingProduct);
  const appearances = ["box","coffee","iced","matcha","tea","croissant","cookie","toast","sandwich","cake","roll"];
  return `<form data-form="product" data-id="${p?.id || ""}" class="mb-6 grid gap-4 rounded-xl border bg-white p-5 sm:grid-cols-3">
    <h2 class="font-semibold sm:col-span-3">${p ? "Edit product" : "Add product"}</h2>
    ${field("Product name", "name", "text", p?.name || "")}
    ${field("Category", "category", "text", p?.category || "")}
    ${amountField("Selling price", "price", p ? (p.price / 100).toFixed(2) : "")}
    ${amountField("Cost", "cost", p ? (p.cost / 100).toFixed(2) : "0.00")}
    ${p ? '<p class="text-xs text-gray-500">Change stock through Inventory adjustments.</p>' : field("Opening stock", "stock", "number", "0")}
    ${field("Reorder level", "reorderLevel", "number", p?.reorderLevel ?? 10)}
    <label class="text-xs font-medium text-gray-600">Appearance<select name="art" class="mt-2 h-11 w-full rounded-lg border px-3">${appearances.map((a) => `<option ${a === (p?.art || "box") ? "selected" : ""}>${a}</option>`).join("")}</select></label>
    ${field("Background color", "color", "color", p?.color || "#eeeeee")}
    <div class="flex items-center gap-3"><button class="rounded-lg bg-accent px-5 py-3 text-sm text-white disabled:opacity-50">${p ? "Save changes" : "Add product"}</button>${p ? '<button type="button" data-cancel-product class="text-sm text-gray-500">Cancel</button>' : ""}</div>
  </form>
  <div class="mb-4 flex items-center justify-between"><h2 class="font-semibold">Products (${products.length})</h2><button data-delete-all class="rounded-lg border px-4 py-2 text-xs text-red-600 disabled:opacity-50" ${products.length ? "" : "disabled"}>Delete all products</button></div>
  ${table(["Product","Category","Price","Cost","Stock","Actions"], products.map((p) => `<tr class="border-b"><td class="p-4 font-medium">${escapeHTML(p.name)}</td><td class="p-4">${escapeHTML(p.category)}</td><td class="p-4">${money(p.price)}</td><td class="p-4">${money(p.cost)}</td><td class="p-4">${p.stock}</td><td class="p-4"><button data-edit-product="${p.id}" class="mr-3 text-orange-600">Edit</button><button data-delete-product="${p.id}" class="text-red-600">Delete</button></td></tr>`).join(""))}
  <p class="mt-3 text-xs text-gray-500">Deleted products are removed from the catalog. Past sales, purchases, and stock records are kept.</p>`;
}
function renderOther() {
  if (page === "pos") return;
  let title = "",
    desc = "",
    body = "";
  if (page === "products") {
    title = "Products";
    desc = "Add your products and categories, set prices, and manage the catalog.";
    body = productManagement();
  } else if (page === "settings") {
    title = "Settings";
    desc = "Manage preferences for this store.";
    body = `<form data-form="settings" class="max-w-md rounded-xl border bg-white p-5">
      <h2 class="font-semibold">Store currency</h2>
      <p class="mt-2 text-xs leading-5 text-gray-500">Used for prices, checkout, receipts, and all history. Changing currency keeps existing amounts the same; no exchange-rate conversion is applied.</p>
      <label class="mt-5 block text-xs font-medium text-gray-600">Currency
        <select name="currency" required class="mt-2 h-11 w-full rounded-lg border border-gray-200 px-3">
          ${settings.currencies.map((c) => `<option value="${c.code}" ${c.code === settings.currency ? "selected" : ""}>${c.code} — ${c.name}</option>`).join("")}
        </select>
      </label>
      <p class="mt-3 text-xs text-gray-500">Current format: ${money(123456)}</p>
      <button class="mt-5 rounded-lg bg-accent px-5 py-3 text-sm text-white disabled:opacity-50">Save settings</button>
    </form>`;
  } else if (page === "users") {
    title = "Staff users";
    desc =
      "Create staff accounts and control their access. Only superadmins can manage users.";
    body =
      `<form data-form="user" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-4">${field("Email", "email", "email")}${field("Temporary password", "password", "password")}<label class="text-xs font-medium text-gray-600">Role<select name="role" class="mt-2 h-11 w-full rounded-lg border px-3"><option value="cashier">Cashier</option><option value="admin">Admin</option><option value="superadmin">Superadmin</option></select></label><button class="mt-6 h-11 rounded-lg bg-ink text-sm text-white">Create user</button></form>` +
      table(
        ["Email", "Role", "Status", "Created", "Action"],
        users
          .map(
            (u) =>
              `<tr class="border-b"><td class="p-4 font-medium">${escapeHTML(u.email)}</td><td class="p-4 capitalize">${escapeHTML(u.role)}</td><td class="p-4"><span class="rounded-full px-2 py-1 ${u.active ? "bg-green-50 text-green-700" : "bg-gray-100 text-gray-500"}">${u.active ? "Active" : "Disabled"}</span></td><td class="p-4">${new Date(u.createdAt).toLocaleDateString()}</td><td class="p-4">${u.id === currentUser.id ? '<span class="text-gray-400">Current user</span>' : `<button data-user-toggle="${u.id}" data-active="${!u.active}" class="text-orange-600">${u.active ? "Disable" : "Enable"}</button>`}</td></tr>`,
          )
          .join(""),
      );
  } else if (page === "sales") {
    title = "Sales history";
    desc = "Orders are tied to the register session that created them.";
    body = table(
      ["Order", "Date", "Items", "Payment", "Total"],
      sales
        .map(
          (s) =>
            `<tr class="border-b"><td class="p-4 font-semibold">#${s.id}</td><td class="p-4">${new Date(s.created).toLocaleString()}</td><td class="p-4">${escapeHTML(s.items)}</td><td class="p-4 capitalize">${s.payment}</td><td class="p-4 font-semibold">${money(s.total)}</td></tr>`,
        )
        .join(""),
    );
  } else if (page === "session") {
    title = "Register session";
    desc =
      "Open the till before selling. Expected cash includes cash sales and petty cash movements.";
    body = session
      ? `<div class="grid gap-4 sm:grid-cols-3">${[
          ["Opened", new Date(session.openedAt).toLocaleString()],
          ["Opening cash", money(session.openingCash)],
          ["Expected cash", money(session.expectedCash)],
        ]
          .map(
            (x) =>
              `<div class="rounded-xl border bg-white p-5"><p class="text-xs text-gray-400">${x[0]}</p><p class="mt-2 text-xl font-semibold">${x[1]}</p></div>`,
          )
          .join(
            "",
          )}</div><form data-form="close-session" class="mt-6 max-w-md rounded-xl border bg-white p-5">${amountField("Counted closing cash", "closingCash")}<button class="mt-4 rounded-lg bg-ink px-5 py-3 text-sm text-white">Close session</button></form>`
      : `<form data-form="open-session" class="max-w-md rounded-xl border bg-white p-5">${amountField("Opening cash", "openingCash", "0.00")}<button class="mt-4 rounded-lg bg-accent px-5 py-3 text-sm text-white">Open register session</button></form>`;
  } else if (page === "inventory") {
    title = "Inventory";
    desc = "Current stock plus an audit trail. No expiry or batch tracking.";
    body =
      `<form data-form="adjust" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-4"><label class="text-xs">Product<select name="productId" required class="mt-2 h-11 w-full rounded-lg border px-3">${products.map((p) => `<option value="${p.id}">${escapeHTML(p.name)}</option>`)}</select></label>${field("Quantity (+ or −)", "quantity", "number")}${field("Reason", "note")}<button class="mt-6 h-11 rounded-lg bg-ink text-sm text-white">Record adjustment</button></form>` +
      table(
        ["Product", "Category", "Cost", "Price", "Stock", "Reorder"],
        products
          .map(
            (p) =>
              `<tr class="border-b"><td class="p-4 font-medium">${escapeHTML(p.name)}</td><td class="p-4">${escapeHTML(p.category)}</td><td class="p-4">${money(p.cost)}</td><td class="p-4">${money(p.price)}</td><td class="p-4 font-semibold">${p.stock}</td><td class="p-4">${p.reorderLevel}</td></tr>`,
          )
          .join(""),
      ) +
      `<h2 class="mb-3 mt-7 font-semibold">Recent movements</h2>` +
      table(
        ["Date", "Product", "Type", "Quantity", "Note"],
        movements
          .map(
            (m) =>
              `<tr class="border-b"><td class="p-4">${new Date(m.created).toLocaleString()}</td><td class="p-4">${escapeHTML(m.product)}</td><td class="p-4 capitalize">${escapeHTML(m.kind)}</td><td class="p-4 font-semibold ${m.quantity > 0 ? "text-green-600" : "text-red-500"}">${m.quantity > 0 ? "+" : ""}${m.quantity}</td><td class="p-4">${escapeHTML(m.note)}</td></tr>`,
          )
          .join(""),
      );
  } else if (page === "purchases") {
    title = "Purchases";
    desc =
      "Receive supplier stock and update product costs in one transaction.";
    body =
      `<form data-form="purchase" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-5">${field("Supplier", "supplier")}${field("Invoice", "invoiceNumber")}<label class="text-xs">Product<select name="productId" required class="mt-2 h-11 w-full rounded-lg border px-3">${products.map((p) => `<option value="${p.id}">${escapeHTML(p.name)}</option>`)}</select></label>${field("Quantity", "quantity", "number", "1")}${amountField("Unit cost", "unitCost")}<button class="h-11 rounded-lg bg-ink text-sm text-white sm:col-span-5">Receive purchase</button></form>` +
      table(
        ["Purchase", "Date", "Supplier", "Invoice", "Items", "Total"],
        purchases
          .map(
            (p) =>
              `<tr class="border-b"><td class="p-4 font-semibold">#${p.id}</td><td class="p-4">${new Date(p.created).toLocaleString()}</td><td class="p-4">${escapeHTML(p.supplier)}</td><td class="p-4">${escapeHTML(p.invoiceNumber || "—")}</td><td class="p-4">${escapeHTML(p.items)}</td><td class="p-4 font-semibold">${money(p.total)}</td></tr>`,
          )
          .join(""),
      );
  } else {
    title = "Petty cash";
    desc = "Record cash paid into or taken from the active register session.";
    body =
      `<form data-form="petty" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-4"><label class="text-xs">Direction<select name="direction" class="mt-2 h-11 w-full rounded-lg border px-3"><option value="out">Cash out</option><option value="in">Cash in</option></select></label>${amountField("Amount", "amount")}${field("Category", "category")}${field("Note", "note")}<button class="h-11 rounded-lg bg-ink text-sm text-white sm:col-span-4">Save entry</button></form>` +
      table(
        ["Date", "Direction", "Category", "Note", "Amount"],
        petty
          .map(
            (p) =>
              `<tr class="border-b"><td class="p-4">${new Date(p.created).toLocaleString()}</td><td class="p-4 capitalize">${escapeHTML(p.direction)}</td><td class="p-4">${escapeHTML(p.category)}</td><td class="p-4">${escapeHTML(p.note)}</td><td class="p-4 font-semibold ${p.direction === "in" ? "text-green-600" : "text-red-500"}">${p.direction === "in" ? "+" : "−"}${money(p.amount)}</td></tr>`,
          )
          .join(""),
      );
  }
  $("#other-page").innerHTML =
    `<div class="mb-7"><h1 class="text-2xl font-semibold">${title}</h1><p class="mt-2 text-sm text-gray-400">${desc}</p></div>${body}`;
  if (!products.length && ["inventory", "purchases"].includes(page)) {
    $("#other-page").querySelectorAll("form button, form input, form select").forEach((el) => el.disabled = true);
    $("#other-page").insertAdjacentHTML("afterbegin", '<p class="mb-4 text-sm text-gray-500">Add a product in <a href="/products" data-page="products" class="text-orange-600">Products</a> before recording stock.</p>');
  }
}
async function loadPage() {
  const request = ++pageRequest, requestedPage = page;
  try {
    const endpoints = { sales: "/api/sales", inventory: "/api/inventory/movements", purchases: "/api/purchases", petty: "/api/petty-cash", users: "/api/users" };
    const [savedSettings, savedProducts, savedSession, records] = await Promise.all([
      api("/api/settings"), api("/api/products"), api("/api/session"),
      endpoints[requestedPage] ? api(endpoints[requestedPage]) : Promise.resolve(null),
    ]);
    if (request !== pageRequest) return;
    applySettings(savedSettings);
    products = savedProducts;
    session = savedSession;
    if (requestedPage === "sales") sales = records;
    if (requestedPage === "inventory") movements = records;
    if (requestedPage === "purchases") purchases = records;
    if (requestedPage === "petty") petty = records;
    if (requestedPage === "users") users = records;
    renderProducts();
    renderCart();
    renderOther();
  } catch (e) {
    if (request !== pageRequest) return;
    toast(e.message);
    if (page !== "pos") $("#other-page").innerHTML = '<p class="text-sm text-red-500">Could not load this page. Select the menu again to retry.</p>';
  }
}
async function navigate(next, push = true) {
  if (!currentUser) return;
  if (!canVisit(next)) { next = "pos"; push = false; }
  page = next;
  const path = "/" + page;
  if (location.pathname !== path) history[push ? "pushState" : "replaceState"]({}, "", path);
  $("#pos-page").classList.toggle("hidden", page !== "pos");
  $("#other-page").classList.toggle("hidden", page === "pos");
  $("#breadcrumb").textContent = pageTitles[page];
  document.title = pageTitles[page] + " — Counter";
  document.querySelectorAll("[data-page]").forEach((el) => {
    el.classList.toggle("active", el.dataset.page === page);
    if (el.dataset.page === page) el.setAttribute("aria-current", "page");
    else el.removeAttribute("aria-current");
  });
  if (page !== "pos") $("#other-page").innerHTML = '<p class="text-sm text-gray-500">Loading…</p>';
  await loadPage();
}
document.addEventListener("click", async (e) => {
  const p = e.target.closest("[data-product]"),
    c = e.target.closest("[data-category]"),
    d = e.target.closest("[data-delta]"),
    n = e.target.closest("[data-page]"),
    pay = e.target.closest("[data-payment]"),
    toggle = e.target.closest("[data-user-toggle]");
  if (p) add(Number(p.dataset.product));
  if (c) {
    category = c.dataset.category;
    renderProducts();
  }
  if (d) add(Number(d.dataset.id), Number(d.dataset.delta));
  if (n && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey && e.button === 0) {
    e.preventDefault();
    navigate(n.dataset.page);
  }
  const edit = e.target.closest("[data-edit-product]");
  if (edit) { editingProduct = Number(edit.dataset.editProduct); renderOther(); }
  if (e.target.closest("[data-cancel-product]")) { editingProduct = null; renderOther(); }
  const remove = e.target.closest("[data-delete-product]"), removeAll = e.target.closest("[data-delete-all]");
  if (remove || removeAll) {
    const target = remove || removeAll;
    if (target.disabled) return;
    let body;
    if (removeAll) {
      const confirmation = window.prompt("Remove all products from the catalog? Past records will be kept. Type DELETE ALL PRODUCTS to confirm.");
      if (confirmation !== "DELETE ALL PRODUCTS") return;
      body = JSON.stringify({ confirmation });
    } else {
      const product = products.find((p) => p.id === Number(remove.dataset.deleteProduct));
      if (!product || !window.confirm(`Delete "${product.name}" from the catalog? Past records will be kept.`)) return;
    }
    target.disabled = true;
    try {
      await api(removeAll ? "/api/products" : `/api/products/${remove.dataset.deleteProduct}`, {
        method: "DELETE", headers: { "Content-Type": "application/json" }, body,
      });
      editingProduct = null;
      await loadPage();
      toast(removeAll ? "All products removed from the catalog" : "Product deleted");
    } catch (err) { toast(err.message); }
    finally { target.disabled = false; }
  }
  if (toggle) {
    try {
      await api(`/api/users/${toggle.dataset.userToggle}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ active: toggle.dataset.active === "true" }),
      });
      toast("User access updated");
      await loadPage();
    } catch (err) {
      toast(err.message);
    }
  }
  if (pay) {
    payment = pay.dataset.payment;
    document.querySelectorAll(".payment").forEach((el) => {
      el.classList.toggle("border-orange-300", el.dataset.payment === payment);
      el.classList.toggle("bg-orange-50", el.dataset.payment === payment);
      el.classList.toggle("text-orange-600", el.dataset.payment === payment);
    });
  }
});
$("#search").addEventListener("input", renderProducts);
document.addEventListener("keydown", (e) => {
  if (
    e.key === "/" &&
    !["INPUT", "TEXTAREA"].includes(document.activeElement.tagName)
  ) {
    e.preventDefault();
    $("#search").focus();
  }
});
$("#clear").onclick = () => {
  if (!busy) {
    cart.clear();
    renderCart();
  }
};
["dine-in", "takeaway"].forEach(
  (id) =>
    ($("#" + id).onclick = () => {
      service = id === "dine-in" ? "Dine in" : "Takeaway";
      ["dine-in", "takeaway"].forEach((x) => {
        const el = $("#" + x);
        el.classList.toggle("bg-white", id === x);
        el.classList.toggle("shadow-sm", id === x);
        el.classList.toggle("text-gray-500", id !== x);
      });
    }),
);
$("#checkout").onclick = async () => {
  if (busy || !cart.size) return;
  busy = true;
  renderCart();
  try {
    const sale = await api("/api/checkout", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        items: [...cart].map(([productId, quantity]) => ({
          productId,
          quantity,
        })),
        payment,
      }),
    });
    $("#receipt").innerHTML =
      `<div class="text-center"><div class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-green-50 text-xl text-green-600">✓</div><h2 class="text-xl font-semibold">Order complete!</h2><p class="mt-2 text-xs text-gray-400">The Daily Grind · ${service}</p><p class="mt-1 text-xs text-gray-400">Order #${String(sale.id).padStart(4, "0")} · ${new Date(sale.created).toLocaleString()}</p></div><div class="my-6 border-y border-dashed py-5 text-sm leading-7">${[
        ...cart,
      ]
        .map(([id, q]) => {
          const p = products.find((p) => p.id === id);
          return `<div class="flex justify-between"><span>${q} × ${escapeHTML(p.name)}</span><span>${money(q * p.price)}</span></div>`;
        })
        .join(
          "",
        )}</div><div class="flex justify-between font-semibold"><span>Total</span><span>${money(sale.total)}</span></div><p class="mt-3 text-xs capitalize text-gray-400">Payment recorded: ${sale.payment}</p><p class="mt-6 text-center text-xs text-gray-400">Thanks for stopping by. See you again soon!</p>`;
    cart.clear();
    $("#receipt-dialog").showModal();
    try {
      products = await api("/api/products");
      renderProducts();
    } catch (e) {
      toast("Sale saved. Reload to refresh inventory.");
    }
  } catch (e) {
    toast(e.message);
  } finally {
    busy = false;
    renderCart();
  }
};
$("#close-receipt").onclick = () => $("#receipt-dialog").close();
$("#print").onclick = () => window.print();
$("#date").textContent = new Date().toLocaleDateString("en-US", {
  weekday: "short",
  month: "short",
  day: "numeric",
  year: "numeric",
});
$("#logout").onclick = async () => {
  await fetch("/api/auth/logout", { method: "POST" });
  location.replace("/login");
};
$("#other-page").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = e.target,
    data = Object.fromEntries(new FormData(f)),
    kind = f.dataset.form;
  if (f.dataset.saving) return;
  f.dataset.saving = "true";
  const submit = f.querySelector('button:not([type="button"])');
  if (submit) submit.disabled = true;
  try {
    if (kind === "product") {
      await api(f.dataset.id ? `/api/products/${f.dataset.id}` : "/api/products", {
        method: f.dataset.id ? "PUT" : "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: data.name, category: data.category,
          price: minorUnits(data.price), cost: minorUnits(data.cost),
          stock: f.dataset.id ? 0 : Number(data.stock),
          reorderLevel: Number(data.reorderLevel), art: data.art, color: data.color,
        }),
      });
      editingProduct = null;
      await loadPage();
      toast(f.dataset.id ? "Product updated" : "Product added");
      return;
    }
    if (kind === "settings") {
      const button = f.querySelector("button");
      button.disabled = true;
      try {
        applySettings(await api("/api/settings", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ currency: data.currency }),
        }));
        renderProducts();
        renderCart();
        renderOther();
        toast("Currency settings saved");
      } finally {
        button.disabled = false;
      }
      return;
    }
    if (kind === "open-session")
      await api("/api/session/open", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ openingCash: minorUnits(data.openingCash) }),
      });
    if (kind === "close-session") {
      const result = await api("/api/session/close", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ closingCash: minorUnits(data.closingCash) }),
      });
      toast(`Session closed · difference ${money(result.difference)}`);
    }
    if (kind === "petty")
      await api("/api/petty-cash", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          direction: data.direction,
          amount: minorUnits(data.amount),
          category: data.category,
          note: data.note,
        }),
      });
    if (kind === "purchase")
      await api("/api/purchases", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          supplier: data.supplier,
          invoiceNumber: data.invoiceNumber,
          items: [
            {
              productId: Number(data.productId),
              quantity: Number(data.quantity),
              unitCost: minorUnits(data.unitCost),
            },
          ],
        }),
      });
    if (kind === "adjust")
      await api("/api/inventory/adjustments", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          productId: Number(data.productId),
          quantity: Number(data.quantity),
          note: data.note,
        }),
      });
    if (kind === "user")
      await api("/api/users", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: data.email,
          password: data.password,
          role: data.role,
        }),
      });
    products = await api("/api/products");
    renderProducts();
    toast(
      kind === "open-session"
        ? "Register session opened"
        : "Saved successfully",
    );
    await loadPage();
  } catch (err) {
    toast(err.message);
  } finally {
    delete f.dataset.saving;
    if (submit) submit.disabled = false;
  }
});
(async () => {
  renderCart();
  try {
    const loaded = await Promise.all([
      api("/api/products"),
      api("/api/session"),
      api("/api/auth/me"),
      api("/api/settings"),
    ]);
    [products, session, currentUser] = loaded;
    applySettings(loaded[3]);
    renderCart();
    document.querySelectorAll("[data-roles]").forEach((el) => {
      el.classList.toggle(
        "hidden",
        !el.dataset.roles.split(" ").includes(currentUser.role),
      );
    });
    $("#user-email").textContent = currentUser.email;
    $("#user-role").textContent = currentUser.role;
    $("#user-initials").textContent = currentUser.email
      .slice(0, 2)
      .toUpperCase();
    await navigate(pageFromURL(), false);
    if (!session) toast("Open a register session before making sales");
  } catch (e) {
    $("#products").innerHTML =
      '<p class="col-span-full py-16 text-center text-sm text-red-500">Could not load the menu. Please reload to try again.</p>';
    toast(e.message);
  }
})();
