const { test, before, after } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs/promises');
const path = require('node:path');
const { chromium } = require('playwright');
let browser, server, origin;
before(async () => {
  server = http.createServer(async (req, res) => {
    const name = path.basename(new URL(req.url, 'http://localhost').pathname);
    const file = name.includes('.') ? name : 'index.html';
    try {
      const body = await fs.readFile(path.join(__dirname, '../web', file));
      res.setHeader('Content-Type', file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html');
      res.end(body);
    } catch { res.writeHead(404); res.end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({headless:true});
});
after(async () => { await browser?.close(); await new Promise(resolve => server?.close(resolve)); });
const product = {id:1,name:'Test bottle',category:'Drinks',categoryId:1,price:12550,cost:7550,stock:8,reorderLevel:2,art:'bottle',color:'#eeeeee'};
const register = {id:1,openingCash:0,expectedCash:0,currency:'LKR',openedAt:'2026-09-25T08:00:00Z',openedByEmail:'test@example.test'};
const sale = {id:1,total:12550,currency:'LKR',payment:'cash',items:'1 × Test bottle',cashierEmail:'test@example.test',created:'2026-09-25T08:00:00Z',cashReceived:13000,changeDue:450,refunded:0,lines:[{id:1,name:'Test bottle',quantity:1,unitPrice:12550,refunded:0}]};
async function setup(t, options = {}) {
  const context = await browser.newContext();
  t.after(() => context.close());
  const page = await context.newPage(), errors = [], requests = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', response => { if (response.url().endsWith('.js') && !response.ok()) errors.push(`Script failed: ${response.url()}`); });
  await page.route('**/api/**', async route => {
    const request = route.request(), pathname = new URL(request.url()).pathname;
    requests.push({path:pathname,method:request.method(),body:request.postDataJSON()});
    if (options.handle && await options.handle(route, pathname)) return;
    const responses = {
      '/api/auth/me':{id:1,email:'test@example.test',role:'superadmin',mustChangePassword:options.mustChangePassword || false},
      '/api/settings':{currency:'LKR',currencies:[{code:'LKR',name:'Sri Lankan Rupee'}]},
      '/api/customers':options.customers || [],
      '/api/customers/1':options.customerAccount || {customer:{id:1,name:'Jane'},sales:[],payments:[]},
      '/api/products':options.empty ? [] : [product], '/api/categories':[{id:1,name:'Drinks',productCount:1}],
      '/api/session':register, '/api/sessions':[], '/api/sales':[sale], '/api/sales/1':sale,
      '/api/inventory/movements':[], '/api/purchases':[], '/api/petty-cash':[], '/api/users':[],
      '/api/reports/daily':{date:'2026-09-25',timezone:'Asia/Colombo',rows:[]}, '/api/audit':[],
      '/api/checkout':sale, '/api/auth/password':{ok:true},
    };
    if (!(pathname in responses)) { errors.push(`Unexpected API: ${pathname}`); await route.fulfill({status:404,json:{error:'Unknown API'}}); return; }
    await route.fulfill({json:responses[pathname]});
  });
  return {page,errors,requests};
}
async function ready(page, route = '/pos') {
  await page.goto(origin+route);
  await page.waitForFunction(() => document.querySelector('#user-email').textContent === 'test@example.test');
  await page.waitForFunction(() => document.querySelector('#products').textContent.includes('Test bottle') || document.querySelector('#products').textContent.includes('No products yet'));
}
test('menu starts and every management page renders without missing functions', async t => {
  const {page,errors} = await setup(t);
  await ready(page);
  for (const route of ['customers','products','sales','reports','audit','account','session','inventory','purchases','petty','users','settings']) {
    await page.evaluate(route => navigate(route), route);
    assert.ok(await page.locator('#other-page h1').count(), `${route} did not render`);
    assert.doesNotMatch(await page.locator('#other-page').innerText(), /Could not load/);
  }
  assert.deepEqual(errors, []);
});
test('new catalogs render an empty menu', async t => {
  const {page,errors} = await setup(t,{empty:true});
  await ready(page);
  assert.match(await page.locator('#products').innerText(), /No products yet/);
  assert.deepEqual(errors,[]);
});
test('temporary password opens a working password-change form', async t => {
  const {page,errors,requests} = await setup(t,{mustChangePassword:true});
  await ready(page);
  await page.locator('[name=currentPassword]').fill('temporary-password');
  await page.locator('[name=newPassword]').fill('replacement-password');
  await page.locator('[name=confirmPassword]').fill('replacement-password');
  await page.locator('[data-form=password] button').click();
  await page.waitForURL('**/login');
  assert.deepEqual(requests.find(r=>r.path==='/api/auth/password').body,{currentPassword:'temporary-password',newPassword:'replacement-password'});
  assert.deepEqual(errors,[]);
});
test('unavailable browser storage does not break menu loading', async t => {
  const {page,errors} = await setup(t);
  await page.addInitScript(() => { Storage.prototype.getItem = () => { throw Error('Storage blocked'); }; });
  await ready(page);
  assert.deepEqual(errors,[]);
});
test('cash checkout sends required amounts and shows change', async t => {
  const {page,errors,requests} = await setup(t);
  await ready(page);
  await page.locator('[data-product="1"]').click();
  page.once('dialog', dialog => dialog.accept('130.00'));
  await page.locator('#checkout').click();
  await page.locator('#receipt-dialog').waitFor({state:'visible'});
  const request = requests.find(r=>r.path==='/api/checkout').body;
  assert.equal(request.cashReceived,13000);
  assert.equal(request.expectedTotal,12550);
  assert.equal(request.sessionId,1);
  assert.match(request.requestId,/^[a-zA-Z0-9_-]{16,100}$/);
  assert.match(await page.locator('#receipt').innerText(),/Change:.*4\.50/);
  assert.deepEqual(errors,[]);
});
test('lost checkout response survives reload and retries with the same request', async t => {
  let attempts = 0;
  const {page,errors,requests} = await setup(t,{handle:async (route,pathname) => {
    if (pathname !== '/api/checkout') return false;
    attempts++;
    if (attempts === 1) await route.abort('failed');
    else await route.fulfill({json:{...sale,payment:'card',cashReceived:null,changeDue:null}});
    return true;
  }});
  await ready(page);
  await page.locator('[data-product="1"]').click();
  await page.locator('[data-payment=card]').click();
  await page.locator('#checkout').click();
  await page.waitForFunction(() => document.querySelector('#checkout').textContent.includes('Recover pending sale') && !document.querySelector('#checkout').disabled);
  await ready(page);
  await page.locator('#checkout').click();
  await page.locator('#receipt-dialog').waitFor({state:'visible'});
  const calls = requests.filter(r=>r.path==='/api/checkout');
  assert.equal(calls.length,2);
  assert.deepEqual(calls[0].body,calls[1].body);
  assert.equal(calls[0].body.cashReceived,undefined);
  assert.deepEqual(errors,[]);
});
test('refund retry survives reload without issuing a second request ID', async t => {
  let attempts = 0;
  const {page,errors,requests} = await setup(t,{handle:async(route,pathname) => {
    if (pathname !== '/api/sales/1/refunds') return false;
    attempts++;
    if (attempts === 1) await route.abort('failed');
    else await route.fulfill({json:{id:1}});
    return true;
  }});
  await ready(page,'/sales');
  await page.locator('[data-refund="1"]').click();
  await page.locator('[data-return-item]').fill('1');
  await page.locator('#refund-form [name=reason]').fill('Returned item');
  await page.locator('#refund-form [name=confirmed]').check();
  await page.locator('#refund-submit').click();
  await page.waitForFunction(()=>document.querySelector('#refund-error').textContent.length>0);
  await ready(page,'/sales');
  await page.locator('[data-refund="1"]').click();
  await page.locator('#refund-dialog').waitFor({state:'visible'});
  assert.equal(await page.locator('#refund-submit').innerText(),'Recover refund');
  await page.locator('#refund-submit').click();
  await page.locator('#refund-dialog').waitFor({state:'hidden'});
  const calls=requests.filter(r=>r.path==='/api/sales/1/refunds');
  assert.equal(calls.length,2);
  assert.deepEqual(calls[0].body,calls[1].body);
  assert.deepEqual(errors,[]);
});
const customer = {id:1,name:'Jane Silva',phone:'0771234567',email:'',address:'',notes:'',balances:[{currency:'LKR',outstanding:10000}]};
const creditSale = {...sale,payment:'credit',customerId:1,customerName:'Jane Silva',paidAmount:2550,outstanding:10000,cashReceived:null,changeDue:null};
test('pay later requires a customer and records a partial payment at checkout', async t => {
  const {page,errors,requests}=await setup(t,{customers:[customer],handle:async(route,pathname)=>{
    if(pathname!=='/api/checkout')return false;
    await route.fulfill({json:creditSale});return true;
  }});
  await ready(page);
  await page.locator('[data-product="1"]').click();
  await page.locator('[data-payment=credit]').click();
  await page.locator('#checkout').click();
  await page.waitForFunction(()=>document.querySelector('#toast').textContent.includes('Select a customer'));
  assert.equal(requests.filter(r=>r.path==='/api/checkout').length,0);
  await page.locator('#checkout-customer').selectOption('1');
  await page.locator('#credit-initial-payment').fill('25.50');
  await page.locator('#credit-initial-method').selectOption('card');
  assert.match(await page.locator('#credit-remaining').innerText(),/100\.00/);
  await page.locator('#checkout').click();
  await page.locator('#receipt-dialog').waitFor({state:'visible'});
  const request=requests.find(r=>r.path==='/api/checkout').body;
  assert.equal(request.customerId,1);
  assert.equal(request.payment,'credit');
  assert.equal(request.initialPayment,2550);
  assert.equal(request.initialPaymentMethod,'card');
  assert.equal(request.cashReceived,undefined);
  assert.match(await page.locator('#receipt').innerText(),/Customer: Jane Silva/);
  assert.match(await page.locator('#receipt').innerText(),/Remaining balance:.*100\.00/);
  assert.equal(await page.locator('#checkout-customer').inputValue(),'');
  assert.deepEqual(errors,[]);
});
test('customers can be created and edited without login fields',async t=>{
  let created=false;
  const {page,errors,requests}=await setup(t,{handle:async(route,pathname)=>{
    if(pathname==='/api/customers' && route.request().method()==='POST') {created=true;await route.fulfill({status:201,json:{id:1}});return true;}
    if(pathname==='/api/customers' && created){await route.fulfill({json:[customer]});return true;}
    if(pathname==='/api/customers/1' && route.request().method()==='PUT'){await route.fulfill({json:{id:1}});return true;}
    return false;
  }});
  await ready(page,'/customers');
  await page.locator('[data-form=customer] [name=name]').fill(customer.name);
  await page.locator('[data-form=customer] [name=phone]').fill(customer.phone);
  assert.equal(await page.locator('[data-form=customer] [type=password]').count(),0);
  await page.locator('[data-form=customer] button').click();
  await page.locator('[data-edit-customer="1"]').click();
  assert.equal(await page.locator('[data-form=customer] [name=name]').inputValue(),customer.name);
  await page.locator('[data-form=customer] [name=notes]').fill('Updated customer notes');
  await page.locator('[data-form=customer] button').first().click();
  await page.waitForFunction(()=>document.querySelector('#toast').textContent==='Customer saved');
  assert.ok(requests.some(r=>r.path==='/api/customers/1'&&r.method==='PUT'&&r.body.notes==='Updated customer notes'));
  assert.deepEqual(errors,[]);
});
test('customer collections recover the same payment after a lost response',async t=>{
  let attempts=0;
  const {page,errors,requests}=await setup(t,{customers:[customer],customerAccount:{customer,sales:[creditSale],payments:[]},handle:async(route,pathname)=>{
    if(pathname!=='/api/customers/1/payments')return false;
    if(++attempts===1)await route.abort('failed');
    else await route.fulfill({json:{id:1,outstanding:5000,currency:'LKR'}});
    return true;
  }});
  await ready(page,'/customers?id=1');
  await page.locator('[data-form=customer-payment] [name=amount]').fill('50.00');
  await page.locator('[data-form=customer-payment] button').click();
  await page.locator('[data-recover-collection]').waitFor();
  await ready(page,'/customers?id=1');
  await page.locator('[data-recover-collection]').click();
  await page.waitForFunction(()=>document.querySelector('#toast').textContent.includes('Payment recorded'));
  const calls=requests.filter(r=>r.path==='/api/customers/1/payments');
  assert.equal(calls.length,2);assert.deepEqual(calls[0].body,calls[1].body);
  assert.equal(calls[0].body.amount,5000);assert.equal(calls[0].body.sessionId,1);
  assert.equal(await page.locator('[data-recover-collection]').count(),0);
  assert.deepEqual(errors,[]);
});
test('credit returns show debt cancellation and money to refund separately',async t=>{
  const {page,errors,requests}=await setup(t,{handle:async(route,pathname)=>{
    if(pathname==='/api/sales/1'){await route.fulfill({json:creditSale});return true;}
    if(pathname==='/api/sales/1/refunds'){await route.fulfill({json:{id:1}});return true;}
    return false;
  }});
  await ready(page,'/sales');
  await page.locator('[data-refund="1"]').click();
  await page.locator('[data-return-item]').fill('1');
  assert.match(await page.locator('#credit-refund-summary').innerText(),/Debt cancelled:.*100\.00.*Money to return:.*25\.50/);
  await page.locator('#refund-form [name=reason]').fill('Customer return');
  await page.locator('#refund-form [name=confirmed]').check();
  await page.locator('#refund-submit').click();
  await page.locator('#refund-dialog').waitFor({state:'hidden'});
  const request=requests.find(r=>r.path==='/api/sales/1/refunds').body;
  assert.equal(request.expectedOutstanding,10000);
  assert.equal(request.creditRefundPayment,'cash');
  assert.deepEqual(errors,[]);
});
test('customers are accessible from the mobile navigation',async t=>{
  const {page,errors}=await setup(t);
  await page.setViewportSize({width:390,height:844});
  await ready(page);
  await page.locator('nav.lg\\:hidden [data-page=customers]').click();
  await page.locator('[data-form=customer]').waitFor({state:'visible'});
  assert.equal(await page.locator('#other-page h1').innerText(),'Customers');
  assert.deepEqual(errors,[]);
});
