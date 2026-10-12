function sessionBreakdown(x) {
  return `<div class="mb-5 space-y-2 rounded-lg bg-gray-50 p-4 text-xs">
    <div class="flex justify-between"><span>Opening cash</span><strong>${money(x.openingCash,x.currency)}</strong></div>
    <div class="flex justify-between"><span>Cash sales</span><strong>${money(x.cashSales || 0,x.currency)}</strong></div>
    <div class="flex justify-between"><span>Cash collected from customers</span><strong>${money(x.cashCollections || 0,x.currency)}</strong></div>
    <div class="flex justify-between"><span>Petty cash in</span><strong>${money(x.pettyIn || 0,x.currency)}</strong></div>
    <div class="flex justify-between"><span>Cash refunds</span><strong>${money(x.cashRefunds || 0,x.currency)}</strong></div>
    <div class="flex justify-between"><span>Petty cash out</span><strong>${money(x.pettyOut || 0,x.currency)}</strong></div>
    <div class="flex justify-between border-t pt-2 text-sm"><span>Current cash in hand</span><strong>${money(x.expectedCash,x.currency)}</strong></div>
  </div>`;
}

function assignedOpeningCash() {
  return openingAllocations.find(a => a.userId === currentUser?.id && a.status === "pending");
}

function assignedOpeningCashValue() {
  const assigned = assignedOpeningCash();
  return assigned ? (assigned.amount / 100).toFixed(2) : "0.00";
}

function assignedCashNotice() {
  const assigned = assignedOpeningCash();
  if (currentUser?.role === "superadmin") return '<p class="mb-4 text-xs text-gray-500">Superadmins can open a register directly or assign opening cash to staff below.</p>';
  if (!assigned) return '<p class="mb-4 rounded-lg bg-orange-50 p-3 text-xs text-orange-700">No opening cash has been assigned to you yet. Ask the superadmin before opening your register.</p>';
  return `<p class="mb-4 rounded-lg bg-green-50 p-3 text-xs text-green-700">Assigned opening cash: <strong>${money(assigned.amount,assigned.currency)}</strong>. Enter the exact same amount to start.</p>`;
}

function adminSessionControls() {
  if (currentUser?.role !== "superadmin") return "";
  const staff = users.filter(u => u.active && u.id !== currentUser.id);
  const openSessions = sessionHistory.filter(x => x.status === "open");
  return `<h2 class="mb-3 mt-7 font-semibold">Assign opening cash</h2>
    <form data-form="cash-allocation" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-4">
      <label class="text-xs">Staff member<select name="userId" required class="mt-2 h-11 w-full rounded-lg border px-3">${staff.map(u=>`<option value="${u.id}">${escapeHTML(u.email)} · ${escapeHTML(u.role)}</option>`).join("")}</select></label>
      ${amountField("Opening cash", "amount", "0.00")}
      ${field("Note", "note", "text", "Session start cash")}
      <button class="mt-6 h-11 rounded-lg bg-ink text-sm text-white">Assign cash</button>
    </form>
    ${table(["Staff","Amount","Assigned by","Note","Assigned"],openingAllocations.map(a=>`<tr class="border-b"><td class="p-4">${escapeHTML(a.userEmail)}</td><td class="p-4 font-semibold">${money(a.amount,a.currency)}</td><td class="p-4">${escapeHTML(a.assignedByEmail)}</td><td class="p-4">${escapeHTML(a.note)}</td><td class="p-4">${new Date(a.createdAt).toLocaleString()}</td></tr>`).join(""))}
    <h2 class="mb-3 mt-7 font-semibold">Open staff sessions</h2>
    ${table(["Session","Staff","Current cash","Admin close"],openSessions.map(x=>`<tr class="border-b"><td class="p-4">#${x.id}</td><td class="p-4">${escapeHTML(x.openedByEmail)}</td><td class="p-4 font-semibold">${money(x.expectedCash,x.currency)}</td><td class="p-4"><form data-form="admin-close" data-session-id="${x.id}" data-expected="${x.expectedCash}" class="grid gap-2 sm:grid-cols-3">${amountField("Counted cash","closingCash",(x.expectedCash/100).toFixed(2))}<input name="reason" required maxlength="500" placeholder="Reason" class="mt-5 h-11 rounded-lg border px-3 text-xs sm:col-span-2"><button class="rounded-lg bg-ink px-4 py-2 text-xs text-white sm:col-span-3">Close session</button></form></td></tr>`).join(""))}`;
}

