(() => {
  const form = document.querySelector("#guest-form");
  const notice = document.querySelector("#notice");
  const history = document.querySelector("#history");
  const escape = value => String(value ?? "").replace(/[&<>"']/g,
    c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
  const request = async (url,options) => {
    const response = await fetch(url,options);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || data.message || "Request failed");
    return data;
  };
  let pendingRequestID = null;
  async function refresh() {
    const [products,records] = await Promise.all([
      request("/api/products"),request("/api/guest-refreshments")
    ]);
    const select = form.elements.productId;
    select.innerHTML = products.filter(p => p.active !== false && p.stock > 0)
      .map(p => `<option value="${p.id}">${escape(p.name)} — ${p.stock} available</option>`).join("");
    history.innerHTML = records.map(r => `<tr class="border-b">
      <td class="p-3">${escape(new Date(r.createdAt).toLocaleString())}</td>
      <td class="p-3">${escape(r.guestName)}</td>
      <td class="p-3">${escape(r.product)}</td>
      <td class="p-3">${r.quantity}</td>
      <td class="p-3">${escape(r.currency)} ${(r.totalCost/100).toFixed(2)}</td>
      <td class="p-3">${escape(r.reason)}</td>
      <td class="p-3">${escape(r.createdByEmail)}</td>
    </tr>`).join("") || '<tr><td colspan="7" class="p-5 text-gray-500">No records yet.</td></tr>';
  }
  form.addEventListener("submit",async e => {
    e.preventDefault();
    const button = form.querySelector("button");
    button.disabled = true;
    if (!pendingRequestID) pendingRequestID = crypto.randomUUID();
    const data = Object.fromEntries(new FormData(form));
    data.productId = Number(data.productId);
    data.quantity = Number(data.quantity);
    data.requestId = pendingRequestID;
    try {
      const result = await request("/api/guest-refreshments",{
        method:"POST",headers:{"Content-Type":"application/json"},
        body:JSON.stringify(data)
      });
      pendingRequestID = null;
      form.reset();
      notice.textContent = `Recorded #${result.id} successfully.`;
      notice.className = "my-4 text-sm text-green-700";
      await refresh();
    } catch(error) {
      notice.textContent = error.message;
      notice.className = "my-4 text-sm text-red-700";
    } finally {
      button.disabled = false;
    }
  });
  refresh().catch(error => {
    notice.textContent = error.message;
    notice.className = "my-4 text-sm text-red-700";
  });
})();
