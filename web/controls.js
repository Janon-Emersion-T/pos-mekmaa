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

function invoiceDetailsView(x) {
  const accounts = fundingSources.filter(a => a.currency === x.currency && ["central_cash","bank","superadmin_cash"].includes(a.accountType));
  const accountOptions = accounts.map(a => `<option value="${a.id}">${escapeHTML(a.accountType.replaceAll("_"," "))} · ${money(a.balance,a.currency)}</option>`).join("");
  return `<section class="mt-8 rounded-xl border bg-white p-5"><div class="flex flex-wrap items-start justify-between gap-4"><div><h2 class="text-xl font-semibold">${escapeHTML(x.supplier)} · ${escapeHTML(x.invoiceNumber||"No invoice number")}</h2><p class="mt-1 text-xs text-gray-500">${new Date(x.date).toLocaleString()} · ${escapeHTML(x.status.replaceAll("_"," "))}</p></div><div class="grid grid-cols-2 gap-x-6 gap-y-1 text-sm"><span>Total</span><strong>${money(x.total,x.currency)}</strong><span>Paid</span><strong>${money(x.paid,x.currency)}</strong><span>Refunded</span><strong>${money(x.refunded,x.currency)}</strong><span>Outstanding</span><strong>${money(x.outstanding,x.currency)}</strong></div></div>
    <div class="mt-6 grid gap-5 lg:grid-cols-2"><form data-form="invoice-payment" data-invoice-id="${x.id}" data-outstanding="${x.outstanding}" data-currency="${escapeHTML(x.currency)}" class="grid gap-3 rounded-lg border p-4"><h3 class="font-semibold">Record supplier payment</h3>${amountField("Amount","amount",(x.outstanding/100).toFixed(2))}<label class="text-xs">Cash or bank account<select name="financialAccountId" required class="mt-2 h-11 w-full rounded-lg border px-3">${accountOptions}</select></label><label class="text-xs">Funding classification<select name="fundingType" class="mt-2 h-11 w-full rounded-lg border px-3"><option value="other">Other</option><option value="reinvested_revenue">Reinvested revenue</option><option value="superadmin_capital">Superadmin capital</option></select></label>${field("Reference","reference")}<label class="text-xs">Notes (optional)<input type="text" name="notes" maxlength="2000" class="mt-2 h-11 w-full rounded-lg border px-3"></label><button ${x.outstanding<=0||!accounts.length?"disabled":""} class="rounded-lg bg-ink px-5 py-3 text-sm text-white disabled:opacity-50">Record payment</button></form>
    <form data-form="invoice-refund" data-invoice-id="${x.id}" data-refundable="${Math.max(x.paid-x.refunded,0)}" data-currency="${escapeHTML(x.currency)}" class="grid gap-3 rounded-lg border p-4"><h3 class="font-semibold">Record supplier refund</h3><p class="text-xs text-gray-500">A refund credits cash but does not change inventory or reopen the invoice liability.</p>${amountField("Refund amount","amount")}<label class="text-xs">Receiving account<select name="financialAccountId" required class="mt-2 h-11 w-full rounded-lg border px-3">${accountOptions}</select></label>${field("Refund reference","reference")}${field("Refund reason","reason")}<label class="text-xs">Notes (optional)<input type="text" name="notes" maxlength="2000" class="mt-2 h-11 w-full rounded-lg border px-3"></label><button ${x.paid-x.refunded<=0||!accounts.length?"disabled":""} class="rounded-lg border border-orange-400 px-5 py-3 text-sm text-orange-700 disabled:opacity-50">Record refund</button></form></div>
    <h3 class="mb-3 mt-7 font-semibold">Linked purchases</h3>${table(["Purchase","Date","Products / quantities / unit costs","Total"],x.purchases.map(p=>`<tr class="border-b"><td class="p-4">#${p.id}</td><td class="p-4">${new Date(p.createdAt).toLocaleString()}</td><td class="p-4">${escapeHTML(p.items)}</td><td class="p-4 font-semibold">${money(p.total,x.currency)}</td></tr>`).join(""))}
    <h3 class="mb-3 mt-7 font-semibold">Transaction history</h3>${table(["Date","Type","Amount","Account","Reference / reason","Notes","Actor"],x.transactions.map(t=>`<tr class="border-b"><td class="p-4">${new Date(t.createdAt).toLocaleString()}</td><td class="p-4 capitalize">${escapeHTML(t.type)}</td><td class="p-4 font-semibold ${t.type==="refund"?"text-green-600":""}">${money(t.amount,t.currency)}</td><td class="p-4 capitalize">${escapeHTML(t.accountType.replaceAll("_"," "))}</td><td class="p-4">${escapeHTML(t.reference)}${t.reason?`<div class="text-xs text-gray-500">${escapeHTML(t.reason)}</div>`:""}</td><td class="p-4">${escapeHTML(t.notes||"—")}</td><td class="p-4">${escapeHTML(t.actorEmail)}</td></tr>`).join(""))}</section>`;
}

function assignedOpeningCash() {
  return openingAllocations.find(a => a.userId === currentUser?.id && a.status === "pending");
}

function assignedOpeningCashValue() {
  const assigned = assignedOpeningCash();
  const amount = assigned ? assigned.amount : (cashHandoverAccount.balance || 0);
  return (amount / 100).toFixed(2);
}

function assignedCashNotice() {
  const assigned = assignedOpeningCash();
  if (currentUser?.role === "superadmin") return '<p class="mb-4 text-xs text-gray-500">Superadmins can open a register directly or assign opening cash to staff below.</p>';
  if (!assigned) return `<p class="mb-4 rounded-lg bg-green-50 p-3 text-xs text-green-700">Your current accountable cash of <strong>${money(cashHandoverAccount.balance || 0,cashHandoverAccount.currency || settings.currency)}</strong> will be used as the opening cash. No new Superadmin assignment is required.</p>`;
  return `<p class="mb-4 rounded-lg bg-green-50 p-3 text-xs text-green-700">Assigned opening cash: <strong>${money(assigned.amount,assigned.currency)}</strong>. Enter the exact same amount to start.</p>`;
}

function handoverStatus(x) {
  return escapeHTML((x.status || "pending").replaceAll("_", " "));
}

function cashHandoverView() {
  const transfers = cashHandoverAccount.transfers || [];
  const pending = transfers.filter(x => x.status === "pending");
  const completed = transfers.filter(x => x.status !== "pending");
  const currency = cashHandoverAccount.currency || settings.currency;
  const rows = items => table(
    ["Requested", "Requested amount", "Approved / rejected", "Status", "Remarks", "Reviewed"],
    items.map(x => `<tr class="border-b"><td class="p-4">${new Date(x.createdAt).toLocaleString()}</td><td class="p-4 font-semibold">${money(x.requestedAmount,x.currency)}</td><td class="p-4">${x.approvedAmount == null ? "—" : money(x.approvedAmount,x.currency)}</td><td class="p-4 capitalize">${handoverStatus(x)}</td><td class="p-4">${escapeHTML(x.remarks||"—")}${x.decisionRemarks ? `<div class="mt-1 text-xs text-gray-500">Review: ${escapeHTML(x.decisionRemarks)}</div>` : ""}</td><td class="p-4">${x.reviewedAt ? `${new Date(x.reviewedAt).toLocaleString()}<div class="text-xs text-gray-500">${escapeHTML(x.reviewerEmail)}</div>` : "—"}</td></tr>`).join("")
  );
  return `<section class="mt-8" aria-labelledby="cash-handover-title">
    <h2 id="cash-handover-title" class="mb-3 font-semibold">Transfer Cash to Superadmin</h2>
    <div class="mb-4 rounded-xl border bg-white p-5"><p class="text-xs text-gray-500">Current accountable cash balance</p><p class="mt-2 text-2xl font-semibold">${money(cashHandoverAccount.balance||0,currency)}</p><p class="mt-2 text-xs text-gray-500">Pending requests do not reduce this balance. Cash moves only after approval.</p></div>
    <form data-form="cash-handover" class="mb-6 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-2">${amountField("Transfer amount","amount")}<label class="text-xs">Remarks (optional)<input name="remarks" maxlength="500" class="mt-2 h-11 w-full rounded-lg border px-3" placeholder="End of shift cash"></label><button class="rounded-lg bg-accent px-5 py-3 text-sm text-white sm:col-span-2">Submit transfer request</button></form>
    <h3 class="mb-3 font-semibold">Pending transfer requests</h3>${rows(pending)}
    <h3 class="mb-3 mt-7 font-semibold">Completed transfer history</h3>${rows(completed)}
  </section>`;
}

function cashHandoverReviewView() {
  const rows = cashHandoverReviews.map(x => `<tr class="border-b align-top"><td class="p-4"><strong>${escapeHTML(x.cashierEmail)}</strong><div class="text-xs text-gray-500">${new Date(x.createdAt).toLocaleString()}</div></td><td class="p-4"><strong>${money(x.requestedAmount,x.currency)}</strong><div class="text-xs text-gray-500">${escapeHTML(x.remarks||"No remarks")}</div></td><td class="p-4 capitalize">${handoverStatus(x)}${x.approvedAmount != null ? `<div class="text-xs">Approved ${money(x.approvedAmount,x.currency)}</div>` : ""}${x.reviewerEmail ? `<div class="text-xs text-gray-500">${escapeHTML(x.reviewerEmail)} · ${x.reviewedAt ? new Date(x.reviewedAt).toLocaleString() : ""}</div>` : ""}${x.decisionRemarks ? `<div class="text-xs text-gray-500">${escapeHTML(x.decisionRemarks)}</div>` : ""}</td><td class="p-4">${x.status === "pending" ? `<form data-form="cash-handover-review" data-handover-id="${x.id}" data-requested="${x.requestedAmount}" data-currency="${escapeHTML(x.currency)}" class="grid min-w-56 gap-2"><input name="approvedAmount" inputmode="decimal" value="${(x.requestedAmount/100).toFixed(2)}" class="h-10 rounded-lg border px-3" aria-label="Approved amount"><input name="remarks" maxlength="500" placeholder="Review remarks" class="h-10 rounded-lg border px-3"><div class="flex flex-wrap gap-2"><button name="action" value="full" class="rounded-lg bg-ink px-3 py-2 text-xs text-white">Approve full</button><button name="action" value="partial" class="rounded-lg border px-3 py-2 text-xs">Approve partial</button><button name="action" value="reject" class="rounded-lg border border-red-300 px-3 py-2 text-xs text-red-600">Reject</button></div></form>` : "—"}</td></tr>`).join("");
  return `<h2 class="mb-3 mt-7 font-semibold">Cash Handover Requests</h2>${table(["Cashier / requested", "Amount / cashier remarks", "Status / review", "Action"], rows)}`;
}

let pendingCapital = null;

function capitalPendingKey() {
  return `counter.pending-capital.${currentUser.id}`;
}

function restorePendingCapital() {
  try {
    const stored = localStorage.getItem(capitalPendingKey());
    if (!stored) {
      pendingCapital = null;
      return null;
    }

    const request = JSON.parse(stored);
    if (!request || typeof request.requestId !== "string" ||
        !/^[a-zA-Z0-9_-]{16,100}$/.test(request.requestId)) {
      throw new Error("Invalid stored capital request");
    }

    pendingCapital = request;
    return pendingCapital;
  } catch {
    // Never discard an in-memory request because storage is unavailable.
    return pendingCapital;
  }
}

function clearPendingCapital() {
  pendingCapital = null;
  try {
    localStorage.removeItem(capitalPendingKey());
  } catch {
    // The server response is authoritative; storage cleanup is best effort.
  }
}

async function sendPendingCapital() {
  if (!pendingCapital) throw new Error("No pending capital request");

  let result;
  try {
    result = await api("/api/capital", jsonRequest(pendingCapital));
  } catch (error) {
    if ([400, 403, 404, 409].includes(error.status)) {
      clearPendingCapital();
    }
    throw error;
  }

  clearPendingCapital();
  toast(`Capital contribution confirmed #${result.id}`);
  await loadPage();
}

