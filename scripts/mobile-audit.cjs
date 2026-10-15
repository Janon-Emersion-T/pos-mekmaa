const { chromium } = require('playwright');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const widths = [320, 360, 375, 390, 430, 768, 1024, 1440];
const routes = [
  'pos', 'customers', 'products', 'sales', 'reports',
  'audit', 'account', 'session', 'inventory',
  'purchases', 'petty', 'users', 'settings'
];

const fixtures = {
  '/api/auth/me': {
    id: 1,
    email: 'audit@example.test',
    role: 'superadmin',
    mustChangePassword: false
  },
  '/api/settings': {
    currency: 'LKR',
    currencies: [{code: 'LKR', name: 'Sri Lankan Rupee'}]
  },
  '/api/optimized-debts/capacity': {
    enabled: false,
    currency: 'LKR',
    verifiedMargin: 0,
    outstandingDebt: 0,
    availableCredit: 0,
    unverifiedItems: 0
  },
  '/api/products': [],
  '/api/categories': [],
  '/api/customers': [],
  '/api/session': null,
  '/api/sessions': [],
  '/api/sales': [],
  '/api/reports/daily': {
    date: '2026-10-15',
    timezone: 'Asia/Colombo',
    rows: []
  },
  '/api/audit': [],
  '/api/inventory/movements': [],
  '/api/purchases': [],
  '/api/petty-cash': [],
  '/api/users': [],
  '/api/register-cash-allocations': [],
  '/api/register-cash-reconciliations': [],
  '/api/register-cash-carry-forward-options': [],
  '/api/financial/funding-sources': []
};

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const name = path.basename(url.pathname);
  const file = name.includes('.') ? name : 'index.html';
  const filename = path.join(root, 'web', file);

  if (!filename.startsWith(path.join(root, 'web') + path.sep)) {
    res.writeHead(403).end();
    return;
  }

  fs.readFile(filename, (error, content) => {
    if (error) {
      res.writeHead(404).end();
      return;
    }

    const type = file.endsWith('.js')
      ? 'text/javascript'
      : file.endsWith('.css')
        ? 'text/css'
        : 'text/html';

    res.writeHead(200, {'Content-Type': type});
    res.end(content);
  });
});

async function main() {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));

  let browser;
  const findings = [];

  try {
    browser = await chromium.launch({headless: true});
    const origin = `http://127.0.0.1:${server.address().port}`;

    for (const width of widths) {
      const context = await browser.newContext({
        viewport: {width, height: 800}
      });

      await context.route('**/api/**', async route => {
        const pathname = new URL(route.request().url()).pathname;
        const data = Object.hasOwn(fixtures, pathname)
          ? fixtures[pathname]
          : [];

        await route.fulfill({json: data});
      });

      const page = await context.newPage();
      const errors = [];

      page.on('pageerror', error => errors.push(error.message));

      await page.goto(origin + '/pos');

      await page.waitForFunction(() =>
        document.querySelector('#user-email')?.textContent
          === 'audit@example.test'
      );

      for (const route of routes) {
        await page.evaluate(name => navigate(name), route);

        await page.waitForFunction(name =>
          document.querySelector('#breadcrumb')?.textContent
            === ({
              pos: 'Point of sale',
              customers: 'Customers',
              products: 'Products',
              sales: 'Sales history',
              reports: 'Daily report',
              audit: 'Audit history',
              account: 'Account',
              session: 'Register session',
              inventory: 'Inventory',
              purchases: 'Purchases',
              petty: 'Petty cash',
              users: 'Staff users',
              settings: 'Settings'
            })[name], route);

        await page.waitForTimeout(100);

        const measurement = await page.evaluate(() => ({
          viewport: document.documentElement.clientWidth,
          pageWidth: document.documentElement.scrollWidth,
          bodyWidth: document.body.scrollWidth,
          title: document.querySelector('#breadcrumb')?.textContent,
          visiblePage: document.querySelector('#other-page')?.innerText
        }));

        const overflow = Math.max(
          measurement.pageWidth,
          measurement.bodyWidth
        ) - measurement.viewport;

        const failed = overflow > 2 ||
          measurement.visiblePage?.includes('Could not load this page');

        findings.push({
          width,
          route,
          overflow,
          failed,
          errors: [...errors]
        });

        console.log(
          `${failed ? 'FAIL' : 'PASS'} ${String(width).padStart(4)}px ` +
          `${route.padEnd(12)} overflow=${overflow}px`
        );
      }

      await context.close();
    }

    fs.mkdirSync(path.join(root, 'reports'), {recursive: true});
    fs.writeFileSync(
      path.join(root, 'reports/mobile-audit.json'),
      JSON.stringify(findings, null, 2)
    );

    const failures = findings.filter(
      item => item.failed || item.errors.length
    );

    console.log('\n========== SUMMARY ==========');
    console.log('Measurements:', findings.length);
    console.log('Failures:', failures.length);
    console.log('Report: reports/mobile-audit.json');

    if (failures.length) process.exitCode = 1;
  } finally {
    await browser?.close();
    await new Promise(resolve => server.close(resolve));
  }
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
  server.close();
});