function pendingWriteBanner() {
  return "";
}

async function durableWrite(kind, path, request) {
  return api(path, jsonRequest({...request, requestId: crypto.randomUUID()}));
}

function customerControls(customer, openingBalance) {
  if (!["superadmin","admin"].includes(currentUser?.role)) return "";
  return `<details class="mb-5 rounded-xl border bg-white p-5">
    <summary class="cursor-pointer font-semibold">Credit controls</summary>
    <form data-form="credit-policy" data-customer-id="${customer.id}" class="mt-5 grid gap-3 sm:grid-cols-3">
      ${amountField("Credit limit", "creditLimit", customer.creditLimit == null ? "0.00" : (customer.creditLimit/100).toFixed(2))}
      <label class="text-xs">Currency<select name="currency" class="mt-2 h-11 w-full rounded-lg border px-3">${settings.currencies.map(c=>`<option value="${c.code}" ${c.code === (customer.creditCurrency || settings.currency) ? "selected" : ""}>${c.code}</option>`).join("")}</select></label>
      <label class="text-xs">Payment terms days<input name="paymentTermsDays" type="number" min="0" max="365" step="1" value="${customer.paymentTermsDays ?? 30}" class="mt-2 h-11 w-full rounded-lg border px-3"></label>
      <button class="rounded-lg bg-ink px-5 py-3 text-sm text-white sm:col-span-3">Save credit policy</button>
    </form>
    ${openingBalance ? `<form data-form="opening-correction" data-customer-id="${customer.id}" data-expected="${openingBalance.amount}" class="mt-5 grid gap-3 border-t pt-5 sm:grid-cols-3">
      ${amountField("Corrected opening debt", "amount", (openingBalance.amount/100).toFixed(2))}
      <label class="text-xs">Due date<input name="dueDate" type="date" value="${escapeHTML(openingBalance.dueDate || "")}" class="mt-2 h-11 w-full rounded-lg border px-3"></label>
      ${field("Reason", "reason")}
      <button class="rounded-lg bg-ink px-5 py-3 text-sm text-white sm:col-span-3">Save opening balance correction</button>
    </form>` : ""}
  </details>`;
}

function paymentControls(p) {
  const receipt = `<button data-payment-receipt="${p.id}" class="mr-3 text-orange-600">Receipt</button>`;
  if (p.reversed || !["superadmin","admin"].includes(currentUser?.role)) return receipt;
  return receipt + `<button data-payment-reverse="${p.id}" class="text-red-600">Reverse</button>`;
}

function renderPosInsights() {
  const el = $("#pos-insights");
  if (!el) return;
  const debtors = customers.filter(c => c.balances?.some(b => b.outstanding > 0)).slice(0, 5);
  el.innerHTML = `<div class="rounded-lg border border-gray-100 bg-white p-3 text-xs">
    <div class="flex items-center justify-between"><span class="text-gray-500">Current cash in hand</span><strong>${session ? money(session.expectedCash,session.currency) : "No open session"}</strong></div>
    ${session ? `<div class="mt-2 grid grid-cols-2 gap-2 text-[10px] text-gray-500"><span>Cash sales ${money(session.cashSales || 0,session.currency)}</span><span>Collections ${money(session.cashCollections || 0,session.currency)}</span></div>` : ""}
  </div>
  <div class="rounded-lg border border-gray-100 bg-white p-3 text-xs">
    <div class="mb-2 flex items-center justify-between"><span class="font-medium">Customers to collect</span><a href="/customers" data-page="customers" class="text-orange-600">View all</a></div>
    ${debtors.length ? debtors.map(c=>`<a href="/customers?id=${c.id}&collect=1" data-page="customers" class="mb-2 flex items-center justify-between rounded-md bg-gray-50 px-2 py-2"><span class="truncate">${escapeHTML(c.name)}</span><strong>${escapeHTML(c.balances.map(b=>money(b.outstanding,b.currency)).join(" · "))}</strong></a>`).join("") : '<p class="text-gray-400">No outstanding customer balances.</p>'}
  </div>`;
}