function adminSessionControls() {
  if (currentUser?.role !== "superadmin") return "";
  restorePendingCapital();
  const staff = users.filter(u => u.active && u.id !== currentUser.id);
  const openSessions = sessionHistory.filter(x => x.status === "open");
  const carryForwardRows = carryForwardOptions.filter(r => r.currency === settings.currency && r.currentAvailableCash > 0);
  const reconciliationRows = registerReconciliations.slice(0, 25);
  return `${pendingCapital
    ? `<div class="mb-5 rounded-lg border border-orange-300 bg-orange-50 p-4 text-sm">
         A capital transaction is awaiting confirmation. Do not record another contribution.
         <button type="button" data-recover-capital class="ml-2 font-semibold text-orange-700">
           Recover capital transaction
         </button>
       </div>`
    : ""}
    ${cashHandoverReviewView()}
    <h2 class="mb-3 mt-7 font-semibold">Record capital contribution</h2>
    <form data-form="capital-contribution" class="mb-4 grid gap-3 rounded-xl border bg-white p-5 sm:grid-cols-3">
      <p class="text-xs leading-5 text-gray-600 sm:col-span-3">Only record genuine new business funds. Do not re-enter cash already recorded in a register, bank account, or financial ledger.</p>
      <label class="text-xs">Destination account
        <select name="fundingAccountId" required class="mt-2 h-11 w-full rounded-lg border px-3">
          <option value="">Select an account</option>
          ${fundingSources.filter(a=>a.currency===settings.currency).map(a=>`<option value="${a.id}">${escapeHTML({
            central_cash:"Central cash",
            investment_cash:"Investment cash",
            bank:"Bank account"
          }[a.accountType] || a.accountType)} · ${money(a.balance,a.currency)}</option>`).join("")}
        </select>
      </label>
      ${amountField("Capital amount","amount","0.00")}
      <label class="text-xs">Reference
        <input name="reference" required maxlength="200" placeholder="Funding receipt or reference" class="mt-2 h-11 w-full rounded-lg border px-3">
      </label>
      <label class="text-xs sm:col-span-3">Explanation
        <textarea name="notes" required maxlength="2000" rows="2" class="mt-2 w-full rounded-lg border p-3" placeholder="Where the money came from and why it is being introduced"></textarea>
      </label>
      <label class="flex items-start gap-2 text-xs sm:col-span-3">
        <input type="checkbox" name="confirmed" required class="mt-0.5">
        <span>I confirm this is genuine new capital and has not already been recorded.</span>
      </label>
      <button ${pendingCapital ? "disabled" : ""} class="h-11 rounded-lg bg-ink px-5 text-sm text-white disabled:opacity-50 sm:col-span-3">Record capital contribution</button>
    </form>
    <details class="mb-6 rounded-xl border bg-white p-4">
      <summary class="cursor-pointer text-sm font-semibold">Recent capital transactions (${capitalEntries.length})</summary>
      <div class="mt-4">${table(["ID","Type","Amount","Reference","Date"],capitalEntries.map(x=>`<tr class="border-b"><td class="p-3">#${x.id}</td><td class="p-3">${escapeHTML(x.entryType)}</td><td class="p-3">${money(x.amount,x.currency)}</td><td class="p-3">${escapeHTML(x.reference||"—")}</td><td class="p-3">${new Date(x.createdAt).toLocaleString()}</td></tr>`).join(""))}</div>
    </details>
    <h2 class="mb-3 mt-7 font-semibold">Assign opening cash</h2>
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
          ${fundingSources.filter(a=>a.currency===settings.currency).map(a=>`<option value="${a.id}">${escapeHTML({
            central_cash: "Central cash",
            investment_cash: "Investment cash",
            bank: "Bank account",
            superadmin_cash: "Superadmin cash"
          }[a.accountType] || a.accountType)} · ${money(a.balance,a.currency)}</option>`).join("")}
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
    ${table(["Session","Staff","Count","Difference","Available","Status","Review"],reconciliationRows.map(r=>`<tr class="border-b"><td class="p-4">#${r.sessionId}</td><td class="p-4">${escapeHTML(r.userEmail)}</td><td class="p-4">${money(r.countedCash,r.currency)} of ${money(r.expectedCash,r.currency)}</td><td class="p-4 ${r.cashDifference<0?"text-red-600":"text-green-600"}">${money(r.cashDifference,r.currency)}</td><td class="p-4">${r.status==="verified"?money(r.currentAvailableCash,r.currency):"—"}</td><td class="p-4 capitalize">${escapeHTML(r.status)}</td><td class="p-4">${r.status==="pending"?`<form data-form="reconciliation-review" data-reconciliation-id="${r.id}" class="grid gap-2"><input name="remarks" maxlength="500" placeholder="Remarks" class="h-10 rounded-lg border px-3 text-xs"><div class="flex gap-2"><button name="action" value="verify" class="rounded-lg bg-ink px-3 py-2 text-xs text-white">Verify</button><button name="action" value="reject" class="rounded-lg border px-3 py-2 text-xs text-red-600">Reject</button></div></form>`:r.status==="verified"?`<form data-form="reconciliation-review" data-reconciliation-id="${r.id}" class="grid gap-2"><div class="text-xs text-gray-500">${escapeHTML(r.remarks||"")}</div><input name="remarks" required minlength="10" maxlength="500" placeholder="Reason for voiding verification" class="h-10 rounded-lg border px-3 text-xs"><button name="action" value="void" class="w-fit rounded-lg border border-red-300 px-3 py-2 text-xs text-red-600">Void verification</button></form>`:escapeHTML(r.remarks||"")}</td></tr>`).join(""))}
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
  const debtorsEl = $("#pos-debtors-list");

  if (el) {
    el.innerHTML = `<div class="rounded-lg border border-gray-100 bg-white p-3 text-xs">
      <div class="flex items-center justify-between gap-2">
        <span class="text-gray-500">Current cash in hand</span>
        <strong>${session ? money(session.expectedCash, session.currency) : "No open session"}</strong>
      </div>
      ${session ? `<div class="mt-2 grid grid-cols-2 gap-2 text-[10px] text-gray-500">
        <span>Cash sales ${money(session.cashSales || 0, session.currency)}</span>
        <span>Collections ${money(session.cashCollections || 0, session.currency)}</span>
      </div>` : ""}
    </div>`;
  }

  if (!debtorsEl) return;

  const debtors = customers
    .map(c => ({
      ...c,
      dueBalances: (c.balances || []).filter(b => b.outstanding > 0)
    }))
    .filter(c => c.dueBalances.length)
    .sort((a, b) =>
      b.dueBalances.reduce((sum, b) => sum + b.outstanding, 0) -
      a.dueBalances.reduce((sum, b) => sum + b.outstanding, 0)
    );

  debtorsEl.innerHTML = debtors.length
    ? `<div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr class="border-b text-xs text-gray-500">
              <th class="pb-3 font-medium">Customer</th>
              <th class="pb-3 text-right font-medium">Outstanding</th>
            </tr>
          </thead>
          <tbody>
            ${debtors.map(c => `<tr class="border-b border-gray-100">
              <td class="py-3 font-medium">${escapeHTML(c.name)}${c.isOwner ? ' (Owner)' : ''}</td>
              <td class="py-3 text-right font-semibold">
                ${c.dueBalances.map(b => money(b.outstanding, b.currency)).join(" · ")}
                <button type="button" data-pos-collect="${c.id}"
                  class="ml-3 rounded-lg bg-orange-600 px-3 py-2 text-xs font-semibold text-white">
                  Collect
                </button>
              </td>
            </tr>`).join("")}
          </tbody>
        </table>
      </div>`
    : '<p class="text-sm text-gray-500">No outstanding debtors.</p>';

  showPosCollectionRecovery();
}


