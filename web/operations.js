// Companion functions for the catalog, register, and management screens.
let reportData = { rows: [] }, auditData = [];
const jsonRequest = (body) => ({ method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
const pendingKey = () => `counter.pending-checkout.${currentUser.id}`;
const refundKey = () => `counter.pending-refund.${currentUser.id}`;
function restorePendingCheckout() {
  // Storage may be disabled by browser policy; that must not prevent menu loading.
  try {
    const saved = localStorage.getItem(pendingKey());
    pendingCheckout = saved ? JSON.parse(saved) : null;
    if (pendingCheckout && (!pendingCheckout.requestId || !Array.isArray(pendingCheckout.items))) throw Error("Invalid pending sale");
    const refund = JSON.parse(localStorage.getItem(refundKey()) || "null");
    if (refund?.request?.requestId && Array.isArray(refund.request.items)) {
      refundSaleId = refund.saleId;
      pendingRefund = refund.request;
    }
  } catch {
    pendingCheckout = null;
    toast("Saved checkout could not be read. Check sales history before repeating a previous sale.");
  }
}
function salesQuery() { return location.search; }
function filterInput(label, name, type = "text") {
  const value = new URLSearchParams(location.search).get(name) || "";
  return `<label class="text-xs text-gray-600">${label}<input name="${name}" type="${type}" value="${escapeHTML(value)}" class="mt-2 h-11 w-full rounded-lg border px-3"></label>`;
}
function filterForm(kind, inputs) {
  return `<form data-form="${kind}" class="mb-5 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-3">${inputs}<button class="rounded-lg bg-ink px-4 py-3 text-sm text-white">Apply filters</button></form>`;
}
function pageLinks(size, count) {
  const params = new URLSearchParams(location.search), offset = Number(params.get("offset")) || 0;
  const link = (next, label) => {
    params.set("offset", String(next));
    return `<a href="/${page}?${escapeHTML(params.toString())}" data-page="${page}" class="mr-4 text-sm text-orange-600">${label}</a>`;
  };
  return `<div class="mt-4">${offset > 0 ? link(Math.max(0, offset-size), "Previous") : ""}${count === size ? link(offset+size, "Next") : ""}</div>`;
}
function salesHistoryView() {
  return filterForm("sale-filter", filterInput("Receipt number or product", "q") + filterInput("From", "from", "date") + filterInput("To", "to", "date") + filterInput("Cashier email", "cashier") + filterInput("Session", "sessionId", "number")) +
    table(["Receipt", "Date", "Items", "Cashier", "Payment", "Total", "Refunded", "Actions"], sales.map(s => `<tr class="border-b"><td class="p-4">#${s.id}</td><td class="p-4">${new Date(s.created).toLocaleString()}</td><td class="p-4">${escapeHTML(s.items)}</td><td class="p-4">${escapeHTML(s.cashierEmail)}</td><td class="p-4">${escapeHTML(s.payment)}</td><td class="p-4">${money(s.total,s.currency)}</td><td class="p-4">${money(s.refunded,s.currency)}</td><td class="p-4"><button data-receipt="${s.id}" class="mr-3 text-orange-600">Receipt</button>${!s.deletedAt && ["admin","superadmin"].includes(currentUser.role) ? `<button data-refund="${s.id}" class="mr-3 text-orange-600">Refund</button>` : ""}${!s.deletedAt && currentUser.role === "superadmin" ? `<button data-delete-sale="${s.id}" class="text-red-600">Delete sale</button>` : ""}</td></tr>`).join("")) + pageLinks(100, sales.length);
}
function dailyReportView() {
  const csv = new URLSearchParams(location.search); csv.set("format", "csv");
  return filterForm("report-filter", filterInput("Report date", "date", "date")) + `<p class="mb-4 text-xs text-gray-500">${escapeHTML(reportData.date)} · ${escapeHTML(reportData.timezone)}</p><a href="/api/reports/daily?${escapeHTML(csv.toString())}" class="text-sm text-orange-600">Download CSV</a>` +
    table(["Currency", "Sales", "Cash sales", "Card sales", "Refunds", "Net sales", "Petty cash in", "Petty cash out", "Closing difference"], reportData.rows.map(r => `<tr class="border-b">${[escapeHTML(r.currency), r.saleCount, money(r.cashSales,r.currency), money(r.cardSales,r.currency), money(r.cashRefunds+r.cardRefunds,r.currency), money(r.cashSales+r.cardSales-r.cashRefunds-r.cardRefunds,r.currency), money(r.pettyIn,r.currency), money(r.pettyOut,r.currency), money(r.closingDifference,r.currency)].map(v=>`<td class="p-4">${v}</td>`).join("")}</tr>`).join(""));
}
function auditView() {
  return filterForm("audit-filter", filterInput("Entity", "entity") + filterInput("Staff email", "actor")) +
    table(["Date", "Staff", "Entity", "Action", "Changes"], auditData.map(x => `<tr class="border-b"><td class="p-4">${new Date(x.created).toLocaleString()}</td><td class="p-4">${escapeHTML(x.actorEmail)}</td><td class="p-4">${escapeHTML(x.entity)} #${x.entityId}</td><td class="p-4">${escapeHTML(x.action)}</td><td class="p-4"><details><summary>View changes</summary><pre class="text-xs">${escapeHTML(JSON.stringify({before:x.before,after:x.after},null,2))}</pre></details></td></tr>`).join("")) + pageLinks(50,auditData.length);
}
function accountView() {
  return `<form data-form="password" class="grid max-w-md gap-4 rounded-xl border bg-white p-5">${currentUser.mustChangePassword ? '<p class="text-sm text-orange-600">Change your temporary password before continuing.</p>' : ""}${field("Current password", "currentPassword", "password")}${field("New password (12–72 bytes)", "newPassword", "password")}${field("Confirm new password", "confirmPassword", "password")}<button class="rounded-lg bg-accent px-5 py-3 text-sm text-white">Change password</button><p class="text-xs text-gray-500">You will need to sign in again on all devices.</p></form>`;
}
function showReceipt(sale) {
  $("#receipt").innerHTML = `<h2 class="text-xl font-semibold">Receipt #${sale.id}</h2><p class="mt-2 text-xs">${new Date(sale.created).toLocaleString()}</p><p class="mt-2 text-xs">${escapeHTML(sale.cashierEmail)}</p><div class="my-6 text-sm">${sale.lines ? sale.lines.map(line => `<p>${line.quantity} × ${escapeHTML(line.name)} — ${money(line.quantity*line.unitPrice,sale.currency)}</p>`).join("") : escapeHTML(sale.items)}</div><p class="font-semibold">Total: ${money(sale.total,sale.currency)}</p><p class="mt-2 text-sm">Payment: ${escapeHTML(sale.payment)}</p>${sale.cashReceived != null ? `<p class="text-sm">Cash received: ${money(sale.cashReceived,sale.currency)}</p><p class="text-sm">Change: ${money(sale.changeDue,sale.currency)}</p>` : ""}${sale.deletedAt ? '<p class="mt-3 text-red-600">Deleted sale</p>' : ""}`;
  $("#receipt-dialog").showModal();
}
$("#checkout").onclick = async () => {
  if (busy || currentUser?.mustChangePassword) return;
  busy = true;
  try {
    if (!pendingCheckout) {
      if (!session || !cart.size) return;
      const items = [...cart].map(([productId, quantity]) => ({productId, quantity}));
      const expectedTotal = items.reduce((total, line) => total + products.find(p => p.id === line.productId).price*line.quantity, 0);
      let cashReceived;
      if (payment === "cash") {
        const entered = window.prompt(`Cash received (${settings.currency}). Total: ${money(expectedTotal)}`, (expectedTotal/100).toFixed(2));
        if (entered === null) return;
        cashReceived = minorUnits(entered.trim());
        if (cashReceived < expectedTotal) throw Error("Cash received must cover the total");
      }
      const request = {requestId: crypto.randomUUID(), items, payment, expectedTotal, sessionId:session.id, ...(cashReceived == null ? {} : {cashReceived})};
      // Persist before sending so a lost response can be retried with the same key.
      localStorage.setItem(pendingKey(), JSON.stringify(request));
      pendingCheckout = request;
    }
    renderCart();
    let sale;
    try { sale = await api("/api/checkout", jsonRequest(pendingCheckout)); }
    catch (error) {
      // Network errors and server failures may have committed: retain their key.
      if ([400,403,409].includes(error.status)) {
        localStorage.removeItem(pendingKey());
        pendingCheckout = null;
      }
      throw error;
    }
    localStorage.removeItem(pendingKey());
    pendingCheckout = null;
    cart.clear();
    showReceipt(sale);
    await loadPage();
  } catch (error) { toast(error.message); }
  finally { busy = false; renderCart(); }
};
$("#other-page").addEventListener("submit", async e => {
  const form = e.target, kind = form.dataset.form;
  if (!["sale-filter","report-filter","audit-filter","password"].includes(kind)) return;
  e.preventDefault();
  if (form.dataset.saving) return;
  const data = Object.fromEntries(new FormData(form));
  if (kind !== "password") {
    const params = new URLSearchParams(Object.entries(data).filter(([,value])=>value !== ""));
    await navigate(page, true, params.size ? "?"+params.toString() : "");
    return;
  }
  const submit = form.querySelector("button");
  form.dataset.saving = "true"; submit.disabled = true;
  try {
    if (data.newPassword !== data.confirmPassword) throw Error("New passwords do not match");
    await api("/api/auth/password", jsonRequest({currentPassword:data.currentPassword,newPassword:data.newPassword}));
    location.replace("/login");
  } catch (error) { toast(error.message); }
  finally { delete form.dataset.saving; submit.disabled = false; }
});
document.addEventListener("change", async e => {
  if (e.target.id !== "product-import" || !e.target.files.length) return;
  const input = e.target; input.disabled = true;
  try {
    await api("/api/products/import", {method:"POST",headers:{"Content-Type":"text/csv"},body:input.files[0]});
    await loadPage(); toast("Products imported");
  } catch (error) { toast(error.message); }
  finally { input.disabled = false; input.value = ""; }
});
let refundSaleId = null, pendingRefund = null;
document.addEventListener("click", async e => {
  const receipt = e.target.closest("[data-receipt]"), refund = e.target.closest("[data-refund]");
  if (!receipt && !refund) return;
  try {
    if (receipt) { showReceipt(await api(`/api/sales/${receipt.dataset.receipt}`)); return; }
    const sale = await api(`/api/sales/${pendingRefund ? refundSaleId : refund.dataset.refund}`);
    refundSaleId = sale.id;
    $("#refund-title").textContent = `Refund for receipt #${sale.id}`;
    $("#refund-content").innerHTML = sale.lines.filter(line => line.quantity > line.refunded).map(line => `<label class="mb-3 block text-sm">${escapeHTML(line.name)} (${line.quantity-line.refunded} available)<input type="number" data-return-item="${line.id}" min="0" max="${line.quantity-line.refunded}" step="1" value="0" required class="mt-2 h-11 w-full rounded-lg border px-3"><span class="text-xs"><input type="checkbox" data-restock="${line.id}"> Return to stock</span></label>`).join("") + '<label class="block text-sm">Reason<textarea name="reason" required maxlength="500" class="mt-2 w-full rounded-lg border p-3"></textarea></label><label class="mt-3 block text-sm"><input name="confirmed" type="checkbox" required> I confirm the payment has been refunded to the customer.</label>';
    if (pendingRefund) $("#refund-content").innerHTML = '<p class="text-sm">A previous refund is awaiting confirmation. Retry it to recover the result without recording it twice.</p>';
    $("#refund-submit").textContent = pendingRefund ? "Recover refund" : "Record refund";
    $("#refund-error").textContent = "";
    $("#refund-dialog").showModal();
  } catch (error) { toast(error.message); }
});
$("#refund-cancel").onclick = () => $("#refund-dialog").close();
$("#refund-form").onsubmit = async e => {
  e.preventDefault();
  const form = e.target, button = $("#refund-submit");
  if (button.disabled) return;
  button.disabled = true;
  try {
    if (!pendingRefund) {
      const data = new FormData(form);
      const items = [...form.querySelectorAll("[data-return-item]")].filter(input => Number(input.value)>0).map(input => ({saleItemId:Number(input.dataset.returnItem),quantity:Number(input.value),restock:form.querySelector(`[data-restock="${input.dataset.returnItem}"]`).checked}));
      if (!items.length) throw Error("Select at least one return quantity");
      const request = {requestId:crypto.randomUUID(),items,reason:data.get("reason"),confirmed:data.has("confirmed")};
      localStorage.setItem(refundKey(), JSON.stringify({saleId:refundSaleId,request}));
      pendingRefund = request;
    }
    await api(`/api/sales/${refundSaleId}/refunds`, jsonRequest(pendingRefund));
    localStorage.removeItem(refundKey());
    pendingRefund = null;
    $("#refund-dialog").close();
    await loadPage(); toast("Refund recorded");
  } catch (error) {
    if ([400,403,404,409].includes(error.status)) {
      localStorage.removeItem(refundKey());
      pendingRefund = null;
    }
    $("#refund-error").textContent = error.message;
  } finally { button.disabled = false; }
};
