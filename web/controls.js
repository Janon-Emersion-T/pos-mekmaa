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
  const carryForwardRows = carryForwardOptions.filter(r => r.currency === settings.currency && r.currentAvailableCash > 0);
  const reconciliationRows = registerReconciliations.slice(0, 25);
  return `<h2 class="mb-3 mt-7 font-semibold">Assign opening cash</h2>
    <form data-form="cash-allocation" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-4">
      <label class="text-xs">Staff member<select name="userId" required class="mt-2 h-11 w-full rounded-lg border px-3">${staff.map(u=>`<option value="${u.id}">${escapeHTML(u.email)} · ${escapeHTML(u.role)}</option>`).join("")}</select></label>
      ${amountField("Opening cash", "amount", "0.00")}
      <label class="text-xs">Funding type
        <select name="fundingType" required class="mt-2 h-11 w-full rounded-lg border px-3">
          <option value="new_funds">New funds — transfer from source</option>
          <option value="carry_forward">Carry-forward — verified cash</option>
        </select>
      </label>
      <label class="text-xs sm:col-span-2">Funding source
        <select name="fundingAccountId" class="mt-2 h-11 w-full rounded-lg border px-3">
          <option value="">Select a funding account</option>
          ${fundingSources.filter(a=>a.currency===settings.currency).map(a=>`<option value="${a.id}">${a.accountType==="central_cash"?"Central cash":"Superadmin cash"} · ${money(a.balance,a.currency)}</option>`).join("")}
        </select>
      </label>
      <label class="text-xs sm:col-span-2">Verified carry-forward
        <select name="reconciliationId" class="mt-2 h-11 w-full rounded-lg border px-3">
          <option value="">Select verified cash</option>
          ${carryForwardRows.map(r=>`<option value="${r.id}" data-user-id="${r.userId}" data-available="${r.currentAvailableCash}">${escapeHTML(r.userEmail)} · session #${r.sessionId} · ${money(r.currentAvailableCash,r.currency)}</option>`).join("")}
        </select>
      </label>
      ${field("Note", "note", "text", "Session start cash")}
      <p class="text-xs text-gray-500 sm:col-span-4">New funds transfer from the selected source when the register opens. Carry-forward uses verified physical cash from a closed register and creates no new ledger credit.</p>
      <button class="mt-6 h-11 rounded-lg bg-ink text-sm text-white">Assign cash</button>
    </form>
    ${table(["Staff","Amount","Source","Assigned by","Assigned"],openingAllocations.map(a=>`<tr class="border-b"><td class="p-4">${escapeHTML(a.userEmail)}</td><td class="p-4 font-semibold">${money(a.amount,a.currency)}</td><td class="p-4">${escapeHTML((a.fundingType||"legacy_unclassified").replaceAll("_"," "))}${a.reconciliationId?` · reconciliation #${a.reconciliationId}`:""}</td><td class="p-4">${escapeHTML(a.assignedByEmail)}</td><td class="p-4">${new Date(a.createdAt).toLocaleString()}</td></tr>`).join(""))}
    <h2 class="mb-3 mt-7 font-semibold">Register reconciliations</h2>
    ${table(["Session","Staff","Count","Difference","Available","Status","Review"],reconciliationRows.map(r=>`<tr class="border-b"><td class="p-4">#${r.sessionId}</td><td class="p-4">${escapeHTML(r.userEmail)}</td><td class="p-4">${money(r.countedCash,r.currency)} of ${money(r.expectedCash,r.currency)}</td><td class="p-4 ${r.cashDifference<0?"text-red-600":"text-green-600"}">${money(r.cashDifference,r.currency)}</td><td class="p-4">${r.status==="verified"?money(r.currentAvailableCash,r.currency):"—"}</td><td class="p-4 capitalize">${escapeHTML(r.status)}</td><td class="p-4">${r.status==="pending"?`<form data-form="reconciliation-review" data-reconciliation-id="${r.id}" class="grid gap-2"><input name="remarks" maxlength="500" placeholder="Remarks" class="h-10 rounded-lg border px-3 text-xs"><div class="flex gap-2"><button name="action" value="verify" class="rounded-lg bg-ink px-3 py-2 text-xs text-white">Verify</button><button name="action" value="reject" class="rounded-lg border px-3 py-2 text-xs text-red-600">Reject</button></div></form>`:escapeHTML(r.remarks||"")}</td></tr>`).join(""))}
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

  const debtors = customers
    .map(c => ({
      ...c,
      dueBalances: (c.balances || []).filter(b => b.outstanding > 0),
    }))
    .filter(c => c.dueBalances.length)
    .sort((a, b) =>
      b.dueBalances.reduce((sum, x) => sum + x.outstanding, 0) -
      a.dueBalances.reduce((sum, x) => sum + x.outstanding, 0)
    );

  const totals = new Map();
  for (const customer of debtors) {
    for (const balance of customer.dueBalances) {
      const current = totals.get(balance.currency) || { outstanding: 0, overdue: 0 };
      current.outstanding += balance.outstanding;
      current.overdue += Math.max(0, balance.overdue || 0);
      totals.set(balance.currency, current);
    }
  }

  const totalText = [...totals]
    .map(([currency, value]) => money(value.outstanding, currency))
    .join(" · ");

  el.innerHTML = `<div class="rounded-lg border border-gray-100 bg-white p-3 text-xs">
    <div class="flex items-center justify-between gap-2">
      <span class="text-gray-500">Current cash in hand</span>
      <strong>${session ? money(session.expectedCash, session.currency) : "No open session"}</strong>
    </div>
    ${session ? `<div class="mt-2 grid grid-cols-2 gap-2 text-[10px] text-gray-500">
      <span>Cash sales ${money(session.cashSales || 0, session.currency)}</span>
      <span>Collections ${money(session.cashCollections || 0, session.currency)}</span>
    </div>` : ""}
  </div>
  <section id="pos-debtors-panel" class="rounded-lg border border-gray-200 bg-white p-3 text-xs">
    <div class="flex items-center justify-between gap-2">
      <h3 class="font-semibold text-gray-800">Debtors (${debtors.length})</h3>
      <a href="/customers" data-page="customers" class="font-medium text-orange-600">View all</a>
    </div>
    <div class="mt-3 rounded-lg bg-orange-50 p-3">
      <p class="text-[10px] font-medium uppercase tracking-wide text-orange-700">Total outstanding</p>
      <p class="mt-1 text-base font-bold text-gray-900">${totalText || money(0, settings.currency)}</p>
    </div>
    ${debtors.length ? `<div class="mt-3 max-h-72 space-y-2 overflow-y-auto" aria-label="Customers with outstanding balances">
      ${debtors.map(c => `<a href="/customers?id=${c.id}&collect=1" data-page="customers"
        class="block rounded-lg border border-gray-100 bg-gray-50 p-3 hover:border-orange-200 hover:bg-orange-50">
        <div class="flex items-start justify-between gap-2">
          <span class="min-w-0 truncate font-semibold text-gray-800">${escapeHTML(c.name)}${c.isOwner ? ' <span class="text-orange-600">(Owner)</span>' : ''}</span>
          <span class="shrink-0 text-right font-semibold text-gray-900">${c.dueBalances.map(b => money(b.outstanding, b.currency)).join(" · ")}</span>
        </div>
        ${c.dueBalances.some(b => b.overdue > 0) ?
          `<p class="mt-1 text-[10px] text-red-600">Overdue: ${c.dueBalances.filter(b => b.overdue > 0).map(b => money(b.overdue, b.currency)).join(" · ")}</p>` : ""}
        <p class="mt-1 text-[10px] text-orange-600">View account / collect payment →</p>
      </a>`).join("")}
    </div>` : '<p class="mt-3 text-gray-500">No customers currently owe money.</p>'}
  </section>`;
}

$("#other-page").addEventListener("submit", async e => {
  const form = e.target, kind = form.dataset.form;
  if (!["inventory-filter","cash-allocation","database-reset","credit-policy","opening-correction","payment-reversal","admin-close","reconciliation-review"].includes(kind)) return;
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
      const fundingType = data.fundingType;
      const fundingAccountId = fundingType === "new_funds"
        ? Number(data.fundingAccountId)
        : null;
      const reconciliationId = fundingType === "carry_forward"
        ? Number(data.reconciliationId)
        : null;
      const amount = minorUnits(data.amount);
      if (fundingType === "new_funds") {
        const selected = fundingSources.find(a => a.id === fundingAccountId);
        if (!selected || amount <= 0 || amount > selected.balance) {
          throw new Error("Select a funding account with sufficient available balance.");
        }
      } else {
        const selected = carryForwardOptions.find(r => r.id === reconciliationId);
        if (!selected || Number(data.userId) !== selected.userId || amount <= 0 || amount > selected.currentAvailableCash) {
          throw new Error("Select verified carry-forward cash for this staff member.");
        }
      }
      await api("/api/register-cash-allocations", jsonRequest({
        userId:Number(data.userId),
        amount,
        note:data.note,
        fundingType,
        fundingAccountId,
        reconciliationId
      }));
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
    } else if (kind === "reconciliation-review") {
      const action = e.submitter?.value || data.action;
      const result = await api(`/api/register-cash-reconciliations/${form.dataset.reconciliationId}/review`, jsonRequest({action,remarks:data.remarks || ""}));
      await loadPage(); toast(action === "verify" ? `Reconciliation verified · available ${money(result.verifiedAvailableCash)}` : "Reconciliation rejected");
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