// POS debt collection uses the same idempotent customer-payment workflow
// as the Customers page. A pending payment must be recovered, never recreated.
let posCollectionAccount = null;
let posCollectionTargets = [];
let posCollectionCustomerId = null;
let posCollectionBusy = false;

function posCollectionPending() {
  if (pendingCollection) return pendingCollection;
  try {
    const stored = localStorage.getItem(collectionKey());
    pendingCollection = stored ? JSON.parse(stored) : null;
  } catch {
    pendingCollection = null;
  }
  return pendingCollection;
}

function posCollectionRecoveryUI() {
  const pending = posCollectionPending();
  const recover = $("#pos-collection-recover");
  recover.classList.toggle("hidden", !pending);
  recover.disabled = posCollectionBusy || !pending;
  $("#pos-collection-submit").disabled = posCollectionBusy || Boolean(pending);
  return pending;
}

async function recoverPosCollection() {
  const pending = posCollectionPending();
  if (!pending || posCollectionBusy) return;

  const elements = posCollectionElements();
  posCollectionSetBusy(true);
  elements.error.textContent = "";

  try {
    const result = await api(
      `/api/customers/${pending.customerId}/payments`,
      jsonRequest(pending.request)
    );

    localStorage.removeItem(collectionKey());
    pendingCollection = null;

    elements.dialog.close();
    await loadPage();

    toast(`Payment confirmed. Remaining on selected debt: ${money(result.outstanding, result.currency)}`);
  } catch (error) {
    if ([400, 403, 404, 409].includes(error.status)) {
      localStorage.removeItem(collectionKey());
      pendingCollection = null;
      elements.error.textContent = error.message;
    } else {
      elements.error.textContent =
        "Payment confirmation is still uncertain. Retry recovery; do not create another payment.";
    }
  } finally {
    posCollectionSetBusy(false);
    posCollectionRecoveryUI();
  }
}

