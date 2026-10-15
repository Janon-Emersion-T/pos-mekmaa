const { chromium } = require("playwright");
const http = require("node:http");
const fs = require("node:fs");
const path = require("node:path");

const webRoot = path.resolve(__dirname, "../web");

const server = http.createServer((req, res) => {
  const pathname = new URL(req.url, "http://localhost").pathname;
  const filename = path.join(
    webRoot,
    path.basename(pathname).includes(".")
      ? path.basename(pathname)
      : "index.html"
  );

  fs.readFile(filename, (error, content) => {
    if (error) {
      res.writeHead(404).end();
      return;
    }

    const type = filename.endsWith(".js")
      ? "text/javascript"
      : filename.endsWith(".css")
        ? "text/css"
        : "text/html";

    res.writeHead(200, {"Content-Type": type});
    res.end(content);
  });
});

async function main() {
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));

  let browser;

  try {
    browser = await chromium.launch({headless: true});

    const page = await browser.newPage({
      viewport: {width: 375, height: 812}
    });

    const errors = [];
    const requested = [];

    page.on("pageerror", error => errors.push(error.message));

    await page.route("**/api/**", async route => {
      const url = new URL(route.request().url());
      requested.push(url.pathname);

      let data = [];

      if (url.pathname === "/api/auth/me") {
        data = {
          id: 1,
          email: "test@example.com",
          role: "superadmin",
          mustChangePassword: false
        };
      } else if (url.pathname === "/api/help") {
        data = [{
          slug: "CASHIER_GUIDE",
          title: "Cashier guide",
          description: "Daily POS operations",
          category: "Daily operations"
        }];
      } else if (url.pathname === "/api/help/CASHIER_GUIDE") {
        data = {
          slug: "CASHIER_GUIDE",
          title: "Cashier guide",
          category: "Daily operations",
          content: "# Cashier guide\n\n## Checkout\n\nFollow the checkout process."
        };
      } else if (url.pathname === "/api/settings") {
        data = {currency: "LKR"};
      }

      await route.fulfill({json: data});
    });

    const base = `http://127.0.0.1:${server.address().port}`;

    await page.goto(base + "/pos");

    await page.waitForFunction(() =>
      document.querySelector("#breadcrumb")?.textContent
        === "Point of sale"
    );

    const mobileHelp = page.locator(
      'nav.lg\\:hidden [data-page="help"]'
    );

    console.log(
      "Mobile Help navigation:",
      await mobileHelp.count() ? "FOUND" : "MISSING"
    );

    await page.evaluate(() => navigate("help"));

    await page.waitForTimeout(500);

    console.log("Current route:", new URL(page.url()).pathname);
    console.log("Help API requested:", requested.includes("/api/help"));
    console.log(
      "Help workspace:",
      await page.locator("#help-workspace").count()
    );
    console.log(
      "Article cards:",
      await page.locator("[data-help-article]").count()
    );

    if (await page.locator("#help-search").count()) {
      await page.locator("#help-search").fill("Cashier");
      console.log(
        "Search results:",
        await page.locator("[data-help-article]").count()
      );
    }

    if (await page.locator("[data-help-article]").count()) {
      await page.locator("[data-help-article]").first().click();

      await page.waitForFunction(() =>
        document.querySelector("#help-workspace")
          ?.textContent.includes("Follow the checkout process.")
      );

      console.log(
        "Article content:",
        await page.locator("#help-workspace").innerText()
      );

      await page.locator("[data-help-back]").click();

      console.log(
        "Back navigation:",
        await page.locator("#help-search").count()
          ? "PASS"
          : "FAIL"
      );
    }

    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth
        - document.documentElement.clientWidth
    );

    console.log("Mobile horizontal overflow:", overflow);
    console.log("JavaScript errors:", errors);

    if (
      !requested.includes("/api/help") ||
      !(await page.locator("#help-search").count()) ||
      overflow > 2 ||
      errors.length
    ) {
      process.exitCode = 1;
    }
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
