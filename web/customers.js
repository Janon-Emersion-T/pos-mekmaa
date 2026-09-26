let customers = [], customerAccount = null, editingCustomer = null, selectedCustomer = null;
let pendingCollection = null;
const collectionKey = () => `counter.pending-collection.${currentUser.id}`;
function renderCustomerPicker() {
  const selector = $("#checkout-customer");
  const selected = pendingCheckout?.customerId || selectedCustomer;
  selector.innerHTML = '<option value="">Walk-in customer</option>' + customers.map(c=>`<option value="${c.id}">${escapeHTML(c.name)}${c.phone ? ' · '+escapeHTML(c.phone) : ''} (#${c.id})</option>`).join("");
  selector.value = selected || "";
  selector.disabled = busy || Boolean(pendingCheckout);
  $("#credit-options").classList.toggle("hidden", payment !== "credit");
  $("#credit-initial-payment").disabled = $("#credit-initial-method").disabled = busy || Boolean(pendingCheckout);
  const total = [...cart].reduce((sum,[id,qty])=>sum+(products.find(p=>p.id===id)?.price || 0)*qty,0);
  const initial = Math.round(Number($("#credit-initial-payment").value)*100);
  $("#credit-remaining").textContent = money(Math.max(0,total-(Number.isFinite(initial)?initial:0)));
}
$("#checkout-customer").onchange = e => {selectedCustomer = e.target.value ? Number(e.target.value) : null;};
$("#credit-initial-payment").oninput = renderCustomerPicker;
function optionalCustomerField(label,name,value,max,type="text") {
  return `<label class="text-xs text-gray-600">${label}<input name="${name}" type="${type}" maxlength="${max}" value="${escapeHTML(value || '')}" class="mt-2 h-11 w-full rounded-lg border px-3"></label>`;
}
function customersView() {
  const editing = customers.find(c=>c.id===editingCustomer);
  const q = (new URLSearchParams(location.search).get("q") || "").toLowerCase();
  const filtered = customers.filter(c=>(c.name+' '+c.phone+' '+c.email).toLowerCase().includes(q));
  const balances = c => c.balances.length ? c.balances.map(b=>money(b.outstanding,b.currency)).join(' · ') : 'No outstanding balance';
  let body = `<form data-form="customer" data-id="${editing?.id || ''}" class="mb-5 grid gap-4 rounded-xl border bg-white p-5 sm:grid-cols-3"><h2 class="font-semibold sm:col-span-3">${editing?'Edit customer':'Add customer'}</h2>${field('Customer name','name','text',editing?.name || '')}${optionalCustomerField('Phone (optional)','phone',editing?.phone,50,'tel')}${optionalCustomerField('Email (optional)','email',editing?.email,254,'email')}${optionalCustomerField('Address (optional)','address',editing?.address,500)}${optionalCustomerField('Notes (optional)','notes',editing?.notes,1000)}<div class="flex items-center gap-3"><button class="rounded-lg bg-accent px-5 py-3 text-sm text-white">${editing?'Save customer':'Add customer'}</button>${editing?'<button type="button" data-cancel-customer class="text-sm">Cancel</button>':''}</div></form>`;
  body += filterForm('customer-filter',filterInput('Search name, phone, or email','q'));
  body += table(['Customer','Phone','Email','Outstanding','Actions'],filtered.map(c=>`<tr class="border-b"><td class="p-4 font-medium">${escapeHTML(c.name)} <span class="text-xs text-gray-400">#${c.id}</span></td><td class="p-4">${escapeHTML(c.phone)}</td><td class="p-4">${escapeHTML(c.email)}</td><td class="p-4">${balances(c)}</td><td class="p-4"><a href="/customers?id=${c.id}" data-page="customers" class="mr-3 text-orange-600">Account</a><button data-edit-customer="${c.id}" class="text-orange-600">Edit</button></td></tr>`).join(''));
  try { pendingCollection = JSON.parse(localStorage.getItem(collectionKey()) || 'null'); } catch { /* Keep any in-memory pending request. */ }
  if (pendingCollection) body += `<div class="my-5 rounded-lg border bg-orange-50 p-4 text-sm">A customer payment is awaiting confirmation. Recover it before recording another payment.<button data-recover-collection class="ml-3 text-orange-600">Recover payment</button></div>`;
  if (!customerAccount) return body;
  const {customer,sales:history,payments} = customerAccount;
  const summary = customers.find(c=>c.id===customer.id) || customer;
  const unpaid = history.filter(s=>s.payment==='credit' && s.outstanding>0);
  body += `<h2 class="mb-3 mt-7 text-xl font-semibold">${escapeHTML(customer.name)} · Account</h2><p class="mb-4 text-sm">${balances(summary)}</p><p class="mb-4 text-xs text-gray-500">${escapeHTML(customer.address)}${customer.notes ? ' · '+escapeHTML(customer.notes) : ''}</p>`;
  if (unpaid.length) body += `<form data-form="customer-payment" data-customer-id="${customer.id}" class="mb-5 grid gap-4 rounded-xl border bg-white p-5 sm:grid-cols-3"><label class="text-xs">Unpaid receipt<select name="saleId" required class="mt-2 h-11 w-full rounded-lg border px-3">${unpaid.map(s=>`<option value="${s.id}">#${s.id} · ${money(s.outstanding,s.currency)} due</option>`).join('')}</select></label>${amountField('Amount received','amount')}<label class="text-xs">Payment method<select name="payment" class="mt-2 h-11 w-full rounded-lg border px-3"><option value="cash">Cash</option><option value="card">Card</option></select></label>${optionalCustomerField('Note (optional)','note','',500)}<button ${!session || pendingCollection?'disabled':''} class="rounded-lg bg-accent px-5 py-3 text-sm text-white disabled:opacity-50">Record payment</button><p class="text-xs text-gray-500">${session?'Record the amount actually received. Partial payments are accepted.':'Open a register session to collect payment.'}</p></form>`;
  body += table(['Receipt','Date','Payment','Total','Returned','Paid (net)','Remaining',''],history.map(s=>`<tr class="border-b"><td class="p-4">#${s.id}</td><td class="p-4">${new Date(s.created).toLocaleString()}</td><td class="p-4">${s.payment==='credit'?'Pay later':escapeHTML(s.payment)}</td><td class="p-4">${money(s.total,s.currency)}</td><td class="p-4">${money(s.refunded,s.currency)}</td><td class="p-4">${money(s.paidAmount,s.currency)}</td><td class="p-4">${money(s.outstanding,s.currency)}</td><td class="p-4"><button data-receipt="${s.id}" class="text-orange-600">Receipt</button></td></tr>`).join(''));
  body += '<h3 class="mb-3 mt-6 font-semibold">Payment history</h3>'+table(['Date','Receipt','Amount','Method','Staff','Note'],payments.map(p=>`<tr class="border-b"><td class="p-4">${new Date(p.created).toLocaleString()}</td><td class="p-4">#${p.saleId}</td><td class="p-4">${money(p.amount,p.currency)}</td><td class="p-4">${escapeHTML(p.payment)}</td><td class="p-4">${escapeHTML(p.cashierEmail)}</td><td class="p-4">${escapeHTML(p.note)}</td></tr>`).join(''));
  return body;
}
async function sendCustomerPayment() {
  try {
    const result = await api(`/api/customers/${pendingCollection.customerId}/payments`,jsonRequest(pendingCollection.request));
    localStorage.removeItem(collectionKey()); pendingCollection = null;
    await loadPage(); toast(`Payment recorded. Remaining balance: ${money(result.outstanding,result.currency)}`);
  } catch(error) {
    if ([400,403,404,409].includes(error.status)) {localStorage.removeItem(collectionKey());pendingCollection=null;}
    throw error;
  }
}
$("#other-page").addEventListener('submit',async e=>{
  const form=e.target,kind=form.dataset.form;
  if (!['customer','customer-filter','customer-payment'].includes(kind)) return;
  e.preventDefault(); if(form.dataset.saving)return;
  const data=Object.fromEntries(new FormData(form));
  if(kind==='customer-filter'){await navigate('customers',true,data.q?'?q='+encodeURIComponent(data.q):'');return;}
  const button=form.querySelector('button');button.disabled=true;form.dataset.saving='true';
  try {
    if(kind==='customer'){
      await api(form.dataset.id?`/api/customers/${form.dataset.id}`:'/api/customers',{...jsonRequest(data),method:form.dataset.id?'PUT':'POST'});
      editingCustomer=null; await loadPage(); toast('Customer saved');
    } else {
      if(pendingCollection)throw Error('Recover the pending payment first');
      if(!session)throw Error('Open a register session first');
      const amount=minorUnits(data.amount),sale=customerAccount.sales.find(s=>s.id===Number(data.saleId));
      if(!sale || amount<=0 || amount>sale.outstanding)throw Error('Enter an amount between zero and the remaining balance');
      if(sale.currency!==session.currency)throw Error('The register currency must match the receipt');
      const pending={customerId:Number(form.dataset.customerId),request:{requestId:crypto.randomUUID(),saleId:sale.id,sessionId:session.id,amount,payment:data.payment,note:data.note}};
      localStorage.setItem(collectionKey(),JSON.stringify(pending));pendingCollection=pending;
      await sendCustomerPayment();
    }
  }catch(error){toast(error.message);if(kind==='customer-payment' && pendingCollection)renderOther();}
  finally{delete form.dataset.saving;button.disabled=false;}
});
document.addEventListener('click',async e=>{
  const edit=e.target.closest('[data-edit-customer]'),cancel=e.target.closest('[data-cancel-customer]'),recover=e.target.closest('[data-recover-collection]');
  if(edit||cancel){editingCustomer=edit?Number(edit.dataset.editCustomer):null;renderOther();}
  if(recover && !recover.disabled){recover.disabled=true;try{await sendCustomerPayment();}catch(error){toast(error.message);}finally{recover.disabled=false;}}
});