function posCollectionElements() {
  return {
    dialog: $("#pos-collection-dialog"),
    form: $("#pos-collection-form"),
    target: $("#pos-collection-target"),
    amount: $("#pos-collection-amount"),
    full: $("#pos-collection-full"),
    error: $("#pos-collection-error"),
    remaining: $("#pos-collection-remaining"),
    submit: $("#pos-collection-submit")
  };
}

function posCollectionSelectedTarget() {
  const {target} = posCollectionElements();
  return posCollectionTargets.find(t => t.key === target.value);
}

function posCollectionUpdateRemaining() {
  const {amount,remaining,error} = posCollectionElements();
  const target = posCollectionSelectedTarget();

  if (!target) {
    remaining.textContent = "";
    return;
  }

  const entered = amount.value ? Math.round(Number(amount.value) * 100) : 0;
  const valid = Number.isSafeInteger(entered) && entered >= 0;

  remaining.textContent = valid
    ? `Remaining on selected debt: ${money(Math.max(0, target.outstanding - entered), target.currency)}`
    : "";

  if (error.textContent && valid && entered <= target.outstanding) {
    error.textContent = "";
  }
}

function posCollectionSetBusy(value) {
  posCollectionBusy = value;
  const {form,submit} = posCollectionElements();
  for (const control of form.querySelectorAll("input, select, textarea, button")) {
    control.disabled = value;
  }
  submit.textContent = value ? "Recording payment..." : "Record collection";
}