$("#other-page").addEventListener("submit", async e => {
  const form = e.target, kind = form.dataset.form;
  if (!["inventory-filter","cash-allocation","database-reset","credit-policy","opening-correction","payment-reversal","admin-close"].includes(kind)) return;
  e.preventDefault();
  if (form.dataset.saving) return;
  const data = Object.fromEntries(new FormData(form));
  const submit = form.querySelector('button:not([type="button"])');
  form.dataset.saving = "true";
  if (submit) submit.disabled = true;
  try {
    if (kind === "inventory-filter") {
      const params = new URLSearchParams(Object.entries(data).filter(([,value])=>value !== ""));
      await navigate("inventory", true, params.size ? "?"+params.toString() : "");
    } else if (kind === "cash-allocation") {
      await api("/api/register-cash-allocations", jsonRequest({userId:Number(data.userId),amount:minorUnits(data.amount),note:data.note}));
      await loadPage(); toast("Opening cash assigned");
    } else if (kind === "database-reset") {
      await api("/api/system/reset", jsonRequest({password:data.password,confirmation:data.confirmation}));
      localStorage.clear(); cart.clear(); pendingCheckout = null; selectedCustomer = null;
      await loadPage(); toast("Database reset complete");
    } else if (kind === "credit-policy") {
      const limit = minorUnits(data.creditLimit);
      await api(`/api/customers/${form.dataset.customerId}/credit-policy`, {...jsonRequest({creditLimit:limit || null,currency:data.currency,paymentTermsDays:Number(data.paymentTermsDays)}), method:"PUT"});
      await loadPage(); toast("Credit policy saved");
    } else if (kind === "opening-correction") {
      await api(`/api/customers/${form.dataset.customerId}/opening-corrections`, jsonRequest({requestId:crypto.randomUUID(),amount:minorUnits(data.amount),expectedAmount:Number(form.dataset.expected),reason:data.reason,dueDate:data.dueDate}));
      await loadPage(); toast("Opening balance corrected");
    } else if (kind === "payment-reversal") {
      await api(`/api/customer-payments/${form.dataset.paymentId}/reverse`, jsonRequest({requestId:crypto.randomUUID(),reason:data.reason}));
      await loadPage(); toast("Payment reversed");
    } else if (kind === "admin-close") {
      const result = await api(`/api/sessions/${form.dataset.sessionId}/close`, jsonRequest({closingCash:minorUnits(data.closingCash),expectedCash:Number(form.dataset.expected),reason:data.reason}));
      await loadPage(); toast(`Session closed · difference ${money(result.difference)}`);
    }
  } catch (error) { toast(error.message); }
  finally { delete form.dataset.saving; if (submit) submit.disabled = false; }
});

document.addEventListener("click", async e => {
  const paymentReceipt = e.target.closest("[data-payment-receipt]");
  const paymentReverse = e.target.closest("[data-payment-reverse]");
  if (paymentReceipt) {
    try {
      const x = await api(`/api/customer-payments/${paymentReceipt.dataset.paymentReceipt}`);
      $("#receipt").innerHTML = `<h2 class="text-xl font-semibold">Payment receipt #${x.payment.id}</h2><p class="mt-2 text-sm">Customer: ${escapeHTML(x.customerName)}</p><p class="mt-4 font-semibold">${money(x.payment.amount,x.payment.currency)}</p><p class="mt-2 text-sm">Method: ${escapeHTML(x.payment.payment)}</p><p class="mt-2 text-xs">${new Date(x.payment.created).toLocaleString()} · ${escapeHTML(x.payment.cashierEmail)}</p>`;
      $("#receipt-dialog").showModal();
    } catch (error) { toast(error.message); }
  }
  if (paymentReverse) {
    const reason = window.prompt("Reason for reversing this payment");
    if (!reason) return;
    const wrapper = document.createElement("form");
    wrapper.dataset.form = "payment-reversal";
    wrapper.dataset.paymentId = paymentReverse.dataset.paymentReverse;
    wrapper.innerHTML = `<input name="reason" value="${escapeHTML(reason)}">`;
    $("#other-page").appendChild(wrapper);
    wrapper.requestSubmit();
    wrapper.remove();
  }
});