async function openPosCollection(customerId) {
  const elements = posCollectionElements();
  if (posCollectionBusy) return;

  elements.error.textContent = "";

  if (posCollectionPending()) {
    elements.dialog.showModal();
    posCollectionRecoveryUI();
    elements.error.textContent =
      "A previous payment is awaiting confirmation. Recover it before collecting another.";
    return;
  }
  if (!session) {
    throw Error("Open your own register session before collecting a payment.");
  }

  const customer = customers.find(c => c.id === customerId);
  if (!customer) throw Error("Customer not found.");

  const account = await api(`/api/customers/${customerId}`);
  const targets = [];

  if (account.openingBalance?.outstanding > 0) {
    const balance = account.openingBalance;
    targets.push({
      key: `opening:${balance.id}`,
      openingBalanceId: balance.id,
      outstanding: balance.outstanding,
      currency: balance.currency,
      label: `Opening balance · ${money(balance.outstanding, balance.currency)}`
    });
  }

  for (const sale of account.sales || []) {
    if (sale.payment !== "credit" || sale.outstanding <= 0) continue;
    targets.push({
      key: `sale:${sale.id}`,
      saleId: sale.id,
      outstanding: sale.outstanding,
      currency: sale.currency,
      label: `Receipt #${sale.id} · ${money(sale.outstanding, sale.currency)}`
    });
  }

  const eligible = targets.filter(t => t.currency === session.currency);
  if (!eligible.length) {
    throw Error("No outstanding debt matches your current register currency.");
  }

  posCollectionAccount = account;
  posCollectionTargets = eligible;
  posCollectionCustomerId = customerId;

  $("#pos-collection-customer").textContent = account.customer.name;

  const outstanding = (customer.balances || [])
    .filter(b => b.outstanding > 0)
    .map(b => money(b.outstanding, b.currency))
    .join(" · ");

  $("#pos-collection-total").textContent = outstanding;

  elements.target.replaceChildren();

  for (const target of eligible) {
    const option = document.createElement("option");
    option.value = target.key;
    option.textContent = target.label;
    elements.target.append(option);
  }

  elements.amount.value = (eligible[0].outstanding / 100).toFixed(2);
  elements.form.elements.payment.value = "cash";
  elements.form.elements.note.value = "";
  posCollectionUpdateRemaining();

  elements.dialog.showModal();
  posCollectionRecoveryUI();
}

function posCollectionClose() {
  if (posCollectionBusy) return;
  posCollectionElements().dialog.close();
}

async function submitPosCollection(event) {
  event.preventDefault();
  if (posCollectionBusy) return;

  const elements = posCollectionElements();
  elements.error.textContent = "";

  const target = posCollectionSelectedTarget();

  try {
    if (posCollectionPending()) {
      throw Error("Recover the previous collection before recording another payment.");
    }
    if (!session) {
      throw Error("Open your register session before collecting a payment.");
    }
    if (!target) {
      throw Error("Select an outstanding receipt or opening balance.");
    }

    const amount = minorUnits(elements.amount.value);

    if (!Number.isSafeInteger(amount) || amount <= 0 || amount > target.outstanding) {
      throw Error("Enter an amount greater than zero and no more than the selected outstanding balance.");
    }

    if (amount > 10000000) {
      throw Error("This payment exceeds the maximum allowed amount.");
    }

    if (target.currency !== session.currency) {
      throw Error("The register currency must match the debt.");
    }

    const payment = elements.form.elements.payment.value;
    if (!["cash", "card"].includes(payment)) {
      throw Error("Select cash or card.");
    }

    const request = {
      requestId: crypto.randomUUID(),
      ...(target.openingBalanceId
        ? {openingBalanceId: target.openingBalanceId}
        : {saleId: target.saleId}),
      sessionId: session.id,
      amount,
      payment,
      note: elements.form.elements.note.value.trim()
    };

    // Persist before sending so an uncertain response can be retried safely.
    const pending = {customerId: posCollectionCustomerId, request};
    localStorage.setItem(collectionKey(), JSON.stringify(pending));
    pendingCollection = pending;

    posCollectionSetBusy(true);

    const result = await api(
      `/api/customers/${pending.customerId}/payments`,
      jsonRequest(pending.request)
    );

    localStorage.removeItem(collectionKey());
    pendingCollection = null;

    elements.dialog.close();

    await loadPage();
    toast(`Payment recorded. Remaining on selected debt: ${money(result.outstanding, result.currency)}`);
  } catch (error) {
    // The server rejects these requests definitively. Network errors and
    // ambiguous server failures retain the original request ID for recovery.
    if (pendingCollection && [400, 403, 404, 409].includes(error.status)) {
      localStorage.removeItem(collectionKey());
      pendingCollection = null;
    }

    elements.error.textContent = pendingCollection
      ? "Payment confirmation is uncertain. Do not submit again. Recover the pending payment."
      : error.message;
    posCollectionRecoveryUI();
  } finally {
    posCollectionSetBusy(false);
    posCollectionRecoveryUI();
  }
}

$("#pos-collection-recover").addEventListener("click", recoverPosCollection);

// Make recovery available from the POS page after a reload, even if
// the original customer no longer appears in the current debtor list.
function showPosCollectionRecovery() {
  const pending = posCollectionPending();
  const container = $("#pos-debtors-list");

  if (!container || !pending) return;

  if (!container.querySelector("[data-pos-recover-pending]")) {
    container.insertAdjacentHTML(
      "afterbegin",
      `<div class="mb-4 rounded-lg border border-orange-300 bg-orange-50 p-4 text-sm">
        <p class="font-semibold text-orange-900">Payment awaiting confirmation</p>
        <p class="mt-1 text-orange-800">Recover the previous collection before recording another payment.</p>
        <button type="button" data-pos-recover-pending
          class="mt-3 rounded-lg bg-orange-600 px-4 py-3 font-semibold text-white">
          Recover payment
        </button>
      </div>`
    );
  }
}

document.addEventListener("click", async event => {
  if (!event.target.closest("[data-pos-recover-pending]")) return;

  const dialog = $("#pos-collection-dialog");

  if (!dialog.open) dialog.showModal();

  posCollectionRecoveryUI();

  $("#pos-collection-error").textContent =
    "Recover the pending payment using its original transaction ID.";
});



document.addEventListener("click", async event => {
  const collect = event.target.closest("[data-pos-collect]");
  if (!collect) return;

  collect.disabled = true;

  try {
    await openPosCollection(Number(collect.dataset.posCollect));
  } catch (error) {
    toast(error.message);
  } finally {
    collect.disabled = false;
  }
});

$("#pos-collection-close").addEventListener("click", posCollectionClose);

$("#pos-collection-dialog").addEventListener("cancel", event => {
  if (posCollectionBusy) event.preventDefault();
});

$("#pos-collection-target").addEventListener("change", () => {
  const target = posCollectionSelectedTarget();
  if (target) {
    $("#pos-collection-amount").value = (target.outstanding / 100).toFixed(2);
  }
  posCollectionUpdateRemaining();
});

$("#pos-collection-amount").addEventListener("input", posCollectionUpdateRemaining);

$("#pos-collection-full").addEventListener("click", () => {
  const target = posCollectionSelectedTarget();
  if (!target) return;
  $("#pos-collection-amount").value = (target.outstanding / 100).toFixed(2);
  posCollectionUpdateRemaining();
});

$("#pos-collection-form").addEventListener("submit", submitPosCollection);

$("#other-page").addEventListener("submit", async e => {
  const form = e.target, kind = form.dataset.form;
  if (!["inventory-filter","capital-contribution","cash-allocation","database-reset","credit-policy","opening-correction","payment-reversal","admin-close","reconciliation-review","cash-handover-review"].includes(kind)) return;
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
    } else if (kind === "capital-contribution") {
      if (currentUser?.role !== "superadmin") throw new Error("Superadmin access required.");
      const amount = minorUnits(data.amount);
      const fundingAccountId = Number(data.fundingAccountId);
      if (amount <= 0 || !fundingSources.some(a => a.id === fundingAccountId && a.currency === settings.currency)) {
        throw new Error("Enter a positive amount and select an eligible account.");
      }
      if (!data.reference?.trim() || !data.notes?.trim() || data.confirmed !== "on") {
        throw new Error("Reference, explanation and confirmation are required.");
      }
      if (!window.confirm(`Record ${money(amount,settings.currency)} as NEW business capital? This changes the financial ledger. Do not retry if the response is uncertain.`)) return;
      if (pendingCapital) throw new Error("Recover the pending capital transaction first.");

      const request = {
        requestId:crypto.randomUUID(),
        entryType:"contribution",
        amount,
        fundingAccountId,
        reference:data.reference.trim(),
        notes:data.notes.trim()
      };

      try {
        localStorage.setItem(capitalPendingKey(), JSON.stringify(request));
      } catch {
        throw new Error("Cannot safely save the capital recovery request in this browser. No capital was submitted.");
      }
      pendingCapital = request;

      try {
        await sendPendingCapital();
      } catch (error) {
        await loadPage();
        throw error;
      }
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
      const remarks = (data.remarks || "").trim();

      if (action === "void") {
        if (remarks.length < 10) {
          toast("Enter a void reason of at least 10 characters");
          return;
        }
        if (!confirm("Void this verified reconciliation? Its cash will no longer be available for carry-forward. This action cannot be undone.")) return;
      }

      const result = await api(
        `/api/register-cash-reconciliations/${form.dataset.reconciliationId}/review`,
        jsonRequest({action, remarks})
      );

      await loadPage();
      toast(
        action === "verify"
          ? `Reconciliation verified · available ${money(result.verifiedAvailableCash)}`
          : action === "void"
            ? "Reconciliation verification voided"
            : "Reconciliation rejected"
      );
    } else if (kind === "cash-handover-review") {
      const action = e.submitter?.value || data.action;
      const requested = Number(form.dataset.requested);
      const approvedAmount = action === "reject" ? 0 : action === "full" ? requested : minorUnits(data.approvedAmount);
      if (action === "partial" && (approvedAmount <= 0 || approvedAmount >= requested)) throw new Error("Partial approval must be greater than zero and less than the requested amount.");
      const verb = action === "reject" ? "Reject" : action === "full" ? "Approve" : "Partially approve";
      if (!confirm(`${verb} this ${money(requested,form.dataset.currency)} cash handover${action === "partial" ? ` for ${money(approvedAmount,form.dataset.currency)}` : ""}?`)) return;
      await api(`/api/cash-handovers/${form.dataset.handoverId}/review`, jsonRequest({action:action === "reject" ? "reject" : "approve",approvedAmount,remarks:(data.remarks||"").trim()}));
      await loadPage();
      toast(action === "reject" ? "Cash handover rejected" : "Cash handover approved");
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


// Recover an uncertain capital submission using its original request ID.
document.addEventListener("click", async event => {
  const button = event.target.closest("[data-recover-capital]");
  if (!button || button.disabled) return;

  button.disabled = true;

  try {
    restorePendingCapital();
    await sendPendingCapital();
  } catch (error) {
    toast(
      pendingCapital
        ? "Capital confirmation remains uncertain. Retry recovery; do not create another contribution."
        : error.message
    );
    if (!pendingCapital) await loadPage();
  } finally {
    button.disabled = false;
  }
});
